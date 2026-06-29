package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/magos/lyceum/internal/store"
)

// seedRealBook persists data as a book's EPUB blob (so epub.OpenFile works) and
// returns the inserted row.
func seedRealBook(t *testing.T, s *store.Store, hash string, data []byte) store.Book {
	t.Helper()
	filePath, _, err := s.SaveBlobs(hash, data, nil)
	if err != nil {
		t.Fatalf("SaveBlobs: %v", err)
	}
	b, err := s.InsertBook(context.Background(), store.Book{
		Title:     "The Test Book",
		Author:    "A. Tester",
		FilePath:  filePath,
		FileHash:  hash,
		SizeBytes: int64(len(data)),
	})
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	return b
}

func newEidolonServer(t *testing.T, s *store.Store) *httptest.Server {
	t.Helper()
	auth, err := ParseTokens(tokenRead + "=eidolon:read," + tokenSend + "=delivery:send")
	if err != nil {
		t.Fatalf("ParseTokens: %v", err)
	}
	srv := httptest.NewServer(New(s, "", WithAuth(auth)).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func getAuth(t *testing.T, url, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func TestEidolonLocation(t *testing.T) {
	s := testStore(t)
	b := seedRealBook(t, s, "hash-loc", syntheticEPUB(t))

	when := time.Date(2026, 6, 26, 19, 0, 0, 0, time.UTC)
	if _, err := s.UpsertPositionLWW(context.Background(), store.ReadingPosition{
		BookID: b.ID, DeviceID: "kobo-1",
		CFI:       "epubcfi(/6/2[c1]!/4/2/1:0)", // spine index 0 -> chapter1
		Progress:  0.42,
		UpdatedAt: when,
	}); err != nil {
		t.Fatalf("UpsertPositionLWW: %v", err)
	}

	srv := newEidolonServer(t, s)
	resp := getAuth(t, srv.URL+"/eidolon/books/"+strconv.FormatInt(b.ID, 10)+"/location", tokenRead)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var loc locationJSON
	if err := json.NewDecoder(resp.Body).Decode(&loc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if loc.CFI != "epubcfi(/6/2[c1]!/4/2/1:0)" {
		t.Errorf("cfi = %q", loc.CFI)
	}
	if loc.Progress != 0.42 {
		t.Errorf("progress = %v, want 0.42", loc.Progress)
	}
	if loc.ChapterHref != "OEBPS/chapter1.xhtml" {
		t.Errorf("chapter_href = %q, want OEBPS/chapter1.xhtml", loc.ChapterHref)
	}
	if !loc.UpdatedAt.Equal(when) {
		t.Errorf("updated_at = %v, want %v", loc.UpdatedAt, when)
	}
}

func TestEidolonLocationNoPosition(t *testing.T) {
	s := testStore(t)
	b := seedRealBook(t, s, "hash-nopos", syntheticEPUB(t))
	srv := newEidolonServer(t, s)

	resp := getAuth(t, srv.URL+"/eidolon/books/"+strconv.FormatInt(b.ID, 10)+"/location", tokenRead)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestEidolonAuth(t *testing.T) {
	s := testStore(t)
	b := seedRealBook(t, s, "hash-eauth", syntheticEPUB(t))
	srv := newEidolonServer(t, s)
	url := srv.URL + "/eidolon/books/" + strconv.FormatInt(b.ID, 10) + "/location"

	// No token → 401.
	resp := getAuth(t, url, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token = %d, want 401", resp.StatusCode)
	}
	// Wrong scope (delivery:send) → 403.
	resp = getAuth(t, url, tokenSend)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong-scope = %d, want 403", resp.StatusCode)
	}
}

func TestEidolonChapterByIndex(t *testing.T) {
	s := testStore(t)
	b := seedRealBook(t, s, "hash-chap", syntheticEPUB(t))
	srv := newEidolonServer(t, s)

	resp := getAuth(t, srv.URL+"/eidolon/books/"+strconv.FormatInt(b.ID, 10)+"/chapter?index=0", tokenRead)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.Contains(text, "Chapter One") || !strings.Contains(text, "Call me Ishmael") {
		t.Errorf("chapter 1 text wrong: %q", text)
	}
	if strings.Contains(text, "carpet-bag") {
		t.Errorf("leaked chapter 2 text: %q", text)
	}
}

func TestEidolonChapterByHref(t *testing.T) {
	s := testStore(t)
	b := seedRealBook(t, s, "hash-href", syntheticEPUB(t))
	srv := newEidolonServer(t, s)

	resp := getAuth(t, srv.URL+"/eidolon/books/"+strconv.FormatInt(b.ID, 10)+"/chapter?href=chapter2.xhtml", tokenRead)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "carpet-bag") {
		t.Errorf("chapter 2 text wrong: %q", body)
	}
}

func TestEidolonChapterByFromCFI(t *testing.T) {
	s := testStore(t)
	b := seedRealBook(t, s, "hash-fromcfi", syntheticEPUB(t))
	srv := newEidolonServer(t, s)

	// /6/4 -> spine index 1 -> chapter2.
	resp := getAuth(t, srv.URL+"/eidolon/books/"+strconv.FormatInt(b.ID, 10)+"/chapter?from_cfi=epubcfi(/6/4[c2]!/4/2/1:0)", tokenRead)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "carpet-bag") {
		t.Errorf("from_cfi chapter wrong: %q", body)
	}
}

// TestEidolonChapterMissingBlob covers DB/disk drift: a book row whose EPUB
// blob no longer exists should 404, not 500.
func TestEidolonChapterMissingBlob(t *testing.T) {
	s := testStore(t)
	b := seedRealBook(t, s, "hash-gone", syntheticEPUB(t))
	// Remove the blob out from under the row.
	if err := os.Remove(b.FilePath); err != nil {
		t.Fatalf("remove blob: %v", err)
	}
	srv := newEidolonServer(t, s)

	resp := getAuth(t, srv.URL+"/eidolon/books/"+strconv.FormatInt(b.ID, 10)+"/chapter?index=0", tokenRead)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestEidolonChapterBadParams(t *testing.T) {
	s := testStore(t)
	b := seedRealBook(t, s, "hash-bad", syntheticEPUB(t))
	srv := newEidolonServer(t, s)
	base := srv.URL + "/eidolon/books/" + strconv.FormatInt(b.ID, 10) + "/chapter"

	// No selector → 400.
	resp := getAuth(t, base, tokenRead)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("no-param status = %d, want 400", resp.StatusCode)
	}
	// Out-of-range index → 404.
	resp = getAuth(t, base+"?index=99", tokenRead)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("oob index status = %d, want 404", resp.StatusCode)
	}
}
