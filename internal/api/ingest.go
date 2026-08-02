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
	"time"

	"github.com/magos/lyceum/internal/coverart"
	"github.com/magos/lyceum/internal/coverimg"
	"github.com/magos/lyceum/internal/dedup"
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
	// ingestTombstoned: the content (or the path it arrived at) belongs to a book
	// somebody deleted, and the file is still sitting in the watched tree. Nothing
	// was written; the delete stands.
	ingestTombstoned
	// ingestPossibleDuplicate: the content is a different file that looks like
	// another copy of a book already present (LYCM-113), and the caller is an
	// upload rather than the watcher. Nothing was written and the returned book
	// is the existing one it matched, so the caller can name it in a conflict.
	// Folder ingest never gets this: it holds the newcomer for review instead.
	ingestPossibleDuplicate
)

// workIDResolveTimeout bounds the work-key lookup. It sits ahead of the insert
// (duplicate detection needs the key), so it has to be short enough that a slow
// resolver cannot hold an ingest open.
const workIDResolveTimeout = 10 * time.Second

// ingestConfig carries the per-call overrides an ingest can take. It exists as
// options rather than parameters because only one caller ever sets one.
type ingestConfig struct {
	// allowDuplicate skips duplicate detection entirely, for an uploader who has
	// looked at the conflict and wants both copies anyway (LYCM-113).
	allowDuplicate bool
}

// ingestOption overrides one part of an ingest.
type ingestOption func(*ingestConfig)

// allowDuplicate keeps a second copy of a book already present, instead of
// refusing the upload. The alternative on offer — delete the existing book —
// destroys its reading positions and read marks, which is a steep price for
// wanting a better scan or another translation.
func allowDuplicate() ingestOption {
	return func(c *ingestConfig) { c.allowDuplicate = true }
}

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
func (a *API) ingestEPUB(ctx context.Context, data []byte, source, sourcePath string, opts ...ingestOption) (book store.Book, result ingestResult, err error) {
	var cfg ingestConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	if len(data) == 0 {
		return store.Book{}, ingestDuplicate, fmt.Errorf("%w: empty input", errNotEPUB)
	}

	md, err := epub.ParseBytes(data)
	if err != nil {
		return store.Book{}, ingestDuplicate, fmt.Errorf("%w: %v", errNotEPUB, err)
	}

	// Canonicalize the path's Unicode spelling up front so every identity check
	// below — and the value that lands in the column — uses one form (LYCM-109).
	sourcePath = store.NormalizeSourcePath(sourcePath)

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	// Dedup on content hash before writing any blobs. If this exact content
	// arrived via the watcher and the existing row has no source path yet — or
	// has the same path respelled (the acquisition pipeline re-cases folder names,
	// LYCM-68, and re-encodes accents, LYCM-109) — adopt this path so a later
	// re-stamp updates it in place instead of duplicating.
	switch existing, err := a.store.GetBookByHash(ctx, hash); {
	case err == nil:
		if sourcePath != "" && existing.SourcePath != sourcePath &&
			(existing.SourcePath == "" ||
				store.SourceKey(existing.SourcePath) == store.SourceKey(sourcePath)) {
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

	if sourcePath != "" {
		// The watcher re-offers every file in the tree on every scan, so content
		// whose book was deleted has to be refused explicitly or the delete
		// silently undoes itself on the next restart (LYCM-109).
		switch tombstoned, err := a.store.IsSourceTombstoned(ctx, sourcePath, hash); {
		case err != nil:
			return store.Book{}, ingestDuplicate, fmt.Errorf("check tombstone: %w", err)
		case tombstoned:
			return store.Book{}, ingestTombstoned, nil
		}

		// New content from a watched path that already maps to a book means the
		// file was re-stamped (metadata edit / re-encode): replace that book's
		// content in place, keeping its id and reading positions.
		switch existing, err := a.store.GetBookBySourcePath(ctx, sourcePath); {
		case err == nil:
			return a.replaceBook(ctx, existing, sourcePath, md, data, hash)
		case errors.Is(err, store.ErrNotFound):
			// no book at this path yet; fall through to a fresh insert
		default:
			return store.Book{}, ingestDuplicate, fmt.Errorf("lookup by source path: %w", err)
		}
	}

	// Resolve the work key once: duplicate detection and inventory linking both
	// want it, and resolving is a network call.
	//
	// Detached from the request context and separately bounded. Hoisting a
	// network call ahead of the insert would otherwise widen the window in which
	// a client disconnect cancels ctx and loses an upload that was already
	// complete — before LYCM-113 this call happened after the row was persisted,
	// where a cancellation cost only the inventory link.
	workID := ""
	if code, ok := isbn.FirstFrom(md.Identifiers); ok {
		resolveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workIDResolveTimeout)
		workID = a.resolveWorkID(resolveCtx, code)
		cancel()
	}

	// Everything above only catches the same *bytes* arriving again — under a
	// new path, a respelled one (LYCM-66/68/109), or after a delete. Two
	// genuinely different files of one book still pass all of it, and the shelf
	// shows the book twice. Look for one before writing any blobs (LYCM-113).
	//
	// Skipped for a folder ingest when QC is off: there would be no queue to hold
	// the result in, so scanning the whole library would buy a log line.
	dup, isDup := dedup.Match{}, false
	if !cfg.allowDuplicate && (sourcePath == "" || a.ingestQC) {
		dup, isDup = a.findDuplicate(ctx, md, source, workID)
	}
	if isDup {
		// Re-read the match. ListDedupCandidates is a snapshot, and the book it
		// named can be gone by now — resolving a duplicate by deleting the older
		// copy is exactly what the review queue is for.
		switch existing, err := a.store.GetBook(ctx, dup.BookID); {
		case err == nil:
			if sourcePath == "" {
				// An upload is a person handing over one specific file, so the
				// answer is an immediate conflict naming what it collides with —
				// not a queue entry they would have to go and find. Nothing is
				// written; ?force=true overrides.
				return existing, ingestPossibleDuplicate, nil
			}
		case errors.Is(err, store.ErrNotFound):
			isDup = false
		default:
			return store.Book{}, ingestDuplicate, fmt.Errorf("load duplicate %d: %w", dup.BookID, err)
		}
	}

	if sourcePath == "" {
		// An upload is an explicit request for this book, so it lifts an earlier
		// delete rather than being refused by it.
		//
		// Deferred until after every refusal above: those write nothing, and
		// clearing the tombstone for an upload that is then refused would let the
		// watcher re-ingest a deliberately deleted book on its next tick — the
		// LYCM-109 regression, reintroduced through a door LYCM-113 opened.
		//
		// Best effort from here: a stale tombstone only affects a later folder
		// ingest, and this upload is about to succeed.
		if err := a.store.ClearTombstone(ctx, hash); err != nil {
			log.Printf("api: clear tombstone for upload %q: %v", source, err)
		}
	}

	// Choose the source cover once (chooseCover may hit the network), run ingest
	// QC on it, then normalize the same bytes for storage.
	chosen := a.chooseCover(ctx, md)
	reviewState, reviewFlags := a.reviewOutcome(md, chosen)

	// A suspected duplicate is held for a person to decide: two files of one work
	// are often deliberate — a better scan, a different translation — so the
	// queue offers the choice instead of collapsing them.
	if isDup {
		reviewState = store.ReviewPending
		reviewFlags = append(reviewFlags, ingestqc.FlagPossibleDuplicate)
	}

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

	// Point the held book at what it matched, after the insert rather than as
	// part of it. duplicate_of is a self-FK, so carrying it into InsertBook makes
	// a deleted match fail the whole ingest — and for a folder ingest that parks
	// the path in the watcher's bad-signature set, where it re-errors until the
	// file changes. The pointer is advisory: losing it costs the review UI its
	// side-by-side comparison, which it already handles.
	if isDup {
		if err := a.store.SetDuplicateOf(ctx, saved.ID, dup.BookID); err != nil {
			log.Printf("api: record duplicate %d -> %d: %v", saved.ID, dup.BookID, err)
		} else {
			saved.DuplicateOf = dup.BookID
		}
	}

	// Best effort: stamp the ISBN/ingested state onto inventory. Never fails the
	// ingest — a book without a usable ISBN identifier simply has no inventory
	// link, and a transient DB hiccup here is logged, not propagated.
	a.linkInventory(ctx, saved, md, workID)

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
// new acquisition. sourcePath is the path the new content arrived at; when it
// differs from the stored one (a re-cased folder, LYCM-68) the row adopts it so
// source_path keeps tracking the on-disk truth.
func (a *API) replaceBook(ctx context.Context, existing store.Book, sourcePath string, md *epub.Metadata, data []byte, hash string) (store.Book, ingestResult, error) {
	oldFilePath := existing.FilePath

	filePath, coverPath, err := a.store.SaveBlobs(hash, data, a.coverForIngest(ctx, md))
	if err != nil {
		return store.Book{}, ingestDuplicate, fmt.Errorf("save blobs: %w", err)
	}

	updated, err := a.store.UpdateBookContent(ctx, existing.ID, store.Book{
		Title:       ingestTitle(md, sourcePath),
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

	// A re-cased path (matched case-insensitively) replaces the stored one, so
	// the row keeps naming the file as it exists on disk. Best effort — the
	// case-insensitive lookup finds the book either way.
	if existing.SourcePath != sourcePath {
		if e := a.store.SetBookSourcePath(ctx, updated.ID, sourcePath); e != nil {
			log.Printf("api: replace book %d: update source path: %v", updated.ID, e)
		} else {
			updated.SourcePath = sourcePath
		}
	}

	// A re-stamp keeps the same book, so there is no duplicate question here —
	// only the work key inventory linking wants, resolved for this call alone.
	workID := ""
	if code, ok := isbn.FirstFrom(md.Identifiers); ok {
		workID = a.resolveWorkID(ctx, code)
	}
	a.linkInventory(ctx, updated, md, workID)
	return updated, ingestReplaced, nil
}

// findDuplicate reports whether the incoming metadata looks like another copy of
// a book already in the library (LYCM-113).
//
// Best effort, like the rest of the non-essential ingest work: a lookup failure
// logs and reports no match, because refusing to ingest a book over a hiccup in
// a suggestion is worse than letting a duplicate through. The next scan asks
// again.
func (a *API) findDuplicate(ctx context.Context, md *epub.Metadata, source, workID string) (dedup.Match, bool) {
	candidates, err := a.store.ListDedupCandidates(ctx)
	if err != nil {
		log.Printf("api: list dedup candidates: %v", err)
		return dedup.Match{}, false
	}
	shelf := make([]dedup.Candidate, len(candidates))
	for i, c := range candidates {
		shelf[i] = dedup.Candidate(c)
	}
	return dedup.Find(dedup.Candidate{
		Title:       ingestTitle(md, source),
		Author:      strings.TrimSpace(md.Author),
		Series:      strings.TrimSpace(md.Series),
		SeriesIndex: md.SeriesIndex,
		WorkID:      workID,
	}, shelf)
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
// normal case and not an error. A series assigned at ingest confirm rides on
// the inventory row (LYCM-82); it is applied here when the EPUB itself declares
// no series — an embedded series wins, matching the fill-gap cover policy.
// workID is the resolver work key the caller already resolved for this book, so
// an ingested ebook joins the row a print scan created even though their ISBNs
// differ (LYCM-35). It is passed in rather than resolved here because duplicate
// detection needs the same key first (LYCM-113) and resolving hits the network;
// "" falls back to exact-ISBN linking, as it did when no resolver was set.
func (a *API) linkInventory(ctx context.Context, book store.Book, md *epub.Metadata, workID string) {
	code, ok := isbn.FirstFrom(md.Identifiers)
	if !ok {
		return
	}
	inv, err := a.store.LinkBookToInventory(ctx, code, workID, book.ID, book.Title, book.Author)
	if err != nil {
		log.Printf("api: link inventory isbn=%s book=%d: %v", code, book.ID, err)
		return
	}
	if book.Series == "" && inv.Series != "" {
		a.applySeriesIntent(ctx, book.ID, inv.Series, inv.SeriesIndex)
	}
}

// applySeriesIntent writes a confirm-time series assignment onto a book, unless
// the book already carries one from its own metadata (fill-gap, LYCM-82). Best
// effort: a failure logs and never disturbs the ingest/confirm that called it.
func (a *API) applySeriesIntent(ctx context.Context, bookID int64, series string, index float64) {
	book, err := a.store.GetBook(ctx, bookID)
	if err != nil {
		log.Printf("api: apply series intent book=%d: %v", bookID, err)
		return
	}
	if book.Series != "" {
		return // the EPUB's own metadata wins
	}
	if _, err := a.store.UpdateBookSeries(ctx, bookID, series, index); err != nil {
		log.Printf("api: apply series intent book=%d: %v", bookID, err)
		return
	}
	log.Printf("api: applied series intent %q #%v to book %d", series, index, bookID)
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
