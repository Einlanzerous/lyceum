package store

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// NormalizeSourcePath canonicalizes a watched-tree path for storage and
// comparison by putting it in Unicode NFC.
//
// The acquisition pipeline writes the same book to paths that differ only in
// how their non-ASCII characters are encoded: "Victor Milán" arrived once with
// á as U+00E1 (NFC) and again as "a" + U+0301 (NFD). Those are distinct byte
// strings, so a re-import looked like a brand-new path and duplicated the book
// (LYCM-109) — exactly the failure LYCM-68 fixed for casing, along a second
// axis. Storing NFC means the column holds one spelling per path; sourceKey
// handles the casing axis on top of it.
//
// The stored path is an identity, not a handle: nothing opens a book by its
// source_path, so normalizing away from the literal on-disk bytes is safe.
func NormalizeSourcePath(p string) string {
	if p == "" {
		return ""
	}
	return norm.NFC.String(p)
}

// sourceKey is the identity a watched file is matched on: NFC-normalized and
// lowercased, so neither a re-cased folder (LYCM-68) nor a re-encoded accent
// (LYCM-109) reads as a different book.
func sourceKey(p string) string {
	return strings.ToLower(NormalizeSourcePath(p))
}

// TombstoneSource records that a book was deliberately deleted, so the folder
// watcher stops re-ingesting the file still sitting in the watched tree.
//
// Without this, delete only appears to work: the watcher skips already-seen
// files via an in-memory signature map, so a deleted book stays gone until the
// next restart and then silently returns (LYCM-109). A tombstone carries both
// identities the watcher could match on — the content hash and the source key —
// so neither re-encoding the path nor re-stamping the file resurrects it.
// sourcePath is "" for books that were uploaded rather than folder-ingested;
// those tombstone on hash alone.
func (s *Store) TombstoneSource(ctx context.Context, sourcePath, fileHash string) error {
	if fileHash == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO deleted_sources (source_key, file_hash) VALUES ($1, $2)
		 ON CONFLICT (file_hash) DO UPDATE
		    SET source_key = EXCLUDED.source_key, deleted_at = now()`,
		sourceKey(sourcePath), fileHash)
	if err != nil {
		return fmt.Errorf("store: tombstone source: %w", err)
	}
	return nil
}

// IsSourceTombstoned reports whether content arriving from the watched tree was
// previously deleted by hand, matching on either identity: the same bytes
// landing anywhere, or any bytes landing at the path whose book was deleted.
// The second case is what stops a re-stamped file (new hash, same path) from
// undoing the delete.
func (s *Store) IsSourceTombstoned(ctx context.Context, sourcePath, fileHash string) (bool, error) {
	key := sourceKey(sourcePath)
	var found bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM deleted_sources
		    WHERE file_hash = $1
		       OR ($2 <> '' AND source_key = $2))`, fileHash, key).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("store: check tombstone: %w", err)
	}
	return found, nil
}

// ClearTombstone forgets a deletion, so the content can be ingested again. An
// HTTP upload calls it: uploading a file is an explicit "I want this book",
// which outranks an earlier delete. Folder ingest deliberately does not — the
// watcher re-offers the same file every scan, and honouring that would make the
// tombstone useless.
func (s *Store) ClearTombstone(ctx context.Context, sourcePath, fileHash string) error {
	key := sourceKey(sourcePath)
	_, err := s.pool.Exec(ctx,
		`DELETE FROM deleted_sources WHERE file_hash = $1 OR ($2 <> '' AND source_key = $2)`,
		fileHash, key)
	if err != nil {
		return fmt.Errorf("store: clear tombstone: %w", err)
	}
	return nil
}
