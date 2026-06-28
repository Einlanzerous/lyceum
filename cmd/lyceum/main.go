// Command lyceum is the entrypoint for the Lyceum ebook server.
//
// This is the Phase 1 (LYCM-100) kickoff skeleton: it boots an HTTP server
// with a /healthz route. Subsequent tickets wire in the Postgres store
// (LYCM-102/103), EPUB parsing (LYCM-104/108), and the REST API surface
// (LYCM-105 /upload, LYCM-106 /library, LYCM-107 /sync).
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/magos/lyceum/internal/api"
	"github.com/magos/lyceum/internal/store"
	"github.com/magos/lyceum/web"
)

// defaultDatabaseURL points at the lyceum database on the shared
// construct-server Postgres. The password is supplied out of band via the
// DATABASE_URL env (see .env) or PGPASSWORD, so it stays out of source.
const defaultDatabaseURL = "postgres://lyceum_user@localhost:5432/lyceum?sslmode=disable"

type config struct {
	addr        string // HTTP listen address
	databaseURL string // Postgres connection string (pgx DSN)
	dataDir     string // blob storage (EPUBs + extracted covers)
}

func loadConfig() config {
	c := config{
		addr:        envOr("LYCEUM_ADDR", ":8080"),
		databaseURL: firstEnv([]string{"LYCEUM_DATABASE_URL", "DATABASE_URL"}, defaultDatabaseURL),
		dataDir:     envOr("LYCEUM_DATA_DIR", "data/blobs"),
	}
	flag.StringVar(&c.addr, "addr", c.addr, "HTTP listen address")
	flag.StringVar(&c.databaseURL, "database-url", c.databaseURL, "Postgres connection string")
	flag.StringVar(&c.dataDir, "data-dir", c.dataDir, "blob storage directory")
	flag.Parse()
	return c
}

func firstEnv(keys []string, def string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return def
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	cfg := loadConfig()

	ctx := context.Background()
	pool, err := store.Connect(ctx, cfg.databaseURL)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	st := store.New(pool, cfg.dataDir)

	// The API registers its specific routes (/upload, /library, /sync,
	// /books/{id}/...) plus /healthz; the embedded SPA is the "/" catch-all.
	// Go 1.22's ServeMux prefers the more specific pattern, so API and asset
	// requests win and only unmatched paths (e.g. /reader/1 deep links) fall
	// through to index.html for client-side routing.
	mux := api.New(st, cfg.dataDir).Handler()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"lyceum"}`))
	})
	mux.Handle("/", web.Handler())

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("lyceum listening on %s (data=%s)", cfg.addr, cfg.dataDir)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
