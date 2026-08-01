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
// axis. Storing NFC means the column holds one spelling per path; SourceKey
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

// SourceKey is the identity a watched file is matched on: NFC-normalized and
// lowercased, so neither a re-cased folder (LYCM-68) nor a re-encoded accent
// (LYCM-109) reads as a different book. It is the one predicate for "these two
// paths name the same file" on the Go side — callers should not hand-roll
// EqualFold comparisons, which cover only the casing axis.
//
// SQL that matches paths deliberately does not use this: it folds both sides in
// Postgres instead, so matching does not depend on Go and the database's lower()
// agreeing. See GetBookBySourcePath. Within one table the keys are written and
// read by this function alone, so they are self-consistent.
func SourceKey(p string) string {
	return strings.ToLower(NormalizeSourcePath(p))
}

// TombstoneSource records that a folder-ingested book was deliberately deleted,
// so the watcher stops re-ingesting the file still sitting in the watched tree.
//
// Without this, delete only appears to work: the watcher skips already-seen
// files via an in-memory signature map, so a deleted book stays gone until the
// next restart and then silently returns (LYCM-109). A tombstone carries both
// identities the watcher could match on — the content hash and the source key —
// so neither re-encoding the path nor re-stamping the file resurrects it.
//
// It is deliberately scoped to books that have a watched file. An uploaded book
// has none, so there is nothing to keep re-offering it and a tombstone would
// only sit there refusing some future legitimate acquisition of the same bytes,
// invisibly and permanently. Callers pass "" for uploads and this is a no-op.
func (s *Store) TombstoneSource(ctx context.Context, sourcePath, fileHash string) error {
	if sourcePath == "" || fileHash == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO deleted_sources (source_key, file_hash) VALUES ($1, $2)
		 ON CONFLICT (file_hash) DO UPDATE
		    SET source_key = EXCLUDED.source_key, deleted_at = now()`,
		SourceKey(sourcePath), fileHash)
	if err != nil {
		return fmt.Errorf("store: tombstone source: %w", err)
	}
	return nil
}

// IsSourceTombstoned reports whether content arriving from the watched tree was
// previously deleted by hand, matching on either identity: the same bytes
// landing anywhere, or any bytes landing at the path whose book was deleted.
// The second case is what stops a re-stamped file (new hash, same path) from
// undoing the delete. Only the folder-ingest path calls it, so sourcePath is
// always a real path.
func (s *Store) IsSourceTombstoned(ctx context.Context, sourcePath, fileHash string) (bool, error) {
	var found bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM deleted_sources WHERE file_hash = $1 OR source_key = $2)`,
		fileHash, SourceKey(sourcePath)).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("store: check tombstone: %w", err)
	}
	return found, nil
}

// ClearTombstone forgets the deletion of some content, so it can be ingested
// again. An HTTP upload calls it: uploading a file is an explicit "I want this
// book", which outranks an earlier delete, and is how a deleted book gets added
// back. Folder ingest deliberately does not — the watcher re-offers the same
// file every scan, and honouring that would make the tombstone useless.
//
// Clearing by content hash also drops that tombstone's source key, so the
// watched file the upload duplicates is welcome again too.
func (s *Store) ClearTombstone(ctx context.Context, fileHash string) error {
	if fileHash == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM deleted_sources WHERE file_hash = $1`, fileHash)
	if err != nil {
		return fmt.Errorf("store: clear tombstone: %w", err)
	}
	return nil
}
