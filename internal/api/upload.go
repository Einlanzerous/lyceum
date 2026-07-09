package api

import (
	"errors"
	"io"
	"net/http"
)

// maxUploadBytes caps an uploaded EPUB. It is generous (200 MiB) but bounds
// memory: the whole file is read into RAM to hash and parse it.
const maxUploadBytes = 200 << 20

// handleUpload ingests an EPUB from a multipart/form-data request (field
// "file"). It reads and bounds the body, then hands the bytes to the shared
// ingestEPUB core, mapping its result to HTTP: 201 with the created book JSON,
// 400 for a non-EPUB, 409 for a duplicate.
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

	// Uploads have no stable filesystem identity, so pass an empty sourcePath:
	// the only non-created outcome is a content duplicate (409). Replace-on-
	// restamp is exclusive to the folder watcher, which knows the file path.
	saved, result, err := a.ingestEPUB(ctx, data, header.Filename, "")
	switch {
	case errors.Is(err, errNotEPUB):
		http.Error(w, "uploaded file is not a valid EPUB", http.StatusBadRequest)
		return
	case err != nil:
		serverError(w, "ingest epub", err)
		return
	case result != ingestCreated:
		http.Error(w, "book already exists", http.StatusConflict)
		return
	}

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
