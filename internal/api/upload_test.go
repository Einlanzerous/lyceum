package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// sampleEPUB returns the bytes of a real sample EPUB shipped with the epub
// package's testdata.
func sampleEPUB(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join("..", "epub", "testdata", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read sample %s: %v", name, err)
	}
	return b
}

// multipartUpload builds a multipart/form-data body with the given file bytes
// under field "file", returning the body and its Content-Type header.
func multipartUpload(t *testing.T, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

func postUpload(t *testing.T, srv *httptest.Server, filename string, data []byte) *http.Response {
	t.Helper()
	body, ct := multipartUpload(t, filename, data)
	resp, err := http.Post(srv.URL+"/upload", ct, body)
	if err != nil {
		t.Fatalf("POST /upload: %v", err)
	}
	return resp
}

func TestUploadEPUB(t *testing.T) {
	s := testStore(t)
	srv := newServer(t, s)
	ctx := context.Background()

	data := sampleEPUB(t, "meditations.epub")
	resp := postUpload(t, srv, "meditations.epub", data)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201; body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var created bookJSON
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("created book has zero id: %+v", created)
	}
	if created.Title == "" {
		t.Fatalf("created book has empty title: %+v", created)
	}
	if created.CoverURL != coverURL(created.ID) {
		t.Fatalf("cover URL = %q, want %q", created.CoverURL, coverURL(created.ID))
	}

	// Queryable via the store.
	got, err := s.GetBook(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetBook: %v", err)
	}
	if got.Title != created.Title || got.Author != created.Author {
		t.Fatalf("store row mismatch: %+v vs %+v", got, created)
	}
	if got.FileHash == "" || got.FilePath == "" {
		t.Fatalf("store row missing blob fields: %+v", got)
	}
	if got.SizeBytes != int64(len(data)) {
		t.Fatalf("size = %d, want %d", got.SizeBytes, len(data))
	}
	// Blobs were actually written.
	if _, err := os.Stat(got.FilePath); err != nil {
		t.Fatalf("epub blob missing: %v", err)
	}
	if got.CoverPath != "" {
		if _, err := os.Stat(got.CoverPath); err != nil {
			t.Fatalf("cover blob missing: %v", err)
		}
	}

	// Queryable via GET /library.
	libResp, err := http.Get(srv.URL + "/library")
	if err != nil {
		t.Fatalf("GET /library: %v", err)
	}
	defer libResp.Body.Close()
	var books []bookJSON
	if err := json.NewDecoder(libResp.Body).Decode(&books); err != nil {
		t.Fatalf("decode library: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("library has %d books, want 1", len(books))
	}
	if books[0].ID != created.ID || books[0].Title != created.Title {
		t.Fatalf("library entry mismatch: %+v vs %+v", books[0], created)
	}
}

func TestUploadDuplicateRejected(t *testing.T) {
	s := testStore(t)
	srv := newServer(t, s)

	data := sampleEPUB(t, "iliad.epub")

	first := postUpload(t, srv, "iliad.epub", data)
	first.Body.Close()
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first upload status = %d, want 201", first.StatusCode)
	}

	second := postUpload(t, srv, "iliad.epub", data)
	second.Body.Close()
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate upload status = %d, want 409", second.StatusCode)
	}

	// Only one row persisted.
	books, err := s.ListBooks(context.Background())
	if err != nil {
		t.Fatalf("ListBooks: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("got %d books after duplicate upload, want 1", len(books))
	}
}

func TestUploadNotAnEPUB(t *testing.T) {
	s := testStore(t)
	srv := newServer(t, s)

	resp := postUpload(t, srv, "notes.txt", []byte("this is plainly not a zip/epub"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	books, err := s.ListBooks(context.Background())
	if err != nil {
		t.Fatalf("ListBooks: %v", err)
	}
	if len(books) != 0 {
		t.Fatalf("got %d books after invalid upload, want 0", len(books))
	}
}

func TestUploadMissingFileField(t *testing.T) {
	s := testStore(t)
	srv := newServer(t, s)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("notfile", "x"); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	_ = mw.Close()

	resp, err := http.Post(srv.URL+"/upload", mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
