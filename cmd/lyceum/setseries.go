package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/magos/lyceum/internal/store"
)

// runSetSeries assigns a series name and reading order to already-ingested books
// (LYCM-36). Series roll-up normally reads the series from EPUB OPF metadata, but
// most store-bought EPUBs carry none — so this tool lets you tag a set by hand,
// keyed on the book IDs shown in the library. Auto-detection still applies to any
// future EPUBs that do carry metadata; this only fills the gaps.
//
//	# Tag three books as volumes 1..N of a series, in the order given:
//	lyceum set-series --name "The Broken Empire" 20 18 19
//
//	# Set an explicit index for a single book:
//	lyceum set-series --name "Dragonlance Legends" --index 2 17
//
//	# Clear a book's series:
//	lyceum set-series --clear 21
//
//	# Preview without writing:
//	lyceum set-series --name "..." --dry-run 20 18 19
//
// Config (DB, data dir) comes from the same env as the server.
func runSetSeries(args []string) {
	fs := flag.NewFlagSet("set-series", flag.ExitOnError)
	name := fs.String("name", "", "series name to assign to the given book IDs")
	index := fs.Float64("index", 0, "explicit series index; requires exactly one book ID (default: auto-number 1..N in arg order)")
	clear := fs.Bool("clear", false, "clear the series on the given book IDs instead of setting one")
	dryRun := fs.Bool("dry-run", false, "report what would change without writing")
	_ = fs.Parse(args)

	ids := make([]int64, 0, len(fs.Args()))
	for _, a := range fs.Args() {
		id, err := strconv.ParseInt(a, 10, 64)
		if err != nil {
			log.Fatalf("set-series: invalid book id %q", a)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		log.Fatalf("set-series: no book IDs given (usage: set-series --name \"Series\" <id> [id...])")
	}

	switch {
	case *clear:
		if *name != "" || fs.Lookup("index").Value.String() != "0" {
			log.Fatalf("set-series: --clear takes only book IDs, not --name/--index")
		}
	case strings.TrimSpace(*name) == "":
		log.Fatalf("set-series: --name is required (or use --clear)")
	case fs.Lookup("index").Value.String() != "0" && len(ids) != 1:
		log.Fatalf("set-series: --index requires exactly one book ID")
	}

	cfg := loadBackfillConfig()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	pool, err := store.Connect(ctx, cfg.databaseURL)
	if err != nil {
		log.Fatalf("set-series: connect database: %v", err)
	}
	defer pool.Close()
	st := store.New(pool, cfg.dataDir)

	var updated, failed int
	for i, id := range ids {
		series := ""
		var idx float64
		if !*clear {
			series = strings.TrimSpace(*name)
			// An explicit --index overrides; otherwise number in argument order.
			if fs.Lookup("index").Value.String() != "0" {
				idx = *index
			} else {
				idx = float64(i + 1)
			}
		}

		if *dryRun {
			if *clear {
				log.Printf("set-series: book %d: would clear series", id)
			} else {
				log.Printf("set-series: book %d: would set series=%q index=%g", id, series, idx)
			}
			updated++
			continue
		}

		b, err := st.UpdateBookSeries(ctx, id, series, idx)
		if errors.Is(err, store.ErrNotFound) {
			log.Printf("set-series: book %d: not found; skipping", id)
			failed++
			continue
		}
		if err != nil {
			log.Printf("set-series: book %d: %v", id, err)
			failed++
			continue
		}
		if *clear {
			log.Printf("set-series: book %d (%q): series cleared", b.ID, b.Title)
		} else {
			log.Printf("set-series: book %d (%q): series=%q index=%g", b.ID, b.Title, b.Series, b.SeriesIndex)
		}
		updated++
	}

	log.Printf("set-series: done — %d updated, %d failed", updated, failed)
	if *dryRun {
		log.Printf("set-series: dry run; nothing was written")
	}
}
