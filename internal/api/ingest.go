package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/magos/lyceum/internal/epub"
	"github.com/magos/lyceum/internal/isbn"
	"github.com/magos/lyceum/internal/store"
)

// errNotEPUB marks input that is empty or does not parse as a valid EPUB.
// Callers (HTTP upload) map it to a 400; the folder watcher logs and retries.
var errNotEPUB = errors.New("not a valid epub")

// ingestEPUB is the single ingest path shared by HTTP upload and folder ingest:
// it validates the bytes are a real EPUB, content-addresses them by SHA-256,
// deduplicates on the hash, persists the blob + cover + row, links the book to
// its ISBN-keyed inventory entry (best effort), and fires any auto-delivery.
//
// created reports whether a new book row was written; created=false with a nil
// error means the content was already present (a duplicate), and the existing
// row is returned. source is a human label (filename or path) used for the
// title fallback and log lines.
func (a *API) ingestEPUB(ctx context.Context, data []byte, source string) (book store.Book, created bool, err error) {
	if len(data) == 0 {
		return store.Book{}, false, fmt.Errorf("%w: empty input", errNotEPUB)
	}

	md, err := epub.ParseBytes(data)
	if err != nil {
		return store.Book{}, false, fmt.Errorf("%w: %v", errNotEPUB, err)
	}

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	// Dedup on content hash before writing any blobs.
	switch existing, err := a.store.GetBookByHash(ctx, hash); {
	case err == nil:
		return existing, false, nil
	case errors.Is(err, store.ErrNotFound):
		// not a duplicate; continue
	default:
		return store.Book{}, false, fmt.Errorf("lookup by hash: %w", err)
	}

	filePath, coverPath, err := a.store.SaveBlobs(hash, data, md.CoverData)
	if err != nil {
		return store.Book{}, false, fmt.Errorf("save blobs: %w", err)
	}

	saved, err := a.store.InsertBook(ctx, store.Book{
		Title:     ingestTitle(md, source),
		Author:    strings.TrimSpace(md.Author),
		CoverPath: coverPath,
		FilePath:  filePath,
		FileHash:  hash,
		SizeBytes: int64(len(data)),
	})
	if err != nil {
		return store.Book{}, false, fmt.Errorf("insert book: %w", err)
	}

	// Best effort: stamp the ISBN/ingested state onto inventory. Never fails the
	// ingest — a book without a usable ISBN identifier simply has no inventory
	// link, and a transient DB hiccup here is logged, not propagated.
	a.linkInventory(ctx, saved, md)

	// Fire-and-forget "Send to Kindle" when auto-send is configured. Done after
	// the row is persisted and never blocks or fails ingest.
	a.maybeAutoDeliver(ctx, saved)

	return saved, true, nil
}

// linkInventory links a freshly-ingested book to its inventory entry by the
// ISBN carried in the EPUB's dc:identifier, if any. EPUBs frequently identify
// themselves by UUID rather than ISBN, so a missing ISBN is the normal case and
// not an error.
func (a *API) linkInventory(ctx context.Context, book store.Book, md *epub.Metadata) {
	code, ok := isbn.FromIdentifier(md.Identifier)
	if !ok {
		return
	}
	if _, err := a.store.LinkBookToInventory(ctx, code, book.ID, book.Title, book.Author); err != nil {
		log.Printf("api: link inventory isbn=%s book=%d: %v", code, book.ID, err)
	}
}

// ingestTitle prefers the EPUB's declared title, falling back to the source
// filename (minus extension) so a book never lands with an empty NOT NULL title
// column.
func ingestTitle(md *epub.Metadata, source string) string {
	if t := strings.TrimSpace(md.Title); t != "" {
		return t
	}
	base := filepath.Base(source)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if base = strings.TrimSpace(base); base != "" && base != "." && base != "/" {
		return base
	}
	return "Untitled"
}
