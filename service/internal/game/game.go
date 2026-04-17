package game

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Game holds the live in-memory state of a Conway's Game of Life simulation.
// Exported fields are readable directly by tests and JSON-encoded by Snapshot().
// history[n] is the board state ([][]uint16) at generation n.
type Game struct {
	ID         string     `json:"id"`
	Width      int        `json:"width"`
	Height     int        `json:"height"`
	Generation int        `json:"generation"`
	Cells      [][]uint16 `json:"cells"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`

	// Pointer avoids "copies lock value" vet error when Game is JSON-decoded into a value.
	mu      *sync.Mutex
	history [][][]uint16
}

// GameState is a JSON-safe snapshot; it duplicates Game's exported shape so
// handlers can return it without holding the Game's mutex.
type GameState struct {
	ID         string     `json:"id"`
	Width      int        `json:"width"`
	Height     int        `json:"height"`
	Generation int        `json:"generation"`
	Cells      [][]uint16 `json:"cells"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// NewGame creates a game with the given dimensions and optional initial live cells.
// Returns an error if any cell coordinate is out of bounds.
func NewGame(width, height int, liveCells [][2]int) (*Game, error) {
	cells := make([][]uint16, height)
	for y := range cells {
		cells[y] = make([]uint16, width)
	}
	for _, c := range liveCells {
		x, y := c[0], c[1]
		if x < 0 || x >= width || y < 0 || y >= height {
			return nil, fmt.Errorf("cell [%d,%d] is out of bounds for %dx%d board", x, y, width, height)
		}
		cells[y][x] = 1
	}
	now := time.Now().UTC()
	g := &Game{
		ID:        uuid.NewString(),
		Width:     width,
		Height:    height,
		Cells:     cells,
		CreatedAt: now,
		UpdatedAt: now,
		mu:        new(sync.Mutex),
	}
	// history[0] = initial state; double-buffer in Step() makes this safe without copying.
	g.history = append(g.history, g.Cells)
	return g, nil
}

// Snapshot returns a copy of the current game state for safe JSON encoding.
func (g *Game) Snapshot() GameState {
	g.mu.Lock()
	defer g.mu.Unlock()
	return GameState{
		ID:         g.ID,
		Width:      g.Width,
		Height:     g.Height,
		Generation: g.Generation,
		Cells:      copyGrid(g.Cells, g.Width, g.Height),
		CreatedAt:  g.CreatedAt,
		UpdatedAt:  g.UpdatedAt,
	}
}

// Step advances the game by one generation (B3/S23, hard edges) and returns the new state.
// Also updates the exported Cells / Generation / UpdatedAt fields so tests can read them directly.
func (g *Game) Step() GameState {
	g.mu.Lock()
	defer g.mu.Unlock()

	next := make([][]uint16, g.Height)
	for y := range next {
		next[y] = make([]uint16, g.Width)
	}

	for y := 0; y < g.Height; y++ {
		for x := 0; x < g.Width; x++ {
			n := countNeighbors(g.Cells, x, y, g.Width, g.Height)
			cur := g.Cells[y][x]
			if cur > 0 {
				if n == 2 || n == 3 {
					age := cur + 1
					if age == 0 { // uint16 wrap: cap at 65535
						age = 65535
					}
					next[y][x] = age
				}
			} else if n == 3 {
				next[y][x] = 1
			}
		}
	}

	g.Cells = next
	g.Generation++
	g.UpdatedAt = time.Now().UTC()
	// Double-buffer: each Step allocates a fresh next grid, so storing g.Cells directly
	// in history is safe — future steps never mutate previous allocations.
	g.history = append(g.history, g.Cells)

	return GameState{
		ID:         g.ID,
		Width:      g.Width,
		Height:     g.Height,
		Generation: g.Generation,
		Cells:      copyGrid(g.Cells, g.Width, g.Height),
		CreatedAt:  g.CreatedAt,
		UpdatedAt:  g.UpdatedAt,
	}
}

// SaveInfo returns dimensions, current generation, and a full copy of history under lock.
type SaveInfo struct {
	ID         string
	Width      int
	Height     int
	Generation int
	History    [][][]uint16
}

func (g *Game) SaveInfo() SaveInfo {
	g.mu.Lock()
	defer g.mu.Unlock()

	histCopy := make([][][]uint16, len(g.history))
	for i, snap := range g.history {
		histCopy[i] = copyGrid(snap, g.Width, g.Height)
	}
	return SaveInfo{
		ID:         g.ID,
		Width:      g.Width,
		Height:     g.Height,
		Generation: g.Generation,
		History:    histCopy,
	}
}

func countNeighbors(cells [][]uint16, x, y, width, height int) int {
	n := 0
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			nx, ny := x+dx, y+dy
			if nx >= 0 && nx < width && ny >= 0 && ny < height && cells[ny][nx] > 0 {
				n++
			}
		}
	}
	return n
}

func copyGrid(src [][]uint16, width, height int) [][]uint16 {
	dst := make([][]uint16, height)
	for y := range dst {
		dst[y] = make([]uint16, width)
		copy(dst[y], src[y])
	}
	return dst
}
