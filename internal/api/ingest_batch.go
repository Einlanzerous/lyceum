package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/magos/lyceum/internal/isbn"
	"github.com/magos/lyceum/internal/store"
)

// Resolver maps a scanned ISBN (or a free-text title) to candidate book
// editions. It is the "match" stage of ISBN ingest (LYCM-603): the edition
// package's Open Library resolver is the shipped implementation, injected via
// WithResolver. The default is a no-op so, unconfigured, every scan cleanly
// resolves to no_match rather than erroring.
type Resolver interface {
	ResolveISBN(ctx context.Context, code string) ([]store.Edition, error)
	SearchTitle(ctx context.Context, query string) ([]store.Edition, error)
}

// nullResolver is the no-op default: it matches nothing, so a batch uploaded
// without a configured resolver comes back all no_match instead of failing.
type nullResolver struct{}

func (nullResolver) ResolveISBN(context.Context, string) ([]store.Edition, error) {
	return nil, nil
}
func (nullResolver) SearchTitle(context.Context, string) ([]store.Edition, error) {
	return nil, nil
}

// WithResolver installs an edition resolver (e.g. the Open Library client) used
// to match scanned ISBNs during batch ingest. Without it, batches resolve to
// no_match only.
func WithResolver(r Resolver) Option {
	return func(a *API) { a.resolver = r }
}

// Confidence bands. A single-edition match is confidently ready (above the 0.80
// review threshold from the handoff); an ambiguous multi-edition match is held
// for review; a manual pick resolves to a full 1.0.
const (
	readyConfidence  = 0.95
	reviewConfidence = 0.6
	pickedConfidence = 1.0
)

// errNoChoice / errNoISBN are the confirm-path 400s: a candidate can only be
// confirmed once it has an unambiguous edition and a valid ISBN.
var (
	errNoChoice = errors.New("candidate has no chosen edition")
	errNoISBN   = errors.New("candidate has no valid ISBN")
)

// ---- wire shapes ----

type candidateJSON struct {
	ID              int64           `json:"id"`
	BatchID         int64           `json:"batch_id"`
	ISBN            string          `json:"isbn"`
	Source          string          `json:"source"`
	Status          string          `json:"status"`
	Confidence      float64         `json:"confidence"`
	ChosenEditionID string          `json:"chosen_edition_id,omitempty"`
	Editions        []store.Edition `json:"editions"`
	Title           string          `json:"title,omitempty"`
	Author          string          `json:"author,omitempty"`
	CoverURL        string          `json:"cover_url,omitempty"`
	Series          string          `json:"series,omitempty"`
	SeriesIndex     float64         `json:"series_index,omitempty"`
	CapturedAt      string          `json:"captured_at,omitempty"`
}

type batchJSON struct {
	ID           int64           `json:"id"`
	SourceDevice string          `json:"source_device,omitempty"`
	Status       string          `json:"status"`
	CreatedAt    string          `json:"created_at"`
	Counts       map[string]int  `json:"counts"`
	Candidates   []candidateJSON `json:"candidates,omitempty"`
}

func toCandidateJSON(c store.Candidate) candidateJSON {
	eds := c.Editions
	if eds == nil {
		eds = []store.Edition{}
	}
	out := candidateJSON{
		ID:              c.ID,
		BatchID:         c.BatchID,
		ISBN:            c.ISBN,
		Source:          c.Source,
		Status:          c.Status,
		Confidence:      c.Confidence,
		ChosenEditionID: c.ChosenEditionID,
		Editions:        eds,
		Title:           c.Title,
		Author:          c.Author,
		CoverURL:        c.CoverURL,
		Series:          c.Series,
		SeriesIndex:     c.SeriesIndex,
	}
	if !c.CapturedAt.IsZero() {
		out.CapturedAt = c.CapturedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func toBatchJSON(b store.Batch, candidates []store.Candidate) batchJSON {
	out := batchJSON{
		ID:           b.ID,
		SourceDevice: b.SourceDevice,
		Status:       b.Status,
		CreatedAt:    b.CreatedAt.UTC().Format(time.RFC3339),
		Counts:       map[string]int{},
	}
	for _, c := range candidates {
		out.Counts[c.Status]++
		out.Candidates = append(out.Candidates, toCandidateJSON(c))
	}
	return out
}

// ---- batch create + resolve ----

type scanJSON struct {
	ISBN       string `json:"isbn"`
	CapturedAt string `json:"captured_at"`
	Source     string `json:"source"`
}

type batchCreateRequest struct {
	SourceDevice string     `json:"source_device"`
	Scans        []scanJSON `json:"scans"`
}

// handleBatchCreate accepts a batch of scans, resolves each to a candidate
// (normalize → dedupe → match), persists the batch, and returns it with all
// candidates and a status histogram. Malformed reads become no_match candidates
// rather than being dropped, so nothing scanned goes missing from review.
func (a *API) handleBatchCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req batchCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	batch, err := a.store.CreateBatch(ctx, strings.TrimSpace(req.SourceDevice))
	if err != nil {
		serverError(w, "create batch", err)
		return
	}

	seen := map[string]bool{}
	saved := make([]store.Candidate, 0, len(req.Scans))
	for _, sc := range req.Scans {
		c := a.resolveScan(ctx, batch.ID, sc, seen)
		stored, err := a.store.AddCandidate(ctx, c)
		if err != nil {
			serverError(w, "add candidate", err)
			return
		}
		saved = append(saved, stored)
	}

	writeJSON(w, http.StatusCreated, toBatchJSON(batch, saved))
}

// resolveScan turns one scan into an unsaved candidate: it normalizes the ISBN,
// flags intra-batch and library duplicates, and otherwise matches the ISBN to
// editions and scores the result. seen tracks ISBNs already resolved in this
// batch so a repeated barcode is flagged duplicate instead of resolved twice.
func (a *API) resolveScan(ctx context.Context, batchID int64, sc scanJSON, seen map[string]bool) store.Candidate {
	c := store.Candidate{
		BatchID:    batchID,
		Source:     normalizeSource(sc.Source),
		CapturedAt: parseCapturedAt(sc.CapturedAt),
	}

	code, err := isbn.Normalize(sc.ISBN)
	if err != nil {
		// A malformed read (bad checksum, non-ISBN barcode) is surfaced as
		// no_match carrying the raw text so the reviewer can retype it.
		c.ISBN = strings.TrimSpace(sc.ISBN)
		c.Status = store.CandidateNoMatch
		return c
	}
	c.ISBN = code

	if seen[code] {
		c.Status = store.CandidateDuplicate
		return c
	}
	seen[code] = true

	// Library dedupe: an ISBN already linked to an ingested book is a duplicate
	// of something on the shelf. An owned-but-not-yet-digital inventory row is not
	// a library duplicate — it is exactly what we're trying to acquire.
	if inv, err := a.store.GetInventoryByISBN(ctx, code); err == nil && inv.BookID != nil {
		c.Status = store.CandidateDuplicate
		c.Title, c.Author = inv.Title, inv.Author
		return c
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		log.Printf("api: dedupe inventory isbn=%s: %v", code, err)
	}

	editions, err := a.resolver.ResolveISBN(ctx, code)
	if err != nil {
		// A resolver failure is not fatal to the batch: the scan simply lands as
		// no_match and can be retried from the review screen.
		log.Printf("api: resolve isbn=%s: %v", code, err)
		editions = nil
	}
	applyMatch(&c, editions)
	return c
}

// applyMatch scores a resolved edition set onto a candidate: none => no_match,
// one => ready (pre-chosen), several => review (reviewer disambiguates).
func applyMatch(c *store.Candidate, editions []store.Edition) {
	c.Editions = editions
	switch len(editions) {
	case 0:
		c.Status = store.CandidateNoMatch
		c.Confidence = 0
	case 1:
		c.Status = store.CandidateReady
		c.Confidence = readyConfidence
		c.ChosenEditionID = editions[0].ID
		fillFromEdition(c, editions[0])
	default:
		c.Status = store.CandidateReview
		c.Confidence = reviewConfidence
	}
}

// fillFromEdition copies a chosen edition's display metadata onto the candidate
// so the queue and detail render without re-reading editions[].
func fillFromEdition(c *store.Candidate, e store.Edition) {
	c.Title = e.Title
	c.Author = e.Author
	c.CoverURL = e.CoverURL
}

// ---- batch/candidate reads ----

func (a *API) handleBatchList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	batches, err := a.store.ListBatches(ctx)
	if err != nil {
		serverError(w, "list batches", err)
		return
	}
	out := make([]batchJSON, 0, len(batches))
	for _, b := range batches {
		cands, err := a.store.ListCandidates(ctx, b.ID)
		if err != nil {
			serverError(w, "list candidates", err)
			return
		}
		bj := toBatchJSON(b, cands)
		bj.Candidates = nil // list view is headers + counts only
		out = append(out, bj)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleBatchGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, ok := a.lookupBatch(w, r)
	if !ok {
		return
	}
	cands, err := a.store.ListCandidates(ctx, b.ID)
	if err != nil {
		serverError(w, "list candidates", err)
		return
	}
	writeJSON(w, http.StatusOK, toBatchJSON(b, cands))
}

// ---- candidate actions ----

type pickRequest struct {
	EditionID string `json:"edition_id"`
}

// handleCandidatePick resolves an ambiguous (review) candidate to one edition:
// it records the choice, promotes the candidate to ready, and sets confidence to
// a full 1.0 (a human pick is authoritative).
func (a *API) handleCandidatePick(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, ok := a.lookupCandidate(w, r)
	if !ok {
		return
	}
	var req pickRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	var chosen store.Edition
	found := false
	for _, e := range c.Editions {
		if e.ID == req.EditionID {
			chosen, found = e, true
			break
		}
	}
	if !found {
		http.Error(w, "edition_id not among candidate editions", http.StatusBadRequest)
		return
	}
	c.ChosenEditionID = chosen.ID
	c.Confidence = pickedConfidence
	c.Status = store.CandidateReady
	fillFromEdition(&c, chosen)

	saved, err := a.store.UpdateCandidate(ctx, c)
	if err != nil {
		serverError(w, "update candidate", err)
		return
	}
	writeJSON(w, http.StatusOK, toCandidateJSON(saved))
}

type confirmRequest struct {
	Series      string  `json:"series"`
	SeriesIndex float64 `json:"series_index"`
}

type confirmResponse struct {
	Candidate candidateJSON `json:"candidate"`
	Inventory inventoryJSON `json:"inventory"`
}

// handleCandidateConfirm confirms a single candidate: it writes the chosen
// edition into inventory (owned) and, when no digital copy is in hand, requests
// one via the acquirer (moving it to wanted) so the acquisition pipeline can
// pick it up. Series assignment is carried on the candidate as intent.
func (a *API) handleCandidateConfirm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, ok := a.lookupCandidate(w, r)
	if !ok {
		return
	}
	var req confirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	inv, saved, err := a.confirmCandidate(ctx, c, strings.TrimSpace(req.Series), req.SeriesIndex)
	switch {
	case errors.Is(err, errNoChoice):
		http.Error(w, "pick an edition before confirming", http.StatusBadRequest)
		return
	case errors.Is(err, errNoISBN):
		http.Error(w, "candidate has no valid ISBN to confirm", http.StatusBadRequest)
		return
	case err != nil:
		serverError(w, "confirm candidate", err)
		return
	}
	// This may have resolved the last reviewable candidate; close the batch so it
	// doesn't stay open with nothing left to review.
	a.closeBatchIfDone(ctx, saved.BatchID)
	writeJSON(w, http.StatusOK, confirmResponse{
		Candidate: toCandidateJSON(saved),
		Inventory: toInventoryJSON(inv),
	})
}

// confirmCandidate is the shared confirm path (single confirm + batch
// confirm-all-ready). It is a no-op-safe transition: an already-owned title stays
// owned/ingested, a fresh one is recorded owned then requested (wanted).
func (a *API) confirmCandidate(ctx context.Context, c store.Candidate, series string, seriesIndex float64) (store.Inventory, store.Candidate, error) {
	chosen, ok := c.ChosenEdition()
	if !ok {
		return store.Inventory{}, store.Candidate{}, errNoChoice
	}
	if !isbn.Valid(c.ISBN) {
		return store.Inventory{}, store.Candidate{}, errNoISBN
	}

	inv, err := a.store.UpsertInventory(ctx, store.Inventory{
		ISBN:   c.ISBN,
		Title:  chosen.Title,
		Author: chosen.Author,
		WorkID: chosen.WorkID, // resolved at batch time; groups print/ebook editions (LYCM-35)
	})
	if err != nil {
		return store.Inventory{}, store.Candidate{}, err
	}
	if inv.State != store.StateIngested {
		// Record `wanted` first, then dispatch the actual grab in the background
		// (LYCM-79): a batch confirm used to block seconds × N on the acquisition
		// backend's synchronous search. A dispatch failure only logs — `wanted` is
		// already the right resting state for a grab that didn't happen.
		inv, err = a.store.SetInventoryState(ctx, c.ISBN, store.StateWanted)
		if err != nil {
			return store.Inventory{}, store.Candidate{}, err
		}
		a.dispatchWant(c.ISBN)
	}

	c.Status = store.CandidateConfirmed
	c.ChosenEditionID = chosen.ID
	fillFromEdition(&c, chosen)
	if series != "" {
		c.Series = series
		c.SeriesIndex = seriesIndex
	}
	saved, err := a.store.UpdateCandidate(ctx, c)
	if err != nil {
		return store.Inventory{}, store.Candidate{}, err
	}
	return inv, saved, nil
}

// handleCandidateSkip removes a candidate from review without shelving it.
func (a *API) handleCandidateSkip(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, ok := a.lookupCandidate(w, r)
	if !ok {
		return
	}
	c.Status = store.CandidateSkipped
	if _, err := a.store.UpdateCandidate(ctx, c); err != nil {
		serverError(w, "skip candidate", err)
		return
	}
	// Skipping the last reviewable candidate also completes the batch.
	a.closeBatchIfDone(ctx, c.BatchID)
	w.WriteHeader(http.StatusNoContent)
}

type confirmReadyResponse struct {
	Confirmed int       `json:"confirmed"`
	Batch     batchJSON `json:"batch"`
}

// handleBatchConfirmReady confirms every ready candidate in a batch in one go
// (the "Confirm N & add to library" header action). When no reviewable
// candidates remain afterwards, the batch itself is marked confirmed.
func (a *API) handleBatchConfirmReady(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, ok := a.lookupBatch(w, r)
	if !ok {
		return
	}
	cands, err := a.store.ListCandidates(ctx, b.ID)
	if err != nil {
		serverError(w, "list candidates", err)
		return
	}

	confirmed := 0
	for _, c := range cands {
		if c.Status != store.CandidateReady {
			continue
		}
		if _, _, err := a.confirmCandidate(ctx, c, c.Series, c.SeriesIndex); err != nil {
			log.Printf("api: confirm-ready candidate %d: %v", c.ID, err)
			continue
		}
		confirmed++
	}

	// Reload to reflect the confirmations and decide whether the batch is done.
	cands, err = a.store.ListCandidates(ctx, b.ID)
	if err != nil {
		serverError(w, "reload candidates", err)
		return
	}
	if !hasReviewable(cands) {
		if updated, err := a.store.SetBatchStatus(ctx, b.ID, store.BatchConfirmed); err != nil {
			log.Printf("api: mark batch %d confirmed: %v", b.ID, err)
		} else {
			b = updated
		}
	}

	writeJSON(w, http.StatusOK, confirmReadyResponse{
		Confirmed: confirmed,
		Batch:     toBatchJSON(b, cands),
	})
}

// hasReviewable reports whether any candidate still needs attention (a ready item
// not yet confirmed, or one held for review).
func hasReviewable(cands []store.Candidate) bool {
	for _, c := range cands {
		if c.Status == store.CandidateReady || c.Status == store.CandidateReview {
			return true
		}
	}
	return false
}

// closeBatchIfDone transitions a batch to confirmed once none of its candidates
// are still reviewable (ready/review) — i.e. everything has been confirmed,
// skipped, or was unmatched. It is called after a single confirm/skip so a
// fully-resolved batch stops lingering in the open list (the batch-level
// confirm-ready path does the same check inline). Best-effort: it is invoked
// after the candidate action already succeeded, so any failure is logged, not
// surfaced. Only an open batch is closed, so a discarded batch is left alone.
func (a *API) closeBatchIfDone(ctx context.Context, batchID int64) {
	cands, err := a.store.ListCandidates(ctx, batchID)
	if err != nil {
		log.Printf("api: close-check batch %d: list candidates: %v", batchID, err)
		return
	}
	if hasReviewable(cands) {
		return
	}
	b, err := a.store.GetBatch(ctx, batchID)
	if err != nil {
		log.Printf("api: close-check batch %d: get batch: %v", batchID, err)
		return
	}
	if b.Status != store.BatchOpen {
		return
	}
	if _, err := a.store.SetBatchStatus(ctx, batchID, store.BatchConfirmed); err != nil {
		log.Printf("api: mark batch %d confirmed: %v", batchID, err)
	}
}

// ---- add-by-title ----

// handleIngestSearch backs the scanner-free add-by-title box: it returns the
// editions matching a free-text query for the reviewer to pick from.
func (a *API) handleIngestSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"editions": []store.Edition{}})
		return
	}
	editions, err := a.resolver.SearchTitle(r.Context(), q)
	if err != nil {
		serverError(w, "search editions", err)
		return
	}
	if editions == nil {
		editions = []store.Edition{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"editions": editions})
}

type addCandidateRequest struct {
	ISBN   string `json:"isbn"`
	Source string `json:"source"`
}

// handleBatchAddCandidate appends a single scan/pick to an existing batch — used
// by add-by-title (the picked edition's ISBN) and manual entry. It dedupes
// against the batch's existing candidates.
func (a *API) handleBatchAddCandidate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, ok := a.lookupBatch(w, r)
	if !ok {
		return
	}
	var req addCandidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	existing, err := a.store.ListCandidates(ctx, b.ID)
	if err != nil {
		serverError(w, "list candidates", err)
		return
	}
	seen := map[string]bool{}
	for _, c := range existing {
		if c.ISBN != "" {
			seen[c.ISBN] = true
		}
	}

	source := normalizeSource(req.Source)
	if source == store.SourceCamera {
		source = store.SourceTitle // added from desktop, not a camera capture
	}
	c := a.resolveScan(ctx, b.ID, scanJSON{ISBN: req.ISBN, Source: source}, seen)
	saved, err := a.store.AddCandidate(ctx, c)
	if err != nil {
		serverError(w, "add candidate", err)
		return
	}
	writeJSON(w, http.StatusCreated, toCandidateJSON(saved))
}

// ---- helpers ----

func (a *API) lookupBatch(w http.ResponseWriter, r *http.Request) (store.Batch, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid batch id", http.StatusBadRequest)
		return store.Batch{}, false
	}
	b, err := a.store.GetBatch(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "batch not found", http.StatusNotFound)
		return store.Batch{}, false
	}
	if err != nil {
		serverError(w, "get batch", err)
		return store.Batch{}, false
	}
	return b, true
}

func (a *API) lookupCandidate(w http.ResponseWriter, r *http.Request) (store.Candidate, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid candidate id", http.StatusBadRequest)
		return store.Candidate{}, false
	}
	c, err := a.store.GetCandidate(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "candidate not found", http.StatusNotFound)
		return store.Candidate{}, false
	}
	if err != nil {
		serverError(w, "get candidate", err)
		return store.Candidate{}, false
	}
	return c, true
}

// normalizeSource clamps a scan source to a known value, defaulting to camera.
func normalizeSource(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case store.SourceManual:
		return store.SourceManual
	case store.SourceTitle:
		return store.SourceTitle
	default:
		return store.SourceCamera
	}
}

// parseCapturedAt parses an RFC3339 capture time, returning the zero time (which
// the store defaults to now()) when absent or malformed.
func parseCapturedAt(s string) time.Time {
	if strings.TrimSpace(s) == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
