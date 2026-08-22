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

	"github.com/magos/lyceum/internal/acquire"
	"github.com/magos/lyceum/internal/api"
	"github.com/magos/lyceum/internal/coverart"
	"github.com/magos/lyceum/internal/delivery"
	"github.com/magos/lyceum/internal/edition"
	"github.com/magos/lyceum/internal/invite"
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

	// Cover art (LYCM-56): at ingest, prefer canonical cover art fetched by ISBN
	// over the EPUB's embedded cover, which is often poor or a title page.
	coverFetch     bool   // LYCEUM_COVER_FETCH — enable external cover fetching (default true)
	coverBaseURL   string // LYCEUM_COVER_BASE_URL — override the Open Library base (testing/self-host)
	coverNormalize bool   // LYCEUM_COVER_NORMALIZE — trim/aspect/downscale stored covers at ingest (default true)
	ingestQC       bool   // LYCEUM_INGEST_QC — hold flagged new ingests for review (default true)

	// ISBN ingest (LYCM-603): resolve scanned ISBNs / titles to candidate
	// editions during batch review. Metadata-only (Open Library); distinct from
	// the acquisition backend that obtains the actual EPUB.
	editionResolve bool   // LYCEUM_EDITION_RESOLVE — enable edition matching (default true)
	editionBaseURL string // LYCEUM_EDITION_BASE_URL — override the Open Library search base (testing/self-host)

	// Acquisition (LYCM-35): the live Bindery backend that turns a `wanted` ISBN
	// into a real grab. Disabled (falls back to a logging no-op) unless both the
	// base URL and API key are set.
	binderyBaseURL string // LYCEUM_BINDERY_BASE_URL — e.g. http://localhost:8787
	binderyAPIKey  string // LYCEUM_BINDERY_API_KEY — Bindery Settings → Security

	// Accounts (LYCM-801). userAuth gates the reader core behind a session token.
	// It defaults OFF: the clients don't sign in yet, and turning it on before
	// they do would lock out every existing install. Flip it once they ship.
	userAuth   bool   // LYCEUM_AUTH — require a signed-in user on the reader core
	ownerEmail string // LYCEUM_OWNER_EMAIL — identity of the owner account
	ownerName  string // LYCEUM_OWNER_NAME — the owner's display name

	// Cloudflare Access SSO (LYCM-803). Both must be set to enable the browser
	// auto-sign-in endpoint; either unset leaves it returning sso_disabled. They
	// are public identifiers (not secrets): the team domain and the Access
	// application's AUD tag from the Zero Trust dashboard.
	cfAccessTeamDomain string // CF_ACCESS_TEAM_DOMAIN — e.g. <team>.cloudflareaccess.com
	cfAccessAUD        string // CF_ACCESS_AUD — Access application audience tag

	// Cross-device sign-in (LYCM-88): the library's public web origin, used to
	// turn an invite into a scannable `<publicURL>/sign-in?token=…` QR. Optional;
	// when unset, mint-token just prints the raw token as before.
	publicURL string // LYCEUM_PUBLIC_URL — e.g. http://192.168.1.9:8080

	// The origin the *mobile app* can reach (LYCM-102), which is not necessarily
	// the one the owner is looking at. Where the browser sits behind Cloudflare
	// Access, an invite QR built from the browser's own origin sends the phone
	// straight into an SSO wall it has no way past; this is the direct,
	// bearer-authenticated hostname instead. When set, minted invites carry a
	// ready-made sign_in_url built from it. Optional and deliberately separate
	// from publicURL: that one addresses the browser, and defaulting to it would
	// reintroduce the exact gated-origin bug this fixes.
	mobileBaseURL string // LYCEUM_MOBILE_BASE_URL — e.g. https://lyceum-direct.example.com

	// Phase 4 (LYCM-400) ecosystem config, env-only.
	apiTokens      string          // LYCEUM_API_TOKENS — bearer tokens for /eidolon + delivery (LYCM-405)
	smtp           delivery.Config // LYCEUM_SMTP_* — "Send to Kindle" relay (LYCM-401)
	kindleAddr     string          // LYCEUM_KINDLE_ADDR — default delivery recipient
	kindleAutoSend bool            // LYCEUM_KINDLE_AUTO_SEND — deliver every upload (LYCM-402)
}

// envConfig resolves the server config from the environment. Flags layer on
// top of it (see bindFlags and loadConfig).
func envConfig() config {
	return config{
		addr:        envOr("LYCEUM_ADDR", ":8080"),
		databaseURL: firstEnv([]string{"LYCEUM_DATABASE_URL", "DATABASE_URL"}, defaultDatabaseURL),
		dataDir:     envOr("LYCEUM_DATA_DIR", "data/blobs"),

		booksWatchDir:      os.Getenv("LYCEUM_BOOKS_WATCH_DIR"),
		booksWatchInterval: envOrInt("LYCEUM_BOOKS_WATCH_INTERVAL", 15),

		corsOrigins: os.Getenv("LYCEUM_CORS_ORIGINS"),

		coverFetch:     envBool("LYCEUM_COVER_FETCH", true),
		coverBaseURL:   os.Getenv("LYCEUM_COVER_BASE_URL"),
		coverNormalize: envBool("LYCEUM_COVER_NORMALIZE", true),
		ingestQC:       envBool("LYCEUM_INGEST_QC", true),

		editionResolve: envBool("LYCEUM_EDITION_RESOLVE", true),
		editionBaseURL: os.Getenv("LYCEUM_EDITION_BASE_URL"),

		binderyBaseURL: os.Getenv("LYCEUM_BINDERY_BASE_URL"),
		binderyAPIKey:  os.Getenv("LYCEUM_BINDERY_API_KEY"),

		userAuth:      envBool("LYCEUM_AUTH", false),
		ownerEmail:    os.Getenv("LYCEUM_OWNER_EMAIL"),
		ownerName:     os.Getenv("LYCEUM_OWNER_NAME"),
		publicURL:     os.Getenv("LYCEUM_PUBLIC_URL"),
		mobileBaseURL: os.Getenv("LYCEUM_MOBILE_BASE_URL"),

		cfAccessTeamDomain: os.Getenv("CF_ACCESS_TEAM_DOMAIN"),
		cfAccessAUD:        os.Getenv("CF_ACCESS_AUD"),

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
}

// loadConfig resolves the config from the environment and lets command-line
// flags override it.
func loadConfig() config {
	c := envConfig()
	apply := bindFlags(flag.CommandLine, &c)
	flag.Parse()
	apply()
	return c
}

// bindFlags registers the server's flags on fs, seeded from the already
// env-resolved c, and returns the func that applies whatever flag could not
// write straight into c. Call it after fs.Parse.
//
// -database-url is deliberately registered with an *empty* default instead of
// the resolved DSN (LYCM-119). flag prints every flag's default in its usage
// output, so seeding that one from config turns `lyceum --help` into a
// credential disclosure needing no privilege beyond running the binary — and
// from there the password lands in support requests, CI logs, screen shares and
// agent transcripts. The usage text names the whole fallback chain instead, so
// an operator can still see where the value comes from — including, for one
// with neither env var set, the value they are actually getting. Naming
// defaultDatabaseURL there is only safe because that constant carries no
// password, which is why a test holds it to that. addr and data-dir hold no
// secret at all, so they go on showing their effective value.
func bindFlags(fs *flag.FlagSet, c *config) func() {
	fs.StringVar(&c.addr, "addr", c.addr, "HTTP listen address")
	databaseURL := fs.String("database-url", "",
		"Postgres connection string (default: $LYCEUM_DATABASE_URL, else $DATABASE_URL, else "+defaultDatabaseURL+")")
	fs.StringVar(&c.dataDir, "data-dir", c.dataDir, "blob storage directory")
	return func() {
		if *databaseURL != "" {
			c.databaseURL = *databaseURL
		}
	}
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
	opts := []api.Option{api.WithAuth(auth), api.WithUserAuth(cfg.userAuth)}
	if cfg.userAuth {
		log.Printf("auth: reader core requires a signed-in user")
	} else {
		log.Printf("config: user auth disabled (LYCEUM_AUTH); the reader core is open and every request is served as the owner")
	}

	// Cloudflare Access SSO (LYCM-803): enable browser auto-sign-in only when
	// both the team domain and the AUD tag are configured. Either missing leaves
	// the endpoint returning sso_disabled and clients fall back to invite sign-in.
	if cfg.cfAccessTeamDomain != "" && cfg.cfAccessAUD != "" {
		opts = append(opts, api.WithCFAccess(api.NewCFAccessVerifier(cfg.cfAccessTeamDomain, cfg.cfAccessAUD)))
		log.Printf("auth: Cloudflare Access SSO enabled (team=%s)", cfg.cfAccessTeamDomain)
	} else if cfg.cfAccessTeamDomain != "" || cfg.cfAccessAUD != "" {
		log.Printf("config: Cloudflare Access SSO needs both CF_ACCESS_TEAM_DOMAIN and CF_ACCESS_AUD; SSO disabled")
	}

	// The origin invites advertise to phones (LYCM-102). A bad value is worse than
	// none — clients prefer it over the URL they would have built, so it would
	// suppress a working fallback — hence refuse it here and say why, rather than
	// pass it through to produce QRs that scan nowhere.
	if mobileBase, err := invite.NormalizeBase(cfg.mobileBaseURL); err != nil {
		log.Printf("config: ignoring LYCEUM_MOBILE_BASE_URL — %v; invites will carry no sign-in URL "+
			"and clients will build one from the origin they know", err)
	} else {
		opts = append(opts, api.WithMobileBaseURL(mobileBase))
		switch {
		case mobileBase != "":
			log.Printf("auth: invites advertise %s for sign-in", mobileBase)
		case cfg.userAuth:
			// Only worth mentioning where invites can actually be minted: with auth
			// off every route that mints one returns 403, so on a default install
			// this would be a line of noise about a feature that cannot run.
			log.Printf("config: LYCEUM_MOBILE_BASE_URL unset; invites carry no sign-in URL and clients build one from the origin they know")
		}
	}

	if cfg.coverFetch {
		opts = append(opts, api.WithCoverFetcher(newCoverFetcher(cfg)))
		log.Printf("cover fetch enabled at ingest (source=%s)", coverSource(cfg))
	}

	opts = append(opts, api.WithCoverNormalize(cfg.coverNormalize))
	if !cfg.coverNormalize {
		log.Printf("config: cover normalization disabled (LYCEUM_COVER_NORMALIZE); storing covers verbatim")
	}

	opts = append(opts, api.WithIngestQC(cfg.ingestQC))
	if cfg.ingestQC {
		log.Printf("ingest QC enabled: flagged new ingests held for review")
	} else {
		log.Printf("config: ingest QC disabled (LYCEUM_INGEST_QC); all ingests publish immediately")
	}

	if cfg.editionResolve {
		opts = append(opts, api.WithResolver(newEditionResolver(cfg)))
		log.Printf("edition resolve enabled for ISBN ingest (source=%s)", editionSource(cfg))
	}

	if cfg.binderyBaseURL != "" && cfg.binderyAPIKey != "" {
		opts = append(opts, api.WithAcquirer(acquire.NewBindery(cfg.binderyBaseURL, cfg.binderyAPIKey)))
		log.Printf("bindery acquirer enabled (base=%s)", cfg.binderyBaseURL)
	} else if cfg.binderyBaseURL != "" || cfg.binderyAPIKey != "" {
		log.Printf("config: Bindery acquirer needs both LYCEUM_BINDERY_BASE_URL and LYCEUM_BINDERY_API_KEY; using no-op acquirer")
	} else {
		log.Printf("config: no Bindery acquirer configured (LYCEUM_BINDERY_BASE_URL/LYCEUM_BINDERY_API_KEY unset); find_digital marks inventory `wanted` but grabs nothing")
	}

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

// newCoverFetcher builds the cover fetcher. Apple Books (the iTunes Search API)
// is the source: keyless, matches by title+author so it covers ISBN-less EPUBs,
// and returns clean high-resolution covers. An optional base-URL override
// (LYCEUM_COVER_BASE_URL) points it at a mirror or test server.
func newCoverFetcher(cfg config) coverart.Fetcher {
	f := coverart.NewAppleBooks()
	if cfg.coverBaseURL != "" {
		f.SearchBaseURL = cfg.coverBaseURL
	}
	return f
}

func coverSource(cfg config) string {
	if cfg.coverBaseURL != "" {
		return cfg.coverBaseURL
	}
	return "applebooks"
}

// newEditionResolver builds the ISBN/title edition resolver for batch ingest
// (LYCM-603). Open Library is the source: keyless and matches by ISBN or title.
// An optional base-URL override (LYCEUM_EDITION_BASE_URL) points it at a mirror
// or test server.
func newEditionResolver(cfg config) api.Resolver {
	r := edition.NewOpenLibrary()
	if cfg.editionBaseURL != "" {
		r.SearchBaseURL = cfg.editionBaseURL
	}
	return r
}

func editionSource(cfg config) string {
	if cfg.editionBaseURL != "" {
		return cfg.editionBaseURL
	}
	return "openlibrary"
}

func main() {
	// Subcommands run to completion and exit; the bare command runs the server.
	if len(os.Args) > 1 && os.Args[1] == "backfill-covers" {
		runBackfillCovers(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "find-duplicates" {
		runFindDuplicates(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "set-series" {
		runSetSeries(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "mint-token" {
		runMintToken(os.Args[2:])
		return
	}

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

	// Accounts (LYCM-801): migration 0011 seeds the owner row; this lands the
	// operator's configured identity on it and prints a first sign-in invite when
	// nobody can get in yet.
	bootstrapOwner(ctx, st, cfg)

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
