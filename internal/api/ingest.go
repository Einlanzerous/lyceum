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

	"github.com/magos/lyceum/internal/coverart"
	"github.com/magos/lyceum/internal/coverimg"
	"github.com/magos/lyceum/internal/epub"
	"github.com/magos/lyceum/internal/ingestqc"
	"github.com/magos/lyceum/internal/isbn"
	"github.com/magos/lyceum/internal/store"
)

// errNotEPUB marks input that is empty or does not parse as a valid EPUB.
// Callers (HTTP upload) map it to a 400; the folder watcher logs and retries.
var errNotEPUB = errors.New("not a valid epub")

// ingestResult reports what ingestEPUB did with the input.
type ingestResult int

const (
	// ingestDuplicate: the content was already present; the existing row is
	// returned unchanged. Also the zero value, so it is what error returns carry.
	ingestDuplicate ingestResult = iota
	// ingestCreated: a new book row was written.
	ingestCreated
	// ingestReplaced: an existing book at the same source path was updated in
	// place (a re-stamped watched file), keeping its id and reading positions.
	ingestReplaced
)

// ingestEPUB is the single ingest path shared by HTTP upload and folder ingest:
// it validates the bytes are a real EPUB, content-addresses them by SHA-256,
// deduplicates on the hash, persists the blob + cover + row, links the book to
// its ISBN-keyed inventory entry (best effort), and fires any auto-delivery.
//
// source is a human label (filename or path) used for the title fallback and
// log lines. sourcePath is the stable filesystem identity of a folder-ingested
// file (empty for HTTP uploads): when a re-stamped watched file arrives with new
// content but a path already mapped to a book, the book is updated in place
// rather than duplicated (LYCM-66). The returned ingestResult reports which
// happened.
func (a *API) ingestEPUB(ctx context.Context, data []byte, source, sourcePath string) (book store.Book, result ingestResult, err error) {
	if len(data) == 0 {
		return store.Book{}, ingestDuplicate, fmt.Errorf("%w: empty input", errNotEPUB)
	}

	md, err := epub.ParseBytes(data)
	if err != nil {
		return store.Book{}, ingestDuplicate, fmt.Errorf("%w: %v", errNotEPUB, err)
	}

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	// Dedup on content hash before writing any blobs. If this exact content
	// arrived via the watcher and the existing row has no source path yet, adopt
	// this path so a later re-stamp updates it in place instead of duplicating.
	switch existing, err := a.store.GetBookByHash(ctx, hash); {
	case err == nil:
		if sourcePath != "" && existing.SourcePath == "" {
			if e := a.store.SetBookSourcePath(ctx, existing.ID, sourcePath); e != nil {
				log.Printf("api: adopt source path for book %d: %v", existing.ID, e)
			} else {
				existing.SourcePath = sourcePath
			}
		}
		return existing, ingestDuplicate, nil
	case errors.Is(err, store.ErrNotFound):
		// not a content duplicate; continue
	default:
		return store.Book{}, ingestDuplicate, fmt.Errorf("lookup by hash: %w", err)
	}

	// New content from a watched path that already maps to a book means the file
	// was re-stamped (metadata edit / re-encode): replace that book's content in
	// place, keeping its id and reading positions.
	if sourcePath != "" {
		switch existing, err := a.store.GetBookBySourcePath(ctx, sourcePath); {
		case err == nil:
			return a.replaceBook(ctx, existing, md, data, hash)
		case errors.Is(err, store.ErrNotFound):
			// no book at this path yet; fall through to a fresh insert
		default:
			return store.Book{}, ingestDuplicate, fmt.Errorf("lookup by source path: %w", err)
		}
	}

	// Choose the source cover once (chooseCover may hit the network), run ingest
	// QC on it, then normalize the same bytes for storage.
	chosen := a.chooseCover(ctx, md)
	reviewState, reviewFlags := a.reviewOutcome(md, chosen)

	filePath, coverPath, err := a.store.SaveBlobs(hash, data, a.normalizeCover(md.Title, chosen))
	if err != nil {
		return store.Book{}, ingestDuplicate, fmt.Errorf("save blobs: %w", err)
	}

	saved, err := a.store.InsertBook(ctx, store.Book{
		Title:       ingestTitle(md, source),
		Author:      strings.TrimSpace(md.Author),
		CoverPath:   coverPath,
		FilePath:    filePath,
		FileHash:    hash,
		SizeBytes:   int64(len(data)),
		SourcePath:  sourcePath,
		Series:      strings.TrimSpace(md.Series),
		SeriesIndex: md.SeriesIndex,
		ReviewState: reviewState,
		ReviewFlags: reviewFlags,
	})
	if err != nil {
		return store.Book{}, ingestDuplicate, fmt.Errorf("insert book: %w", err)
	}
	if reviewState == store.ReviewPending {
		log.Printf("ingest: %q held for review (flags=%v)", saved.Title, reviewFlags)
	}

	// Best effort: stamp the ISBN/ingested state onto inventory. Never fails the
	// ingest — a book without a usable ISBN identifier simply has no inventory
	// link, and a transient DB hiccup here is logged, not propagated.
	a.linkInventory(ctx, saved, md)

	// Fire-and-forget "Send to Kindle" when auto-send is configured. Done after
	// the row is persisted and never blocks or fails ingest.
	a.maybeAutoDeliver(ctx, saved)

	return saved, ingestCreated, nil
}

// replaceBook updates an existing book's content in place for a re-stamped
// watched file: it writes the new blobs, repoints the row at them, refreshes the
// title/author from the new metadata, removes the now-orphaned old blobs, and
// re-links inventory. The book keeps its id, so reading positions survive. It
// deliberately does not re-fire auto-delivery — a re-stamp is an update, not a
// new acquisition.
func (a *API) replaceBook(ctx context.Context, existing store.Book, md *epub.Metadata, data []byte, hash string) (store.Book, ingestResult, error) {
	oldFilePath := existing.FilePath

	filePath, coverPath, err := a.store.SaveBlobs(hash, data, a.coverForIngest(ctx, md))
	if err != nil {
		return store.Book{}, ingestDuplicate, fmt.Errorf("save blobs: %w", err)
	}

	updated, err := a.store.UpdateBookContent(ctx, existing.ID, store.Book{
		Title:       ingestTitle(md, existing.SourcePath),
		Author:      strings.TrimSpace(md.Author),
		CoverPath:   coverPath,
		FilePath:    filePath,
		FileHash:    hash,
		SizeBytes:   int64(len(data)),
		Series:      strings.TrimSpace(md.Series),
		SeriesIndex: md.SeriesIndex,
	})
	if err != nil {
		return store.Book{}, ingestDuplicate, fmt.Errorf("update book content: %w", err)
	}

	// New content hashes to a new blob dir; drop the old one. Best effort — a
	// leftover dir is harmless, and the row already points at the new blobs.
	if oldFilePath != "" && oldFilePath != filePath {
		if e := a.store.RemoveBlobs(oldFilePath); e != nil {
			log.Printf("api: replace book %d: remove old blobs: %v", updated.ID, e)
		}
	}

	a.linkInventory(ctx, updated, md)
	return updated, ingestReplaced, nil
}

// coverForIngest returns the cover bytes to store for a freshly-parsed EPUB: it
// chooses the source cover (chooseCover) and normalizes it for the shelf
// (LYCM-65). It is used by the re-stamp path (replaceBook); the fresh-insert
// path calls chooseCover + normalizeCover separately so it can also QC the
// pre-normalization bytes without fetching the cover twice.
func (a *API) coverForIngest(ctx context.Context, md *epub.Metadata) []byte {
	return a.normalizeCover(md.Title, a.chooseCover(ctx, md))
}

// normalizeCover cleans cover bytes for the shelf (LYCM-65) unless normalization
// is disabled. title is only used for log context. Best effort: on any failure
// Normalize returns the original bytes, so ingest never breaks on a bad cover.
func (a *API) normalizeCover(title string, cover []byte) []byte {
	if !a.normalizeCovers || len(cover) == 0 {
		return cover
	}
	norm, err := coverimg.Normalize(cover, coverimg.DefaultOptions())
	if err != nil {
		log.Printf("api: normalize cover for %q: %v (storing original)", title, err)
		return cover
	}
	return norm
}

// reviewOutcome decides a freshly-ingested book's review state (LYCM-58). When
// ingest QC is enabled and the source trips a detector (no ISBN, poor cover,
// mangled title), the book is held ReviewPending with the detected flags;
// otherwise it publishes straight to the shelf. cover is the pre-normalization
// source cover. QC disabled → always published, no flags.
func (a *API) reviewOutcome(md *epub.Metadata, cover []byte) (string, []string) {
	if !a.ingestQC {
		return store.ReviewPublished, nil
	}
	flags := ingestqc.Detect(md, cover)
	if len(flags) == 0 {
		return store.ReviewPublished, nil
	}
	return store.ReviewPending, flags
}

// chooseCover selects the source cover bytes for a freshly-parsed EPUB.
// It only reaches for external art when the EPUB has NO embedded cover: fetched
// art fills the gap so the book gets a real cover instead of the generated
// fallback tile. A book that ships its own cover keeps it — external sources
// (e.g. Open Library) frequently only have a lower-resolution or wrong-edition
// image, so replacing a present cover tends to make the shelf worse, not better.
// Deliberate replacement of a poor embedded cover is the job of the
// `backfill-covers` tool, not the ingest path. Best effort: a fetch error is
// logged and never fails the ingest.
func (a *API) chooseCover(ctx context.Context, md *epub.Metadata) []byte {
	if a.covers == nil || len(md.CoverData) > 0 {
		return md.CoverData
	}
	q := coverart.Query{
		Title:    md.Title,
		Author:   md.Author,
		Language: md.Language,
		ISBNs:    isbn.AllFrom(md.Identifiers),
	}
	// Nothing to key on (no ISBN and no title): no cover.
	if len(q.ISBNs) == 0 && strings.TrimSpace(q.Title) == "" {
		return md.CoverData
	}
	switch art, _, err := a.covers.Fetch(ctx, q); {
	case err == nil && len(art) > 0:
		return art
	case err != nil && !errors.Is(err, coverart.ErrNotFound):
		log.Printf("api: fetch cover for %q: %v (no embedded cover to fall back to)", md.Title, err)
	}
	return md.CoverData
}

// linkInventory links a freshly-ingested book to its inventory entry by the
// ISBN carried in the EPUB's dc:identifiers, if any. EPUBs frequently identify
// themselves by UUID (sometimes ahead of the ISBN), so a missing ISBN is the
// normal case and not an error.
func (a *API) linkInventory(ctx context.Context, book store.Book, md *epub.Metadata) {
	code, ok := isbn.FirstFrom(md.Identifiers)
	if !ok {
		return
	}
	// Resolve the work so an ingested ebook joins the row a print scan created,
	// even though their ISBNs differ (LYCM-35). Best effort: no resolver / no
	// match falls back to exact-ISBN linking.
	workID := a.resolveWorkID(ctx, code)
	if _, err := a.store.LinkBookToInventory(ctx, code, workID, book.ID, book.Title, book.Author); err != nil {
		log.Printf("api: link inventory isbn=%s book=%d: %v", code, book.ID, err)
	}
}

// resolveWorkID best-effort resolves an ISBN to its resolver work key, so print
// and ebook editions of one work group onto a single inventory entry. It returns
// "" when the resolver is the no-op default, finds nothing, or errors — grouping
// then falls back to exact-ISBN matching.
func (a *API) resolveWorkID(ctx context.Context, code string) string {
	eds, err := a.resolver.ResolveISBN(ctx, code)
	if err != nil {
		log.Printf("api: resolve work id for isbn=%s: %v", code, err)
		return ""
	}
	for _, e := range eds {
		if e.WorkID != "" {
			return e.WorkID
		}
	}
	return ""
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
