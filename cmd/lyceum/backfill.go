package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"path/filepath"
	"strconv"
	"time"

	"github.com/magos/lyceum/internal/coverart"
	"github.com/magos/lyceum/internal/epub"
	"github.com/magos/lyceum/internal/isbn"
	"github.com/magos/lyceum/internal/store"
)

// runBackfillCovers re-derives covers for already-ingested books from an
// external source (Open Library), writing the result to each book's served
// cover location. It exists because covers are extracted once at ingest, so a
// book with a broken embedded cover (e.g. a publisher title page) keeps it.
//
// External art is a mixed bag — Open Library often only has a lower-resolution
// or wrong-edition image — so this is deliberately NOT aggressive by default:
//
//	lyceum backfill-covers                 # only books with NO cover (safe)
//	lyceum backfill-covers 10 42           # exactly these book IDs (fix a bad cover)
//	lyceum backfill-covers --all           # every book (may downgrade good covers)
//	lyceum backfill-covers --dry-run ...    # preview without writing
//
// Config (DB, data dir, cover base URL) comes from the same env as the server.
// It is safe to re-run: fetching is idempotent and only books with a resolvable
// ISBN/title and available art are touched.
//
// Note: covers are served with a long immutable Cache-Control, so already-loaded
// clients may keep showing the old image until their cache is cleared / hard-refreshed.
func runBackfillCovers(args []string) {
	fs := flag.NewFlagSet("backfill-covers", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "report what would change without writing any covers")
	all := fs.Bool("all", false, "consider every book, replacing existing covers (may downgrade good ones)")
	_ = fs.Parse(args)

	// Any positional args are book IDs to target explicitly (replaced regardless
	// of whether they already have a cover).
	targetIDs := map[int64]bool{}
	for _, a := range fs.Args() {
		id, err := strconv.ParseInt(a, 10, 64)
		if err != nil {
			log.Fatalf("backfill: invalid book id %q", a)
		}
		targetIDs[id] = true
	}
	if len(targetIDs) > 0 && *all {
		log.Fatalf("backfill: pass book IDs or --all, not both")
	}

	cfg := loadBackfillConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pool, err := store.Connect(ctx, cfg.databaseURL)
	if err != nil {
		log.Fatalf("backfill: connect database: %v", err)
	}
	defer pool.Close()
	st := store.New(pool, cfg.dataDir)
	fetcher := newCoverFetcher(cfg)

	books, err := st.ListBooks(ctx)
	if err != nil {
		log.Fatalf("backfill: list books: %v", err)
	}
	mode := "only-missing"
	if len(targetIDs) > 0 {
		mode = "targeted"
	} else if *all {
		mode = "all"
	}
	log.Printf("backfill: %d books; mode=%s dry-run=%v source=%s",
		len(books), mode, *dryRun, coverSource(cfg))

	var updated, skipped, failed int
	for _, b := range books {
		// Selection: targeted IDs win; else --all covers everything; else only
		// books that currently have no cover.
		switch {
		case len(targetIDs) > 0:
			if !targetIDs[b.ID] {
				continue
			}
		case *all:
			// consider every book
		case b.CoverPath != "":
			skipped++
			continue
		}

		md, err := epub.ParseFile(b.FilePath)
		if err != nil {
			log.Printf("backfill: book %d (%q): parse epub: %v", b.ID, b.Title, err)
			failed++
			continue
		}
		// Prefer the stored book's title/author for the search (they are the
		// canonical, cleaned values shown in the library and match Open Library
		// better than the raw OPF metadata), but take ISBNs and language from the
		// EPUB where they live.
		q := coverart.Query{
			Title:    b.Title,
			Author:   b.Author,
			Language: md.Language,
			ISBNs:    isbn.AllFrom(md.Identifiers),
		}
		if len(q.ISBNs) == 0 && q.Title == "" {
			log.Printf("backfill: book %d (%q): no ISBN or title to search; skipping", b.ID, b.Title)
			skipped++
			continue
		}

		art, _, err := fetcher.Fetch(ctx, q)
		switch {
		case errors.Is(err, coverart.ErrNotFound):
			log.Printf("backfill: book %d (%q): no cover found; skipping", b.ID, b.Title)
			skipped++
			continue
		case err != nil:
			log.Printf("backfill: book %d (%q): fetch: %v", b.ID, b.Title, err)
			failed++
			continue
		}

		if *dryRun {
			log.Printf("backfill: book %d (%q): would set cover (%d bytes)", b.ID, b.Title, len(art))
			updated++
			continue
		}

		// Write to the book's actual served cover location. Prefer the recorded
		// cover_path; otherwise place cover.bin alongside the EPUB blob (which is
		// what the reader actually serves). Deriving the path from file_hash is
		// unsafe: a book's recorded hash may not match its blob directory.
		coverPath := b.CoverPath
		if coverPath == "" {
			coverPath = filepath.Join(filepath.Dir(b.FilePath), "cover.bin")
		}
		if err := st.SaveCoverAt(coverPath, art); err != nil {
			log.Printf("backfill: book %d (%q): save cover: %v", b.ID, b.Title, err)
			failed++
			continue
		}
		if b.CoverPath == "" {
			if err := st.SetCoverPath(ctx, b.ID, coverPath); err != nil {
				log.Printf("backfill: book %d (%q): set cover path: %v", b.ID, b.Title, err)
				failed++
				continue
			}
		}
		log.Printf("backfill: book %d (%q): cover updated (%d bytes)", b.ID, b.Title, len(art))
		updated++
	}

	log.Printf("backfill: done — %d updated, %d skipped, %d failed", updated, skipped, failed)
	if *dryRun {
		log.Printf("backfill: dry run; no covers were written")
	}
}

// loadBackfillConfig reads just the config the backfill needs (DB, data dir,
// cover source) from env, without touching the server's flag set.
func loadBackfillConfig() config {
	return config{
		databaseURL:  firstEnv([]string{"LYCEUM_DATABASE_URL", "DATABASE_URL"}, defaultDatabaseURL),
		dataDir:      envOr("LYCEUM_DATA_DIR", "data/blobs"),
		coverBaseURL: envOr("LYCEUM_COVER_BASE_URL", ""),
	}
}
