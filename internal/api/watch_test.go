package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/magos/lyceum/internal/store"
)

// The watcher ingests EPUBs that appear in its tree (including nested
// per-author subfolders, as Bindery lays them out), deduplicates across ticks,
// and skips files that aren't valid EPUBs.
func TestWatcherScanOnce(t *testing.T) {
	s := testStore(t)
	a := New(s, "")
	dir := t.TempDir()
	w := NewWatcher(a, dir, 0)
	ctx := context.Background()

	// A book nested in a subfolder, as Bindery imports them.
	sub := filepath.Join(dir, "Homer")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(sub, "odyssey.epub"), epubWithIdentifier(t, "The Odyssey", "urn:isbn:9780140449334"))

	w.scanOnce(ctx)
	if n := countBooks(t, s); n != 1 {
		t.Fatalf("after first scan: %d books, want 1", n)
	}

	// Inventory was stamped from the ISBN.
	if inv, err := s.GetInventoryByISBN(ctx, "9780140449334"); err != nil {
		t.Fatalf("GetInventoryByISBN: %v", err)
	} else if inv.State != "ingested" || inv.BookID == nil {
		t.Fatalf("inventory not linked: %+v", inv)
	}

	// Re-scanning the unchanged tree ingests nothing new.
	w.scanOnce(ctx)
	if n := countBooks(t, s); n != 1 {
		t.Fatalf("after re-scan: %d books, want 1 (dedup)", n)
	}

	// A second, distinct book is picked up.
	writeFile(t, filepath.Join(dir, "iliad.epub"), epubWithIdentifier(t, "The Iliad", "urn:isbn:9780140449198"))
	w.scanOnce(ctx)
	if n := countBooks(t, s); n != 2 {
		t.Fatalf("after adding a book: %d books, want 2", n)
	}

	// A non-EPUB with a .epub extension is skipped, not ingested.
	writeFile(t, filepath.Join(dir, "broken.epub"), []byte("this is not a zip"))
	w.scanOnce(ctx)
	if n := countBooks(t, s); n != 2 {
		t.Fatalf("after a broken file: %d books, want 2", n)
	}
}

// A grab that lands as .mobi/.azw3 is not ingested (the reader is EPUB-only),
// but the watcher surfaces it — recorded once per file signature so the warning
// doesn't repeat every tick (LYCM-77).
func TestWatcherReportsNonEPUBLandings(t *testing.T) {
	s := testStore(t)
	a := New(s, "")
	dir := t.TempDir()
	w := NewWatcher(a, dir, 0)
	ctx := context.Background()

	sub := filepath.Join(dir, "Joe Abercrombie", "Before they are hanged (2007)")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(sub, "Before they are hanged - Joe Abercrombie.mobi")
	writeFile(t, path, []byte("BOOKMOBI fake payload"))

	w.scanOnce(ctx)
	if n := countBooks(t, s); n != 0 {
		t.Fatalf("mobi landed as %d books, want 0 (not ingestible)", n)
	}
	sig, reported := w.nonEPUB[path]
	if !reported || sig == "" {
		t.Fatalf("non-EPUB landing not recorded: nonEPUB=%v", w.nonEPUB)
	}

	// A re-scan of the unchanged file keeps the same signature (one report).
	w.scanOnce(ctx)
	if got := w.nonEPUB[path]; got != sig {
		t.Fatalf("signature churned across ticks: %q -> %q", sig, got)
	}

	// Unrelated file types (e.g. leftover .jpg sidecars) are ignored entirely.
	writeFile(t, filepath.Join(sub, "cover.jpg"), []byte("\xff\xd8\xff fake jpeg"))
	w.scanOnce(ctx)
	if len(w.nonEPUB) != 1 {
		t.Fatalf("nonEPUB tracks %d files, want 1 (jpg must not be reported)", len(w.nonEPUB))
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func countBooks(t *testing.T, s *store.Store) int {
	t.Helper()
	books, err := s.ListBooks(context.Background())
	if err != nil {
		t.Fatalf("ListBooks: %v", err)
	}
	return len(books)
}
