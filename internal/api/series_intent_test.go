package api

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http/httptest"
	"testing"
)

// epubWithSeries builds a minimal EPUB whose OPF declares a calibre-style
// series, for exercising the embedded-metadata-wins rule (LYCM-82).
func epubWithSeries(t *testing.T, title, identifier, series, index string) []byte {
	t.Helper()
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

	write("mimetype", "application/epub+zip")
	write("META-INF/container.xml", `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`)
	write("OEBPS/content.opf", `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>`+title+`</dc:title>
    <dc:creator>Homer</dc:creator>
    <dc:language>en</dc:language>
    <meta name="calibre:series" content="`+series+`"/>
    <meta name="calibre:series_index" content="`+index+`"/>
    <dc:identifier id="bookid">`+identifier+`</dc:identifier>
  </metadata>
  <manifest><item id="c1" href="chapter1.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="c1"/></spine>
</package>`)
	write("OEBPS/chapter1.xhtml", `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><body><p>text</p></body></html>`)

	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// The core LYCM-82 regression: series assigned at confirm reaches the book when
// its grabbed EPUB later ingests without embedded series metadata.
func TestSeriesIntentAppliedOnIngest(t *testing.T) {
	s := testStore(t)
	a := New(s, "", WithResolver(testResolver()))
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	ctx := context.Background()

	b := decodeBatch(t, postJSON(t, srv.URL+"/ingest/batches", map[string]any{
		"scans": []map[string]any{{"isbn": isbnPiranesi}},
	}))
	resp := postJSON(t, srv.URL+"/ingest/candidates/"+itoa(b.Candidates[0].ID)+"/confirm",
		map[string]any{"series": "Piranesi Cycle", "series_index": 1})
	resp.Body.Close()

	// Intent landed on the inventory row.
	inv, err := s.GetInventoryByISBN(ctx, isbnPiranesi)
	if err != nil || inv.Series != "Piranesi Cycle" || inv.SeriesIndex != 1 {
		t.Fatalf("inventory intent = %q #%v err=%v, want Piranesi Cycle #1", inv.Series, inv.SeriesIndex, err)
	}

	// The grab lands: an EPUB with the same ISBN and no series metadata.
	book, result, err := a.ingestEPUB(ctx, epubWithIdentifier(t, "Piranesi", "urn:isbn:"+isbnPiranesi), "p.epub", "")
	if err != nil || result != ingestCreated {
		t.Fatalf("ingest: result=%v err=%v", result, err)
	}
	got, err := s.GetBook(ctx, book.ID)
	if err != nil {
		t.Fatalf("get book: %v", err)
	}
	if got.Series != "Piranesi Cycle" || got.SeriesIndex != 1 {
		t.Fatalf("book series = %q #%v, want intent applied (Piranesi Cycle #1)", got.Series, got.SeriesIndex)
	}
}

// Confirming after the EPUB already ingested applies the intent immediately —
// the other ordering of the same race.
func TestSeriesIntentAppliedToAlreadyIngestedBook(t *testing.T) {
	s := testStore(t)
	a := New(s, "", WithResolver(testResolver()))
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	ctx := context.Background()

	// Scan first (candidate ready, inventory not yet created)…
	b := decodeBatch(t, postJSON(t, srv.URL+"/ingest/batches", map[string]any{
		"scans": []map[string]any{{"isbn": isbnPiranesi}},
	}))

	// …then the EPUB arrives before the user confirms.
	book, result, err := a.ingestEPUB(ctx, epubWithIdentifier(t, "Piranesi", "urn:isbn:"+isbnPiranesi), "p.epub", "")
	if err != nil || result != ingestCreated {
		t.Fatalf("ingest: result=%v err=%v", result, err)
	}

	resp := postJSON(t, srv.URL+"/ingest/candidates/"+itoa(b.Candidates[0].ID)+"/confirm",
		map[string]any{"series": "Piranesi Cycle", "series_index": 1})
	resp.Body.Close()

	got, err := s.GetBook(ctx, book.ID)
	if err != nil {
		t.Fatalf("get book: %v", err)
	}
	if got.Series != "Piranesi Cycle" || got.SeriesIndex != 1 {
		t.Fatalf("book series = %q #%v, want Piranesi Cycle #1 applied at confirm", got.Series, got.SeriesIndex)
	}
}

// An EPUB that declares its own series keeps it — intent only fills gaps,
// matching the cover policy.
func TestSeriesIntentDoesNotOverrideEmbedded(t *testing.T) {
	s := testStore(t)
	a := New(s, "", WithResolver(testResolver()))
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	ctx := context.Background()

	b := decodeBatch(t, postJSON(t, srv.URL+"/ingest/batches", map[string]any{
		"scans": []map[string]any{{"isbn": isbnPiranesi}},
	}))
	resp := postJSON(t, srv.URL+"/ingest/candidates/"+itoa(b.Candidates[0].ID)+"/confirm",
		map[string]any{"series": "Wrong Series", "series_index": 9})
	resp.Body.Close()

	book, result, err := a.ingestEPUB(ctx,
		epubWithSeries(t, "Piranesi", "urn:isbn:"+isbnPiranesi, "Real Series", "1.0"),
		"p.epub", "")
	if err != nil || result != ingestCreated {
		t.Fatalf("ingest: result=%v err=%v", result, err)
	}
	got, err := s.GetBook(ctx, book.ID)
	if err != nil {
		t.Fatalf("get book: %v", err)
	}
	if got.Series != "Real Series" {
		t.Fatalf("book series = %q, want embedded metadata to win (Real Series)", got.Series)
	}
}
