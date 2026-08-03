package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	// replace-on-restamp is exclusive to the folder watcher, which knows the file
	// path. That leaves two non-created outcomes, both 409 — the same content
	// again, and a different file of a book already here (LYCM-113).
	//
	// ?force=true keeps the second anyway. The review queue holds only folder
	// ingests, so without an override the sole remedy the conflict can offer is
	// deleting the existing book — which takes its reading positions and read
	// marks with it (LYCM-112). That is a steep price for wanting a better scan
	// or a second translation, and precisely what the queue's hold exists to
	// avoid on the other path.
	var opts []ingestOption
	if forceUpload(r) {
		opts = append(opts, allowDuplicate())
	}
	saved, result, err := a.ingestEPUB(ctx, data, header.Filename, "", opts...)
	switch {
	case errors.Is(err, errNotEPUB):
		http.Error(w, "uploaded file is not a valid EPUB", http.StatusBadRequest)
		return
	case err != nil:
		serverError(w, "ingest epub", err)
		return
	case result == ingestPossibleDuplicate:
		// A different file that looks like a book already here (LYCM-113). The
		// review queue only holds new folder ingests, and an uploader is standing
		// right there, so say what it collided with rather than filing it
		// somewhere they would have to go looking.
		http.Error(w, fmt.Sprintf(
			"looks like another copy of %q by %s (book %d); re-upload with ?force=true to keep both",
			saved.Title, saved.Author, saved.ID), http.StatusConflict)
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

// forceUpload reports whether the request asked to keep a suspected duplicate
// (LYCM-113). Anything but an explicit true is a no, so a stray ?force= in a
// link cannot quietly disable the check.
func forceUpload(r *http.Request) bool {
	v, err := strconv.ParseBool(r.URL.Query().Get("force"))
	return err == nil && v
}
