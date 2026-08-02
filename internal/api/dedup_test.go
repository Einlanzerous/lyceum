package api

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/magos/lyceum/internal/ingestqc"
	"github.com/magos/lyceum/internal/store"
)

// dedupEPUB builds an EPUB with a chosen title, author and optional series
// position. filler varies the bytes so two books of the same work hash
// differently — which is the whole point: the hash and source-path checks
// already catch identical bytes, and this is about the files they miss.
func dedupEPUB(t *testing.T, title, author, series string, index int, filler, identifier string) []byte {
	t.Helper()
	if identifier == "" {
		identifier = "urn:uuid:" + filler
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, contents string) {
		t.Helper()
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(contents)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}

	seriesMeta := ""
	if series != "" {
		seriesMeta = fmt.Sprintf(
			`<meta name="calibre:series" content="%s"/><meta name="calibre:series_index" content="%d"/>`,
			series, index)
	}

	write("mimetype", "application/epub+zip")
	write("META-INF/container.xml", `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`)
	write("OEBPS/content.opf", `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>`+title+`</dc:title>
    <dc:creator>`+author+`</dc:creator>
    <dc:language>en</dc:language>
    <dc:identifier id="bookid">`+identifier+`</dc:identifier>
    `+seriesMeta+`
  </metadata>
  <manifest><item id="c1" href="chapter1.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="c1"/></spine>
</package>`)
	write("OEBPS/chapter1.xhtml", `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><body><p>`+filler+`</p></body></html>`)

	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// dedupAPI builds an API whose ingests pass every other QC check, so that when a
// book lands in the review queue the duplicate match is the only thing that put
// it there. Without the cover fetcher every fixture trips no_cover and the
// distinction the tests are drawing disappears.
func dedupAPI(t *testing.T, s *store.Store) *API {
	t.Helper()
	return New(s, "", WithCoverFetcher(&fakeFetcher{data: solidPNG(t, 366, 600)}), WithIngestQC(true))
}

// realISBN is a valid ISBN-13, so ingest QC's no_isbn check passes. Two files of
// one book legitimately share one.
const realISBN = "urn:isbn:9780140447941"

// TestFolderIngestHoldsPossibleDuplicate is the core of LYCM-113: a second,
// byte-different EPUB of a book already on the shelf lands in the review queue
// rather than silently becoming a second shelf entry.
func TestFolderIngestHoldsPossibleDuplicate(t *testing.T) {
	s := testStore(t)
	a := dedupAPI(t, s)
	ctx := context.Background()

	first, result, err := a.ingestEPUB(ctx,
		dedupEPUB(t, "The Left Hand of Darkness", "Ursula K. Le Guin", "", 0, "aaaa", realISBN),
		"lhod.epub", "/media/lhod.epub")
	if err != nil || result != ingestCreated {
		t.Fatalf("first ingest: result=%v err=%v", result, err)
	}

	// A different file, a different path, and the inverted author spelling one
	// packager uses and another doesn't — the shape a re-download actually takes.
	second, result, err := a.ingestEPUB(ctx,
		dedupEPUB(t, "Left Hand of Darkness", "Le Guin, Ursula K.", "", 0, "bbbb", realISBN),
		"lhod-2.epub", "/media/retagged/lhod-2.epub")
	if err != nil || result != ingestCreated {
		t.Fatalf("second ingest: result=%v err=%v", result, err)
	}
	if second.ID == first.ID {
		t.Fatal("second file replaced the first; these are different files at different paths")
	}
	if second.ReviewState != store.ReviewPending {
		t.Errorf("second book review_state = %q, want pending", second.ReviewState)
	}
	if !slices.Contains(second.ReviewFlags, ingestqc.FlagPossibleDuplicate) {
		t.Errorf("second book flags = %v, want %s", second.ReviewFlags, ingestqc.FlagPossibleDuplicate)
	}
	// The first book passed QC cleanly, so the duplicate match is the only thing
	// holding the second — otherwise this test would pass on an unrelated flag.
	if first.ReviewState != store.ReviewPublished {
		t.Errorf("first book review_state = %q (flags %v), want published", first.ReviewState, first.ReviewFlags)
	}
	if second.DuplicateOf != first.ID {
		t.Errorf("second book duplicate_of = %d, want %d", second.DuplicateOf, first.ID)
	}

	// The shelf shows one book; the other is waiting on a decision.
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	var shelf, queue []bookJSON
	getJSON(t, srv.URL+"/library", &shelf)
	getJSON(t, srv.URL+"/ingest/review", &queue)
	if len(shelf) != 1 || shelf[0].ID != first.ID {
		t.Errorf("shelf = %+v, want only the first book", shelf)
	}
	if len(queue) != 1 || queue[0].ID != second.ID {
		t.Fatalf("review queue = %+v, want the suspected duplicate", queue)
	}
	if queue[0].DuplicateOf != first.ID {
		t.Errorf("queue entry duplicate_of = %d, want %d; the UI cannot show the pair without it",
			queue[0].DuplicateOf, first.ID)
	}
}

// TestSeriesVolumesArePublishedNormally guards the false positive that would
// hurt most. A series arrives in bulk and every volume shares an author, so
// flagging each one against the last would bury the queue.
func TestSeriesVolumesArePublishedNormally(t *testing.T) {
	s := testStore(t)
	a := New(s, "", WithIngestQC(true))
	ctx := context.Background()

	for i, title := range []string{"The Eye of the World", "The Great Hunt", "The Dragon Reborn"} {
		book, result, err := a.ingestEPUB(ctx,
			dedupEPUB(t, title, "Robert Jordan", "The Wheel of Time", i+1, fmt.Sprintf("wot%d", i), ""),
			fmt.Sprintf("wot%d.epub", i), fmt.Sprintf("/media/wot/%d.epub", i))
		if err != nil || result != ingestCreated {
			t.Fatalf("ingest %q: result=%v err=%v", title, result, err)
		}
		if slices.Contains(book.ReviewFlags, ingestqc.FlagPossibleDuplicate) {
			t.Errorf("%q flagged as a duplicate of book %d; series volumes are not copies",
				title, book.DuplicateOf)
		}
	}

	// A reissue of volume 2 under volume 1's title. Contrived as a title, but it
	// is the only shape that reaches the series guard through the whole stack:
	// the volumes above have distinct titles, so they never get that far, and
	// this is what proves calibre:series_index survives EPUB parsing into the
	// matcher rather than being dropped somewhere between.
	clash, result, err := a.ingestEPUB(ctx,
		dedupEPUB(t, "The Eye of the World", "Robert Jordan", "The Wheel of Time", 9, "wot-clash", ""),
		"wot-clash.epub", "/media/wot/clash.epub")
	if err != nil || result != ingestCreated {
		t.Fatalf("clashing-title ingest: result=%v err=%v", result, err)
	}
	if slices.Contains(clash.ReviewFlags, ingestqc.FlagPossibleDuplicate) {
		t.Errorf("volume 9 flagged as a copy of book %d; a different series index makes it a different volume",
			clash.DuplicateOf)
	}
}

// TestUploadRejectsPossibleDuplicate: the review queue only holds new folder
// ingests, so an upload gets the conflict directly instead of being filed
// somewhere the uploader would have to go and find.
func TestUploadRejectsPossibleDuplicate(t *testing.T) {
	s := testStore(t)
	a := dedupAPI(t, s)
	ctx := context.Background()

	first, _, err := a.ingestEPUB(ctx,
		dedupEPUB(t, "Piranesi", "Susanna Clarke", "", 0, "cccc", realISBN),
		"piranesi.epub", "/media/piranesi.epub")
	if err != nil {
		t.Fatalf("seed ingest: %v", err)
	}

	srv := httptest.NewServer(a.Handler())
	defer srv.Close()

	status, body := uploadResult(t, srv, "piranesi-again.epub",
		dedupEPUB(t, "Piranesi", "Susanna Clarke", "", 0, "dddd", realISBN))
	if status != http.StatusConflict {
		t.Fatalf("upload of a second copy = %d, want 409 (body %q)", status, body)
	}
	// The uploader has to be able to act on this, which means knowing what it
	// collided with.
	if !strings.Contains(body, "Piranesi") || !strings.Contains(body, fmt.Sprint(first.ID)) {
		t.Errorf("conflict body = %q, want it to name the existing book and its id", body)
	}

	// And nothing was written: a refused upload must not leave a row behind.
	var shelf []bookJSON
	getJSON(t, srv.URL+"/library", &shelf)
	if len(shelf) != 1 {
		t.Errorf("shelf has %d books after a refused upload, want 1", len(shelf))
	}
}

// TestUploadOfAGenuinelyNewBookStillWorks: the check must not turn every upload
// into a conflict.
func TestUploadOfAGenuinelyNewBookStillWorks(t *testing.T) {
	s := testStore(t)
	a := dedupAPI(t, s)
	ctx := context.Background()

	if _, _, err := a.ingestEPUB(ctx,
		dedupEPUB(t, "Piranesi", "Susanna Clarke", "", 0, "eeee", realISBN),
		"piranesi.epub", "/media/piranesi.epub"); err != nil {
		t.Fatalf("seed ingest: %v", err)
	}

	srv := httptest.NewServer(a.Handler())
	defer srv.Close()

	status, body := uploadResult(t, srv, "annihilation.epub",
		dedupEPUB(t, "Annihilation", "Jeff VanderMeer", "", 0, "ffff", ""))
	if status != http.StatusCreated {
		t.Fatalf("upload of an unrelated book = %d, want 201 (body %q)", status, body)
	}
}

// TestDuplicateOfClearsWhenTheMatchIsDeleted: resolving a duplicate by deleting
// the older book must not delete the newer one with it, and must leave no
// pointer at a row that is gone.
func TestDuplicateOfClearsWhenTheMatchIsDeleted(t *testing.T) {
	s := testStore(t)
	a := dedupAPI(t, s)
	ctx := context.Background()

	first, _, err := a.ingestEPUB(ctx,
		dedupEPUB(t, "Dune", "Frank Herbert", "", 0, "gggg", ""),
		"dune.epub", "/media/dune.epub")
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	second, _, err := a.ingestEPUB(ctx,
		dedupEPUB(t, "Dune", "Frank Herbert", "", 0, "hhhh", ""),
		"dune-2.epub", "/media/dune-2.epub")
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if second.DuplicateOf != first.ID {
		t.Fatalf("duplicate_of = %d, want %d", second.DuplicateOf, first.ID)
	}

	if _, err := s.DeleteBook(ctx, first.ID); err != nil {
		t.Fatalf("DeleteBook: %v", err)
	}

	got, err := s.GetBook(ctx, second.ID)
	if err != nil {
		t.Fatalf("the suspected duplicate was deleted along with its match: %v", err)
	}
	if got.DuplicateOf != 0 {
		t.Errorf("duplicate_of = %d after the target was deleted, want 0", got.DuplicateOf)
	}
}

// uploadResult uploads data and returns the status and trimmed body. The
// postUpload helper in upload_test.go hands back the raw response, and
// uploadEPUB in deliveries_test.go asserts 201 outright — neither reports the
// conflict message these cases are about.
func uploadResult(t *testing.T, srv *httptest.Server, filename string, data []byte) (int, string) {
	t.Helper()
	resp := postUpload(t, srv, filename, data)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read upload response: %v", err)
	}
	return resp.StatusCode, strings.TrimSpace(string(raw))
}
