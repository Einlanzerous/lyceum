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
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/magos/lyceum/internal/api"
	"github.com/magos/lyceum/internal/delivery"
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

	// Folder ingest (LYCM-601): watch the acquisition stack's book library and
	// ingest new EPUBs. Disabled when booksWatchDir is empty.
	booksWatchDir      string // LYCEUM_BOOKS_WATCH_DIR — e.g. /data/media/books
	booksWatchInterval int    // LYCEUM_BOOKS_WATCH_INTERVAL — poll seconds

	// Cross-platform wrappers (LYCM-300): extra CORS origins the Wails desktop
	// shell calls from. The built-in native origins are always allowed; this
	// extends them (or "*" allows any).
	corsOrigins string // LYCEUM_CORS_ORIGINS — comma-separated

	// Phase 4 (LYCM-400) ecosystem config, env-only.
	apiTokens      string          // LYCEUM_API_TOKENS — bearer tokens for /eidolon + delivery (LYCM-405)
	smtp           delivery.Config // LYCEUM_SMTP_* — "Send to Kindle" relay (LYCM-401)
	kindleAddr     string          // LYCEUM_KINDLE_ADDR — default delivery recipient
	kindleAutoSend bool            // LYCEUM_KINDLE_AUTO_SEND — deliver every upload (LYCM-402)
}

func loadConfig() config {
	c := config{
		addr:        envOr("LYCEUM_ADDR", ":8080"),
		databaseURL: firstEnv([]string{"LYCEUM_DATABASE_URL", "DATABASE_URL"}, defaultDatabaseURL),
		dataDir:     envOr("LYCEUM_DATA_DIR", "data/blobs"),

		booksWatchDir:      os.Getenv("LYCEUM_BOOKS_WATCH_DIR"),
		booksWatchInterval: envOrInt("LYCEUM_BOOKS_WATCH_INTERVAL", 15),

		corsOrigins: os.Getenv("LYCEUM_CORS_ORIGINS"),

		apiTokens: os.Getenv("LYCEUM_API_TOKENS"),
		smtp: delivery.Config{
			Host:     os.Getenv("LYCEUM_SMTP_HOST"),
			Port:     envOrInt("LYCEUM_SMTP_PORT", 587),
			Username: os.Getenv("LYCEUM_SMTP_USERNAME"),
			Password: os.Getenv("LYCEUM_SMTP_PASSWORD"),
			From:     os.Getenv("LYCEUM_SMTP_FROM"),
			TLS:      envOr("LYCEUM_SMTP_TLS", delivery.TLSStartTLS),
		},
		kindleAddr:     os.Getenv("LYCEUM_KINDLE_ADDR"),
		kindleAutoSend: envBool("LYCEUM_KINDLE_AUTO_SEND", false),
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

func envOrInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		log.Printf("config: %s=%q is not an integer; using %d", key, os.Getenv(key), def)
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
		log.Printf("config: %s=%q is not a bool; using %v", key, os.Getenv(key), def)
	}
	return def
}

// buildAPIOptions assembles the api.Options from config: the bearer-token table
// (LYCM-405) and, when an SMTP relay is configured, the "Send to Kindle"
// dispatcher (LYCM-401/402). It returns a cleanup func to drain the dispatcher
// on shutdown.
func buildAPIOptions(cfg config, st *store.Store) ([]api.Option, func()) {
	cleanup := func() {}

	auth, err := api.ParseTokens(cfg.apiTokens)
	if err != nil {
		log.Fatalf("parse LYCEUM_API_TOKENS: %v", err)
	}
	opts := []api.Option{api.WithAuth(auth)}

	if cfg.smtp.Host == "" || cfg.smtp.From == "" {
		if cfg.kindleAutoSend || cfg.kindleAddr != "" {
			log.Printf("config: Kindle delivery requested but LYCEUM_SMTP_HOST/FROM unset; send-to-kindle disabled")
		}
		return opts, cleanup
	}

	sender, err := delivery.New(cfg.smtp)
	if err != nil {
		log.Fatalf("configure SMTP delivery: %v", err)
	}
	dispatcher := api.NewDispatcher(st, sender, 2, 60*time.Second)
	cleanup = dispatcher.Close
	opts = append(opts, api.WithDeliveries(dispatcher, cfg.kindleAddr, cfg.kindleAutoSend))
	log.Printf("send-to-kindle enabled via %s (auto-send=%v)", cfg.smtp.Host, cfg.kindleAutoSend)
	return opts, cleanup
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

	opts, cleanup := buildAPIOptions(cfg, st)
	defer cleanup()

	// The API registers its specific routes (/upload, /library, /sync,
	// /books/{id}/..., /eidolon/...) plus /healthz; the embedded SPA is the "/"
	// catch-all. Go 1.22's ServeMux prefers the more specific pattern, so API
	// and asset requests win and only unmatched paths (e.g. /reader/1 deep
	// links) fall through to index.html for client-side routing.
	apiSrv := api.New(st, cfg.dataDir, opts...)
	mux := apiSrv.Handler()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"lyceum"}`))
	})
	mux.Handle("/", web.Handler())

	// Cross-platform wrappers (LYCM-300): the Wails desktop shell calls this
	// server from a different origin (wails.localhost), so wrap the whole mux
	// with CORS. The web build is same-origin and unaffected (it sends no
	// Origin). Preflights are answered here before the method-specific routes
	// can 405 them.
	corsOrigins := api.ParseCORSOrigins(cfg.corsOrigins)
	handler := api.CORS(corsOrigins, mux)

	// Folder ingest: poll the acquisition stack's book library and ingest new
	// EPUBs through the same path as /upload. Its context is cancelled on
	// shutdown so the poller returns cleanly.
	watchCtx, stopWatch := context.WithCancel(context.Background())
	defer stopWatch()
	if cfg.booksWatchDir != "" {
		watcher := api.NewWatcher(apiSrv, cfg.booksWatchDir,
			time.Duration(cfg.booksWatchInterval)*time.Second)
		go watcher.Run(watchCtx)
	}

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Serve in the background so main can wait for a shutdown signal and then
	// return normally — running the deferred cleanup() (which drains the
	// delivery dispatcher) and pool.Close(). A fatal listen error short-circuits
	// shutdown via the signal context.
	shutdown, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("lyceum listening on %s (data=%s)", cfg.addr, cfg.dataDir)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("server error: %v", err)
			stop() // unblock main; deferred cleanup still runs
		}
	}()

	<-shutdown.Done()
	log.Printf("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown: %v", err)
	}
}
