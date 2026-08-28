package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/magos/lyceum/internal/store"
)

// epubWithIdentifier builds a minimal valid EPUB whose dc:identifier is the
// given value, so tests can exercise ISBN extraction (urn:isbn:...) vs. the
// common non-ISBN case (urn:uuid:...).
func epubWithIdentifier(t *testing.T, title, identifier string) []byte {
	t.Helper()
	return epubWithIdentifierAndAuthor(t, title, "Homer", identifier)
}

// epubWithIdentifierAndAuthor is epubWithIdentifier with the dc:creator chosen
// too, for tests that match on author (LYCM-128).
func epubWithIdentifierAndAuthor(t *testing.T, title, author, identifier string) []byte {
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
    <dc:creator>`+author+`</dc:creator>
    <dc:language>en</dc:language>
    <dc:identifier id="bookid">`+identifier+`</dc:identifier>
  </metadata>
  <manifest><item id="c1" href="chapter1.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="c1"/></spine>
</package>`)
	write("OEBPS/chapter1.xhtml", `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><body><p>Sing, goddess.</p></body></html>`)

	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func getInventory(t *testing.T, baseURL string) []inventoryJSON {
	t.Helper()
	resp, err := http.Get(baseURL + "/inventory")
	if err != nil {
		t.Fatalf("GET /inventory: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /inventory status = %d, want 200", resp.StatusCode)
	}
	var out []inventoryJSON
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode inventory: %v", err)
	}
	return out
}

// Ingesting an EPUB that identifies itself by ISBN links it to an inventory
// entry, marked ingested, with the ISBN normalized to its 13-digit form.
func TestIngestLinksInventoryByISBN(t *testing.T) {
	s := testStore(t)
	srv := newServer(t, s)

	data := epubWithIdentifier(t, "The Odyssey", "urn:isbn:978-0-14-044933-4")
	resp := postUpload(t, srv, "odyssey.epub", data)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload status = %d, want 201; body=%s", resp.StatusCode, body)
	}
	var created bookJSON
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	inv := getInventory(t, srv.URL)
	if len(inv) != 1 {
		t.Fatalf("inventory has %d entries, want 1: %+v", len(inv), inv)
	}
	e := inv[0]
	if e.ISBN != "9780140449334" {
		t.Fatalf("isbn = %q, want normalized 9780140449334", e.ISBN)
	}
	if e.State != "ingested" {
		t.Fatalf("state = %q, want ingested", e.State)
	}
	if e.BookID == nil || *e.BookID != created.ID {
		t.Fatalf("book_id = %v, want %d", e.BookID, created.ID)
	}
}

// An EPUB identified by UUID (the common case) leaves inventory untouched.
func TestIngestNoISBNNoInventory(t *testing.T) {
	s := testStore(t)
	srv := newServer(t, s)

	data := epubWithIdentifier(t, "Anonymous", "urn:uuid:0a1b2c3d-4e5f-6789-abcd-ef0123456789")
	resp := postUpload(t, srv, "anon.epub", data)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201", resp.StatusCode)
	}

	if inv := getInventory(t, srv.URL); len(inv) != 0 {
		t.Fatalf("inventory has %d entries, want 0: %+v", len(inv), inv)
	}
}

// Re-ingesting identical content is a no-op duplicate, not an error, when it
// comes through the shared core (the watcher relies on this).
func TestIngestDuplicateIsNoOp(t *testing.T) {
	s := testStore(t)
	a := New(s, "")
	ctx := context.Background()

	data := epubWithIdentifier(t, "Dup", "urn:isbn:9780140449334")
	first, result, err := a.ingestEPUB(ctx, data, "dup.epub", "")
	if err != nil || result != ingestCreated {
		t.Fatalf("first ingest: result=%v err=%v", result, err)
	}
	second, result, err := a.ingestEPUB(ctx, data, "dup.epub", "")
	if err != nil {
		t.Fatalf("second ingest err: %v", err)
	}
	if result != ingestDuplicate {
		t.Fatalf("second ingest result=%v, want ingestDuplicate", result)
	}
	if second.ID != first.ID {
		t.Fatalf("dedup returned id %d, want %d", second.ID, first.ID)
	}
}

// An ingested EPUB whose ISBN the resolver cannot place (no work key) joins the
// wanted entry whose title and author match rather than opening a second row
// (LYCM-128: HP #1's Pottermore ISBN is unknown to Open Library).
func TestIngestJoinsWantedEntryByTitleAuthor(t *testing.T) {
	s := testStore(t)
	srv := newServer(t, s)
	ctx := context.Background()
	if _, err := s.UpsertInventory(ctx, store.Inventory{ISBN: "9780590353427", Title: "Harry Potter and the Philosopher's Stone", Author: "J. K. Rowling"}); err != nil {
		t.Fatalf("UpsertInventory: %v", err)
	}
	if _, err := s.SetInventoryState(ctx, "9780590353427", store.StateWanted); err != nil {
		t.Fatalf("SetInventoryState: %v", err)
	}

	data := epubWithIdentifierAndAuthor(t, "Harry Potter and the Philosopher's Stone", "Rowling, J. K.", "urn:isbn:9781781100073")
	resp := postUpload(t, srv, "hp1.epub", data)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload status = %d, want 201; body=%s", resp.StatusCode, body)
	}
	var created bookJSON
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	inv := getInventory(t, srv.URL)
	if len(inv) != 1 {
		t.Fatalf("inventory has %d entries, want 1 (the wanted row fulfilled): %+v", len(inv), inv)
	}
	if inv[0].ISBN != "9780590353427" || inv[0].State != "ingested" || inv[0].BookID == nil || *inv[0].BookID != created.ID {
		t.Fatalf("entry = %+v, want the print entry ingested with book %d", inv[0], created.ID)
	}
}
