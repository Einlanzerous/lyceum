package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/magos/lyceum/internal/epub"
	"github.com/magos/lyceum/internal/store"
)

// maxUploadBytes caps an uploaded EPUB. It is generous (200 MiB) but bounds
// memory: the whole file is read into RAM to hash and parse it.
const maxUploadBytes = 200 << 20

// handleUpload ingests an EPUB from a multipart/form-data request (field
// "file"): it validates the bytes are a real EPUB, content-addresses them by
// SHA-256, rejects duplicates, persists the blob + cover + row, and responds
// 201 with the created book JSON.
func (a *API) handleUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing multipart file field \"file\"", http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		// MaxBytesReader surfaces oversize bodies here.
		http.Error(w, "could not read uploaded file", http.StatusBadRequest)
		return
	}
	if len(data) == 0 {
		http.Error(w, "uploaded file is empty", http.StatusBadRequest)
		return
	}

	// Validate it is a real EPUB by parsing it.
	md, err := epub.ParseBytes(data)
	if err != nil {
		http.Error(w, "uploaded file is not a valid EPUB", http.StatusBadRequest)
		return
	}

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	// Dedup: reject an EPUB whose content we already hold.
	switch _, err := a.store.GetBookByHash(ctx, hash); {
	case err == nil:
		http.Error(w, "book already exists", http.StatusConflict)
		return
	case errors.Is(err, store.ErrNotFound):
		// not a duplicate; continue
	default:
		serverError(w, "lookup by hash", err)
		return
	}

	filePath, coverPath, err := a.store.SaveBlobs(hash, data, md.CoverData)
	if err != nil {
		serverError(w, "save blobs", err)
		return
	}

	book := store.Book{
		Title:     uploadTitle(md, header.Filename),
		Author:    strings.TrimSpace(md.Author),
		CoverPath: coverPath,
		FilePath:  filePath,
		FileHash:  hash,
		SizeBytes: int64(len(data)),
	}
	saved, err := a.store.InsertBook(ctx, book)
	if err != nil {
		serverError(w, "insert book", err)
		return
	}

	// Fire-and-forget "Send to Kindle" when auto-send is configured. Done after
	// the row is persisted and never blocks or fails the upload response.
	a.maybeAutoDeliver(ctx, saved)

	entry := bookJSON{
		ID:     saved.ID,
		Title:  saved.Title,
		Author: saved.Author,
	}
	if saved.CoverPath != "" {
		entry.CoverURL = coverURL(saved.ID)
	}
	writeJSON(w, http.StatusCreated, entry)
}

// uploadTitle prefers the EPUB's declared title, falling back to the uploaded
// filename (minus extension) so a book never lands with an empty NOT NULL
// title column.
func uploadTitle(md *epub.Metadata, filename string) string {
	if t := strings.TrimSpace(md.Title); t != "" {
		return t
	}
	base := filepath.Base(filename)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if base = strings.TrimSpace(base); base != "" && base != "." && base != "/" {
		return base
	}
	return "Untitled"
}
