package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ErrSessionNotFound  = errors.New("session not found")
	ErrStoreUnavailable = errors.New("store unavailable: Firestore is disabled")
)

// SessionMeta holds the metadata for a saved session.
type SessionMeta struct {
	ID              string    `json:"id"               firestore:"id"`
	Name            string    `json:"name"             firestore:"name"`
	Width           int       `json:"width"            firestore:"width"`
	Height          int       `json:"height"           firestore:"height"`
	GenerationCount int       `json:"generation_count" firestore:"generation_count"`
	// Write the client-provided time.Now().UTC() directly so the field is a real
	// Firestore Timestamp. The serverTimestamp tag can leave the field null/missing,
	// which causes OrderBy("created_at") in ListSessions to silently exclude documents.
	CreatedAt time.Time `json:"created_at" firestore:"created_at"`
}

// GenerationSnapshot stores one generation's board for Firestore.
// Cells is a flattened byte slice (len == width*height); ages are clamped to uint8.
// JSON encoding is suppressed; the handler builds the 2-D matrix from DecodeCells.
type GenerationSnapshot struct {
	Generation int    `json:"generation" firestore:"generation"`
	Cells      []byte `json:"-"          firestore:"cells"`
}

// Session combines metadata with all its generation snapshots.
type Session struct {
	SessionMeta
	Generations []GenerationSnapshot `json:"generations"`
}

// EncodeCells flattens a [][]uint16 grid into []byte (1 byte per cell, ages capped at 255).
func EncodeCells(cells [][]uint16, width, height int) []byte {
	b := make([]byte, height*width)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			v := cells[y][x]
			if v > 255 {
				v = 255
			}
			b[y*width+x] = byte(v)
		}
	}
	return b
}

// DecodeCells reconstructs a [][]uint16 grid from a flattened byte slice.
func DecodeCells(b []byte, width, height int) [][]uint16 {
	cells := make([][]uint16, height)
	for y := 0; y < height; y++ {
		cells[y] = make([]uint16, width)
		for x := 0; x < width; x++ {
			cells[y][x] = uint16(b[y*width+x])
		}
	}
	return cells
}

// Store is the persistence interface. All bulk writes use BulkWriter (never deprecated Batch()).
type Store interface {
	// SaveSession writes metadata and all generation snapshots.
	// Uses BulkWriter for generation documents.
	SaveSession(ctx context.Context, meta SessionMeta, gens []GenerationSnapshot) error

	// ListSessions returns all session metadata sorted by created_at DESC.
	// Returns a non-nil empty slice when no sessions exist.
	ListSessions(ctx context.Context) ([]SessionMeta, error)

	// LoadSession returns metadata and all generation snapshots in ascending order.
	// Returns ErrSessionNotFound if the id does not exist.
	LoadSession(ctx context.Context, id string) (SessionMeta, []GenerationSnapshot, error)

	// DeleteSession removes the session and all its generation snapshots via BulkWriter.
	// Returns ErrSessionNotFound if missing.
	DeleteSession(ctx context.Context, id string) error

	// Close releases resources.
	Close() error
}

// FirestoreStore implements Store using Google Cloud Firestore.
type FirestoreStore struct {
	client *firestore.Client
}

// NewFirestoreStore creates a Firestore-backed store using the named database.
func NewFirestoreStore(ctx context.Context, projectID, databaseName string) (*FirestoreStore, error) {
	client, err := firestore.NewClientWithDatabase(ctx, projectID, databaseName)
	if err != nil {
		return nil, fmt.Errorf("firestore.NewClientWithDatabase: %w", err)
	}
	return &FirestoreStore{client: client}, nil
}

func (fs *FirestoreStore) Close() error {
	return fs.client.Close()
}

func (fs *FirestoreStore) SaveSession(ctx context.Context, meta SessionMeta, gens []GenerationSnapshot) error {
	sessionRef := fs.client.Collection("sessions").Doc(meta.ID)

	// Write metadata first (separate from BulkWriter per spec).
	if _, err := sessionRef.Set(ctx, meta); err != nil {
		return fmt.Errorf("set session metadata: %w", err)
	}

	if len(gens) == 0 {
		return nil
	}

	bw := fs.client.BulkWriter(ctx)
	jobs := make([]*firestore.BulkWriterJob, len(gens))
	for i, gen := range gens {
		docID := fmt.Sprintf("%06d", gen.Generation)
		ref := sessionRef.Collection("generations").Doc(docID)
		job, err := bw.Set(ref, gen)
		if err != nil {
			bw.End()
			return fmt.Errorf("enqueue generation %d: %w", gen.Generation, err)
		}
		jobs[i] = job
	}
	bw.End()

	for _, job := range jobs {
		if _, err := job.Results(); err != nil {
			return fmt.Errorf("bulk write generation: %w", err)
		}
	}
	return nil
}

func (fs *FirestoreStore) ListSessions(ctx context.Context) ([]SessionMeta, error) {
	iter := fs.client.Collection("sessions").OrderBy("created_at", firestore.Desc).Documents(ctx)
	defer iter.Stop()

	var sessions []SessionMeta
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list sessions: %w", err)
		}
		var meta SessionMeta
		if err := doc.DataTo(&meta); err != nil {
			return nil, fmt.Errorf("decode session metadata: %w", err)
		}
		sessions = append(sessions, meta)
	}

	if sessions == nil {
		sessions = []SessionMeta{}
	}
	return sessions, nil
}

func (fs *FirestoreStore) LoadSession(ctx context.Context, id string) (SessionMeta, []GenerationSnapshot, error) {
	sessionRef := fs.client.Collection("sessions").Doc(id)

	doc, err := sessionRef.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return SessionMeta{}, nil, ErrSessionNotFound
		}
		return SessionMeta{}, nil, fmt.Errorf("get session: %w", err)
	}

	var meta SessionMeta
	if err := doc.DataTo(&meta); err != nil {
		return SessionMeta{}, nil, fmt.Errorf("decode session metadata: %w", err)
	}

	iter := sessionRef.Collection("generations").OrderBy("generation", firestore.Asc).Documents(ctx)
	defer iter.Stop()

	var gens []GenerationSnapshot
	for {
		genDoc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return SessionMeta{}, nil, fmt.Errorf("list generations: %w", err)
		}
		var gen GenerationSnapshot
		if err := genDoc.DataTo(&gen); err != nil {
			return SessionMeta{}, nil, fmt.Errorf("decode generation: %w", err)
		}
		gens = append(gens, gen)
	}

	return meta, gens, nil
}

func (fs *FirestoreStore) DeleteSession(ctx context.Context, id string) error {
	sessionRef := fs.client.Collection("sessions").Doc(id)

	if _, err := sessionRef.Get(ctx); err != nil {
		if status.Code(err) == codes.NotFound {
			return ErrSessionNotFound
		}
		return fmt.Errorf("get session for delete: %w", err)
	}

	// Delete all generation docs with BulkWriter.
	bw := fs.client.BulkWriter(ctx)
	var jobs []*firestore.BulkWriterJob

	refIter := sessionRef.Collection("generations").DocumentRefs(ctx)
	for {
		ref, err := refIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			bw.End()
			return fmt.Errorf("iterate generation refs: %w", err)
		}
		job, err := bw.Delete(ref)
		if err != nil {
			bw.End()
			return fmt.Errorf("enqueue generation delete: %w", err)
		}
		jobs = append(jobs, job)
	}
	bw.End()

	for _, job := range jobs {
		if _, err := job.Results(); err != nil {
			return fmt.Errorf("bulk delete generation: %w", err)
		}
	}

	if _, err := sessionRef.Delete(ctx); err != nil {
		return fmt.Errorf("delete session metadata: %w", err)
	}
	return nil
}

// NoopStore is used when Firestore is disabled (GCP_PROJECT_ID not set).
// All store operations return ErrStoreUnavailable.
type NoopStore struct{}

func NewNoopStore() *NoopStore { return &NoopStore{} }

func (n *NoopStore) SaveSession(_ context.Context, _ SessionMeta, _ []GenerationSnapshot) error {
	return ErrStoreUnavailable
}
func (n *NoopStore) ListSessions(_ context.Context) ([]SessionMeta, error) {
	return nil, ErrStoreUnavailable
}
func (n *NoopStore) LoadSession(_ context.Context, _ string) (SessionMeta, []GenerationSnapshot, error) {
	return SessionMeta{}, nil, ErrStoreUnavailable
}
func (n *NoopStore) DeleteSession(_ context.Context, _ string) error {
	return ErrStoreUnavailable
}
func (n *NoopStore) Close() error { return nil }
