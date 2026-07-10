package api

import (
	"bytes"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/magos/lyceum/internal/epub"
	"github.com/magos/lyceum/internal/store"
)

// locationJSON is the wire shape of GET /eidolon/books/{id}/location. ChapterHref
// is the spine document the current CFI resolves to, omitted when it can't be
// resolved (the position still round-trips for TTS via the CFI + progress).
type locationJSON struct {
	CFI         string    `json:"cfi"`
	Progress    float64   `json:"progress"`
	ChapterHref string    `json:"chapter_href,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// handleEidolonLocation returns the active reading location for a book: the
// latest synced CFI/progress plus the resolved chapter href, for Project
// Eidolon's local TTS (LYCM-403).
func (a *API) handleEidolonLocation(w http.ResponseWriter, r *http.Request) {
	b, ok := a.lookupBook(w, r)
	if !ok {
		return
	}

	pos, err := a.store.GetFurthestPosition(r.Context(), b.ID)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "no reading position", http.StatusNotFound)
		return
	}
	if err != nil {
		serverError(w, "get latest position", err)
		return
	}

	out := locationJSON{
		CFI:       pos.CFI,
		Progress:  pos.Progress,
		UpdatedAt: pos.UpdatedAt,
	}
	if href, ok := a.resolveChapterHref(b.FilePath, pos.CFI); ok {
		out.ChapterHref = href
	}
	writeJSON(w, http.StatusOK, out)
}

// resolveChapterHref opens the book's EPUB and maps the CFI to its spine
// document href. It is best-effort: any failure (unreadable EPUB, unparseable
// or out-of-range CFI) yields ok=false rather than an error, so the location
// endpoint still serves the raw position.
func (a *API) resolveChapterHref(filePath, cfi string) (string, bool) {
	rd, err := epub.OpenFile(filePath)
	if err != nil {
		return "", false
	}
	defer func() { _ = rd.Close() }()

	parsed, err := epub.ParseCFI(cfi)
	if err != nil {
		return "", false
	}
	idx, ok := parsed.SpineIndex()
	if !ok {
		return "", false
	}
	item, ok := rd.SpineItemAt(idx)
	if !ok {
		return "", false
	}
	return item.Href, true
}

// handleEidolonChapter streams a chapter's plain-text content for TTS (LYCM-404).
// The chapter is selected by ?index= (0-based spine index), ?href= (spine
// document href), or ?from_cfi= (resolve the chapter from a reading CFI), in
// that precedence. Output is text/plain with HTML stripped and paragraphs
// preserved.
//
// from_cfi currently resumes at chapter granularity — it selects the chapter
// the CFI lives in and streams it from the start. Sub-chapter (paragraph/offset)
// resume is a documented future refinement.
func (a *API) handleEidolonChapter(w http.ResponseWriter, r *http.Request) {
	b, ok := a.lookupBook(w, r)
	if !ok {
		return
	}

	rd, err := epub.OpenFile(b.FilePath)
	if errors.Is(err, fs.ErrNotExist) {
		// Row exists but its blob is gone (storage/DB drift): treat like a
		// missing resource, consistent with the cover/file blob handlers.
		http.Error(w, "book file missing", http.StatusNotFound)
		return
	}
	if err != nil {
		serverError(w, "open epub", err)
		return
	}
	defer func() { _ = rd.Close() }()

	item, ok := a.selectChapter(w, r, rd)
	if !ok {
		return
	}

	content, err := rd.ReadContent(item.Href)
	if err != nil {
		http.Error(w, "chapter content not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Chapter-Href", item.Href)
	// Track whether any byte has reached the client. ExtractText streams, so once
	// it has written, the 200 + headers are committed and we can't switch to a
	// 5xx — we can only log. If it fails before writing anything (e.g. malformed
	// content), nothing is committed yet and a real 500 is still possible.
	tw := &trackingWriter{w: w}
	if err := epub.ExtractText(tw, bytes.NewReader(content)); err != nil {
		if tw.written {
			log.Printf("api: extract chapter text (partial response already sent): %v", err)
			return
		}
		serverError(w, "extract chapter text", err)
		return
	}
}

// trackingWriter records whether any byte has been written, so a streaming
// handler can tell if the response status is already committed.
type trackingWriter struct {
	w       http.ResponseWriter
	written bool
}

func (t *trackingWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		t.written = true
	}
	return t.w.Write(p)
}

// selectChapter resolves the requested spine item from the query parameters,
// writing the appropriate 4xx response and returning ok=false on failure.
func (a *API) selectChapter(w http.ResponseWriter, r *http.Request, rd *epub.Reader) (epub.SpineItem, bool) {
	q := r.URL.Query()

	if idxStr := q.Get("index"); idxStr != "" {
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			http.Error(w, "invalid index", http.StatusBadRequest)
			return epub.SpineItem{}, false
		}
		item, ok := rd.SpineItemAt(idx)
		if !ok {
			http.Error(w, "spine index out of range", http.StatusNotFound)
			return epub.SpineItem{}, false
		}
		return item, true
	}

	if href := q.Get("href"); href != "" {
		item, _, ok := rd.FindSpineItem(href)
		if !ok {
			http.Error(w, "chapter not found in spine", http.StatusNotFound)
			return epub.SpineItem{}, false
		}
		return item, true
	}

	if cfi := q.Get("from_cfi"); cfi != "" {
		parsed, err := epub.ParseCFI(cfi)
		if err != nil {
			http.Error(w, "invalid from_cfi: "+err.Error(), http.StatusBadRequest)
			return epub.SpineItem{}, false
		}
		idx, ok := parsed.SpineIndex()
		if !ok {
			http.Error(w, "from_cfi does not address a spine item", http.StatusBadRequest)
			return epub.SpineItem{}, false
		}
		item, ok := rd.SpineItemAt(idx)
		if !ok {
			http.Error(w, "from_cfi spine index out of range", http.StatusNotFound)
			return epub.SpineItem{}, false
		}
		return item, true
	}

	http.Error(w, "specify one of index, href, or from_cfi", http.StatusBadRequest)
	return epub.SpineItem{}, false
}
