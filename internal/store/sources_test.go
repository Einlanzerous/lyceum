package store

import (
	"context"
	"testing"
)

// Two spellings of the same path: á composed (U+00E1) and decomposed
// (a + U+0301). Escaped rather than written literally so the difference is
// visible in review and survives any tool that normalizes source files.
const (
	srcNFC = "/data/media/books/Victor Mil\u00e1n/knights.epub"
	srcNFD = "/data/media/books/Victor Mila\u0301n/knights.epub"
)

// TestSourcePathStoredNormalized: whichever spelling ingest hands the store,
// the column holds one canonical form (LYCM-109).
func TestSourcePathStoredNormalized(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	b := sampleBook("hash-nfd")
	b.SourcePath = srcNFD
	saved, err := s.InsertBook(ctx, b)
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	if saved.SourcePath != srcNFC {
		t.Fatalf("stored source_path=%q, want NFC %q", saved.SourcePath, srcNFC)
	}

	// SetBookSourcePath normalizes on the same terms.
	other, err := s.InsertBook(ctx, sampleBook("hash-other"))
	if err != nil {
		t.Fatalf("InsertBook other: %v", err)
	}
	const otherNFD = "/data/media/books/Björk Güðmundsdóttir/album.epub"
	if err := s.SetBookSourcePath(ctx, other.ID, otherNFD); err != nil {
		t.Fatalf("SetBookSourcePath: %v", err)
	}
	reloaded, err := s.GetBook(ctx, other.ID)
	if err != nil {
		t.Fatalf("GetBook: %v", err)
	}
	if reloaded.SourcePath != NormalizeSourcePath(otherNFD) {
		t.Fatalf("stored source_path=%q, want normalized", reloaded.SourcePath)
	}
}

// TestGetBookBySourcePathIgnoresSpelling: a lookup resolves regardless of how
// the caller spells the path — different case (LYCM-68), different
// normalization form (LYCM-109), or both.
func TestGetBookBySourcePathIgnoresSpelling(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	b := sampleBook("hash-lookup")
	b.SourcePath = srcNFC
	saved, err := s.InsertBook(ctx, b)
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}

	for _, q := range []string{
		srcNFC,
		srcNFD,
		"/data/media/books/VICTOR MILÁN/KNIGHTS.EPUB",
	} {
		got, err := s.GetBookBySourcePath(ctx, q)
		if err != nil {
			t.Fatalf("GetBookBySourcePath(%q): %v", q, err)
		}
		if got.ID != saved.ID {
			t.Fatalf("GetBookBySourcePath(%q) = book %d, want %d", q, got.ID, saved.ID)
		}
	}

	// A genuinely different path still misses.
	if _, err := s.GetBookBySourcePath(ctx, "/data/media/books/elsewhere.epub"); err != ErrNotFound {
		t.Fatalf("unrelated path: got %v, want ErrNotFound", err)
	}
}

// TestGetBookBySourcePathFindsLegacyRow covers the rows migration 0013 leaves
// un-normalized: when a duplicate already claims the canonical spelling, the
// back-fill skips the row rather than tripping books_source_path_key, so the
// query itself has to normalize to keep resolving it. The path is written
// straight to the column to bypass the store's own normalization.
func TestGetBookBySourcePathFindsLegacyRow(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	saved, err := s.InsertBook(ctx, sampleBook("hash-legacy"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE books SET source_path = $2 WHERE id = $1`, saved.ID, srcNFD); err != nil {
		t.Fatalf("seed un-normalized row: %v", err)
	}

	got, err := s.GetBookBySourcePath(ctx, srcNFC)
	if err != nil {
		t.Fatalf("GetBookBySourcePath: %v", err)
	}
	if got.ID != saved.ID {
		t.Fatalf("got book %d, want %d", got.ID, saved.ID)
	}
}

// TestTombstoneLifecycle: a tombstone blocks the deleted content and anything
// arriving at the deleted path, ignores spelling, and is lifted by a clear.
func TestTombstoneLifecycle(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	if err := s.TombstoneSource(ctx, srcNFC, "hash-gone"); err != nil {
		t.Fatalf("TombstoneSource: %v", err)
	}

	cases := []struct {
		name string
		path string
		hash string
		want bool
	}{
		{"same content, same path", srcNFC, "hash-gone", true},
		{"same content, elsewhere", "/data/media/books/moved.epub", "hash-gone", true},
		{"re-stamped at the deleted path", srcNFC, "hash-new", true},
		{"re-stamped, path respelled", srcNFD, "hash-new", true},
		{"unrelated book", "/data/media/books/other.epub", "hash-other", false},
	}
	for _, tc := range cases {
		got, err := s.IsSourceTombstoned(ctx, tc.path, tc.hash)
		if err != nil {
			t.Fatalf("%s: IsSourceTombstoned: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: tombstoned=%v, want %v", tc.name, got, tc.want)
		}
	}

	// Re-tombstoning the same content updates the row instead of erroring on the
	// unique hash: a book can be deleted, re-added, and deleted again.
	if err := s.TombstoneSource(ctx, "/data/media/books/moved.epub", "hash-gone"); err != nil {
		t.Fatalf("re-TombstoneSource: %v", err)
	}

	if err := s.ClearTombstone(ctx, "hash-gone"); err != nil {
		t.Fatalf("ClearTombstone: %v", err)
	}
	got, err := s.IsSourceTombstoned(ctx, srcNFC, "hash-gone")
	if err != nil {
		t.Fatalf("IsSourceTombstoned after clear: %v", err)
	}
	if got {
		t.Error("content still tombstoned after clear")
	}
}

// TestUploadedBookIsNotTombstoned: a tombstone exists to stop the watcher
// re-offering a file. An uploaded book has no watched file, so deleting one must
// not leave a hash tombstone behind to refuse a later acquisition of the same
// bytes — invisibly, and with no way to lift it but re-uploading them.
func TestUploadedBookIsNotTombstoned(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	if err := s.TombstoneSource(ctx, "", "hash-upload"); err != nil {
		t.Fatalf("TombstoneSource: %v", err)
	}
	got, err := s.IsSourceTombstoned(ctx, "/data/media/books/acquired-later.epub", "hash-upload")
	if err != nil {
		t.Fatalf("IsSourceTombstoned: %v", err)
	}
	if got {
		t.Error("deleting an uploaded book blocked a later folder ingest of the same bytes")
	}
}
