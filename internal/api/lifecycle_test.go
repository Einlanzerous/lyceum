package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/magos/lyceum/internal/store"
)

// blobDirExists reports whether the hash-named blob directory backing filePath
// still exists on disk.
func blobDirExists(t *testing.T, filePath string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Dir(filePath))
	return err == nil
}

// TestReplaceOnRestamp verifies the LYCM-66 stable-identity path: a re-stamped
// watched file (same source path, new content) updates its book in place rather
// than creating a duplicate — keeping the id and reading positions, swapping the
// blobs.
func TestReplaceOnRestamp(t *testing.T) {
	s := testStore(t)
	a := New(s, "")
	ctx := context.Background()
	const path = "/data/media/books/knights.epub"

	v1 := epubWithIdentifier(t, "Knights V1", "urn:isbn:9780765332677")
	b1, result, err := a.ingestEPUB(ctx, v1, path, path)
	if err != nil || result != ingestCreated {
		t.Fatalf("first ingest: result=%v err=%v", result, err)
	}

	// A reading position must survive the in-place replace.
	if _, err := s.UpsertPositionLWW(ctx, store.ReadingPosition{
		BookID: b1.ID, DeviceID: "dev-1", CFI: "/6/4!/2", Progress: 0.42,
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed position: %v", err)
	}

	// Re-stamp: same path, different bytes (new title) -> new content hash.
	v2 := epubWithIdentifier(t, "Knights V2", "urn:isbn:9780765332677")
	b2, result, err := a.ingestEPUB(ctx, v2, path, path)
	if err != nil {
		t.Fatalf("re-stamp ingest err: %v", err)
	}
	if result != ingestReplaced {
		t.Fatalf("re-stamp result=%v, want ingestReplaced", result)
	}
	if b2.ID != b1.ID {
		t.Fatalf("replace created a new id %d, want %d (no duplicate)", b2.ID, b1.ID)
	}
	if b2.Title != "Knights V2" {
		t.Fatalf("replace kept stale title %q, want %q", b2.Title, "Knights V2")
	}

	// Exactly one book on the shelf — the whole point.
	books, err := s.ListBooks(ctx)
	if err != nil {
		t.Fatalf("list books: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("library has %d books after re-stamp, want 1", len(books))
	}

	// Reading position preserved (same book id).
	pos, err := s.GetFurthestPosition(ctx, b1.ID)
	if err != nil {
		t.Fatalf("position lost after replace: %v", err)
	}
	if pos.Progress != 0.42 {
		t.Fatalf("position progress=%v after replace, want 0.42", pos.Progress)
	}

	// Blobs swapped: old dir gone, new dir present.
	if b1.FilePath != b2.FilePath && blobDirExists(t, b1.FilePath) {
		t.Errorf("old blob dir %q still present after replace", filepath.Dir(b1.FilePath))
	}
	if !blobDirExists(t, b2.FilePath) {
		t.Errorf("new blob dir %q missing after replace", filepath.Dir(b2.FilePath))
	}
}

// TestAdoptSourcePathOnDuplicate verifies that when identical content is later
// seen via the watcher, a book ingested without a source path (e.g. an upload,
// or a pre-migration row) adopts the watched path — so a subsequent re-stamp
// can update it in place instead of duplicating.
func TestAdoptSourcePathOnDuplicate(t *testing.T) {
	s := testStore(t)
	a := New(s, "")
	ctx := context.Background()
	const path = "/data/media/books/adopted.epub"

	data := epubWithIdentifier(t, "Adopt Me", "urn:isbn:9780618260300")

	// Ingested as an upload: no source path.
	b, result, err := a.ingestEPUB(ctx, data, "adopted.epub", "")
	if err != nil || result != ingestCreated {
		t.Fatalf("upload ingest: result=%v err=%v", result, err)
	}
	if b.SourcePath != "" {
		t.Fatalf("upload book has source_path %q, want empty", b.SourcePath)
	}

	// The watcher then sees the same bytes at a real path -> dedup + adopt.
	adopted, result, err := a.ingestEPUB(ctx, data, path, path)
	if err != nil {
		t.Fatalf("watcher ingest err: %v", err)
	}
	if result != ingestDuplicate {
		t.Fatalf("watcher ingest result=%v, want ingestDuplicate", result)
	}
	if adopted.ID != b.ID {
		t.Fatalf("adopt created a new id %d, want %d", adopted.ID, b.ID)
	}
	reloaded, err := s.GetBook(ctx, b.ID)
	if err != nil {
		t.Fatalf("get book: %v", err)
	}
	if reloaded.SourcePath != path {
		t.Fatalf("book did not adopt source_path: got %q, want %q", reloaded.SourcePath, path)
	}
}

// TestDeleteBookEndpoint verifies DELETE /books/{id} removes the row and its
// blobs (204), and 404s an unknown id.
func TestDeleteBookEndpoint(t *testing.T) {
	s := testStore(t)
	a := New(s, "")
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	ctx := context.Background()

	data := epubWithIdentifier(t, "Delete Me", "urn:isbn:9780553573404")
	b, result, err := a.ingestEPUB(ctx, data, "del.epub", "")
	if err != nil || result != ingestCreated {
		t.Fatalf("ingest: result=%v err=%v", result, err)
	}
	if !blobDirExists(t, b.FilePath) {
		t.Fatalf("blob dir missing before delete")
	}

	// Unknown id -> 404.
	if code := deleteBook(t, srv.URL, b.ID+999); code != http.StatusNotFound {
		t.Fatalf("delete unknown id: got %d, want 404", code)
	}

	// Real delete -> 204, gone from the store, blobs removed.
	if code := deleteBook(t, srv.URL, b.ID); code != http.StatusNoContent {
		t.Fatalf("delete: got %d, want 204", code)
	}
	if _, err := s.GetBook(ctx, b.ID); err == nil {
		t.Fatal("book still present after delete")
	}
	if blobDirExists(t, b.FilePath) {
		t.Errorf("blob dir %q still present after delete", filepath.Dir(b.FilePath))
	}

	books, err := s.ListBooks(ctx)
	if err != nil {
		t.Fatalf("list books: %v", err)
	}
	if len(books) != 0 {
		t.Fatalf("library has %d books after delete, want 0", len(books))
	}
}

// deleteBook issues DELETE /books/{id} and returns the status code.
func deleteBook(t *testing.T, baseURL string, id int64) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, baseURL+"/books/"+itoa(id), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /books/%d: %v", id, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
