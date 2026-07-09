package api

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Watcher polls a directory recursively for EPUBs and ingests new ones through
// the shared ingestEPUB core. It bridges the acquisition stack (Bindery imports
// to /data/media/books in argosy-acquisition) into the Lyceum library: a
// correctly-named EPUB landing in the watched tree is picked up, validated,
// deduped, stored, and stamped with its ISBN — the same path as an HTTP upload.
//
// A single in-process poller is plenty for a personal library and avoids a
// platform-specific filesystem-notification dependency. Files are tracked by a
// (size, modtime) signature so a stable file is ingested once; a file still
// being written (whose signature keeps changing) is retried on a later tick,
// and a genuinely-invalid file is logged once per signature rather than every
// tick.
type Watcher struct {
	api      *API
	dir      string
	interval time.Duration

	// ok/bad are tracked across ticks (the poll loop is single-goroutine, so no
	// locking is needed): ok holds signatures already ingested, bad holds
	// signatures that failed to parse, so each is acted on at most once.
	ok  map[string]string
	bad map[string]string
}

// NewWatcher builds a Watcher over dir. interval defaults to 15s when not
// positive.
func NewWatcher(a *API, dir string, interval time.Duration) *Watcher {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	return &Watcher{
		api:      a,
		dir:      dir,
		interval: interval,
		ok:       map[string]string{},
		bad:      map[string]string{},
	}
}

// Run scans once immediately, then on every tick until ctx is cancelled. It is
// meant to be launched in its own goroutine; cancelling ctx returns it.
func (w *Watcher) Run(ctx context.Context) {
	log.Printf("watch: ingesting EPUBs from %s every %s", w.dir, w.interval)
	w.scanOnce(ctx)

	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.scanOnce(ctx)
		}
	}
}

// scanOnce walks the watched tree once and ingests any new or changed EPUBs.
// Per-file errors are logged and skipped; the walk continues.
func (w *Watcher) scanOnce(ctx context.Context) {
	err := filepath.WalkDir(w.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A vanished/locked entry mid-walk shouldn't abort the whole scan.
			log.Printf("watch: walk %s: %v", path, err)
			return nil
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".epub") {
			return nil
		}
		w.consider(ctx, path, d)
		return nil
	})
	if err != nil {
		log.Printf("watch: scan %s: %v", w.dir, err)
	}
}

// consider ingests path when its (size, modtime) signature has not already been
// ingested or recorded as bad.
func (w *Watcher) consider(ctx context.Context, path string, d fs.DirEntry) {
	info, err := d.Info()
	if err != nil {
		log.Printf("watch: stat %s: %v", path, err)
		return
	}
	sig := fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
	if w.ok[path] == sig || w.bad[path] == sig {
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("watch: read %s: %v", path, err)
		return // transient; retry next tick (signature not recorded)
	}

	// The file path is both the human label and the stable identity, so a
	// re-stamped file updates its book in place instead of duplicating.
	book, result, err := w.api.ingestEPUB(ctx, data, path, path)
	switch {
	case err != nil:
		// Likely a partial write (still copying) or a non-EPUB. Record the
		// signature so we don't re-log until the file changes.
		w.bad[path] = sig
		delete(w.ok, path)
		log.Printf("watch: ingest %s: %v", path, err)
	case result == ingestCreated:
		w.ok[path] = sig
		delete(w.bad, path)
		log.Printf("watch: ingested %s -> book %d", path, book.ID)
	case result == ingestReplaced:
		w.ok[path] = sig
		delete(w.bad, path)
		log.Printf("watch: re-stamped %s -> updated book %d", path, book.ID)
	default:
		// Already in the library (deduped); remember it so we skip it next tick.
		w.ok[path] = sig
		delete(w.bad, path)
	}
}
