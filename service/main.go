package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"conway-life/internal/config"
	"conway-life/internal/server"
	"conway-life/internal/store"
)

func main() {
	cfg := config.Load()

	ctx := context.Background()

	var st store.Store
	if cfg.ProjectID != "" {
		fs, err := store.NewFirestoreStore(ctx, cfg.ProjectID, cfg.FirestoreDatabaseName)
		if err != nil {
			log.Fatalf("create Firestore store: %v", err)
		}
		defer func() {
			if err := fs.Close(); err != nil {
				log.Printf("close Firestore store: %v", err)
			}
		}()
		st = fs
	} else {
		log.Println("GCP_PROJECT_ID not set — Firestore disabled; save/load endpoints return 503")
		st = store.NewNoopStore()
	}

	srv := server.New(cfg, st)
	mux := srv.SetupRoutes()

	httpSrv := &http.Server{
		Addr:    fmt.Sprintf(":%s", cfg.Port),
		Handler: mux,
	}

	done := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutCtx); err != nil {
			log.Printf("HTTP shutdown: %v", err)
		}
		close(done)
	}()

	log.Printf("starting conway-life %s (%s) on :%s", cfg.Version, cfg.Environment, cfg.Port)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("HTTP server: %v", err)
	}
	<-done
}
