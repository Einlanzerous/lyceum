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
