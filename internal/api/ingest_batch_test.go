package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/magos/lyceum/internal/store"
)

// fakeResolver is a deterministic, network-free Resolver for handler tests.
type fakeResolver struct {
	byISBN  map[string][]store.Edition
	byTitle map[string][]store.Edition
}

func (f fakeResolver) ResolveISBN(_ context.Context, code string) ([]store.Edition, error) {
	return f.byISBN[code], nil
}
func (f fakeResolver) SearchTitle(_ context.Context, q string) ([]store.Edition, error) {
	return f.byTitle[q], nil
}

const (
	isbnPiranesi = "9781635575637" // resolves to one edition -> ready
	workPiranesi = "/works/OL21067624W"
	isbnDune     = "9780441172719" // resolves to two editions -> review
	isbnDune2    = "9781473233966" // the second Dune edition
	isbnNoMatch  = "9780306406157" // valid checksum, resolver returns nothing
)

func testResolver() fakeResolver {
	return fakeResolver{
		byISBN: map[string][]store.Edition{
			isbnPiranesi: {{ID: isbnPiranesi, ISBN13: isbnPiranesi, WorkID: workPiranesi, Title: "Piranesi", Author: "Susanna Clarke", CoverURL: "https://c/p.jpg"}},
			isbnDune: {
				{ID: isbnDune, ISBN13: isbnDune, Title: "Dune", Author: "Frank Herbert", Publisher: "Ace"},
				{ID: isbnDune2, ISBN13: isbnDune2, Title: "Dune", Author: "Frank Herbert", Publisher: "Gollancz"},
			},
		},
		byTitle: map[string][]store.Edition{
			"Dune": {{ID: isbnDune, ISBN13: isbnDune, Title: "Dune", Author: "Frank Herbert"}},
		},
	}
}

func ingestServer(t *testing.T, acq Acquirer) (*httptest.Server, *store.Store) {
	t.Helper()
	s := testStore(t)
	opts := []Option{WithResolver(testResolver())}
	if acq != nil {
		opts = append(opts, WithAcquirer(acq))
	}
	srv := httptest.NewServer(New(s, "", opts...).Handler())
	t.Cleanup(srv.Close)
	return srv, s
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func decodeBatch(t *testing.T, resp *http.Response) batchJSON {
	t.Helper()
	defer resp.Body.Close()
	var b batchJSON
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatalf("decode batch: %v", err)
	}
	return b
}

// A batch of scans is classified into ready / review / no_match / duplicate, and
// nothing scanned goes missing: a malformed read and an intra-batch repeat both
// land in the queue rather than being dropped.
func TestBatchCreateClassifies(t *testing.T) {
	srv, _ := ingestServer(t, nil)

	resp := postJSON(t, srv.URL+"/ingest/batches", map[string]any{
		"source_device": "iPhone",
		"scans": []map[string]any{
			{"isbn": "978-1-63557-563-7", "source": "camera"}, // ready (hyphenated)
			{"isbn": isbnDune, "source": "camera"},            // review (2 editions)
			{"isbn": isbnNoMatch, "source": "camera"},         // no_match (resolver empty)
			{"isbn": "not-a-barcode", "source": "camera"},     // no_match (malformed)
			{"isbn": isbnPiranesi, "source": "camera"},        // duplicate (repeat)
		},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	b := decodeBatch(t, resp)

	if b.Counts[store.CandidateReady] != 1 ||
		b.Counts[store.CandidateReview] != 1 ||
		b.Counts[store.CandidateNoMatch] != 2 ||
		b.Counts[store.CandidateDuplicate] != 1 {
		t.Fatalf("counts = %v, want ready1/review1/no_match2/duplicate1", b.Counts)
	}
	if len(b.Candidates) != 5 {
		t.Fatalf("candidates = %d, want 5", len(b.Candidates))
	}

	// The ready candidate carries the normalized ISBN, a pre-chosen edition, and
	// a confidence above the review threshold.
	ready := b.Candidates[0]
	if ready.Status != store.CandidateReady || ready.ISBN != isbnPiranesi {
		t.Fatalf("first candidate = %+v, want ready %s", ready, isbnPiranesi)
	}
	if ready.ChosenEditionID != isbnPiranesi || ready.Title != "Piranesi" || ready.Confidence < 0.8 {
		t.Fatalf("ready candidate not resolved: %+v", ready)
	}
}

// Picking an edition promotes a review candidate to ready at full confidence.
func TestCandidatePick(t *testing.T) {
	srv, _ := ingestServer(t, nil)
	b := decodeBatch(t, postJSON(t, srv.URL+"/ingest/batches", map[string]any{
		"scans": []map[string]any{{"isbn": isbnDune}},
	}))
	c := b.Candidates[0]
	if c.Status != store.CandidateReview {
		t.Fatalf("status = %q, want review", c.Status)
	}

	resp := postJSON(t, srv.URL+"/ingest/candidates/"+itoa(c.ID)+"/pick",
		map[string]any{"edition_id": isbnDune2})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pick status = %d, want 200", resp.StatusCode)
	}
	defer resp.Body.Close()
	var got candidateJSON
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != store.CandidateReady || got.ChosenEditionID != isbnDune2 || got.Confidence != 1.0 {
		t.Fatalf("after pick = %+v, want ready/%s/1.0", got, isbnDune2)
	}
}

// Confirming a ready candidate records ownership and requests a digital copy,
// moving the inventory entry to wanted and calling the acquirer.
func TestCandidateConfirm(t *testing.T) {
	acq := &recordingAcquirer{}
	srv, s := ingestServer(t, acq)
	b := decodeBatch(t, postJSON(t, srv.URL+"/ingest/batches", map[string]any{
		"scans": []map[string]any{{"isbn": isbnPiranesi}},
	}))
	c := b.Candidates[0]

	resp := postJSON(t, srv.URL+"/ingest/candidates/"+itoa(c.ID)+"/confirm",
		map[string]any{"series": "Piranesi Cycle", "series_index": 1})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm status = %d, want 200", resp.StatusCode)
	}
	defer resp.Body.Close()
	var got confirmResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Candidate.Status != store.CandidateConfirmed || got.Candidate.Series != "Piranesi Cycle" {
		t.Fatalf("candidate = %+v, want confirmed with series", got.Candidate)
	}
	if got.Inventory.State != store.StateWanted || got.Inventory.ISBN != isbnPiranesi {
		t.Fatalf("inventory = %+v, want wanted %s", got.Inventory, isbnPiranesi)
	}
	if len(acq.wants) != 1 || acq.wants[0] != isbnPiranesi {
		t.Fatalf("acquirer wants = %v", acq.wants)
	}
	// The ownership row is persisted, carrying the chosen edition's work key so a
	// later ebook ingest of the same work reconciles onto it (LYCM-35).
	if inv, err := s.GetInventoryByISBN(context.Background(), isbnPiranesi); err != nil || inv.State != store.StateWanted {
		t.Fatalf("stored inventory = %+v err=%v", inv, err)
	} else if inv.WorkID != workPiranesi {
		t.Fatalf("stored work_id = %q, want %q", inv.WorkID, workPiranesi)
	}
}

// Confirm-ready shelves every ready item at once and only marks the batch
// confirmed when nothing reviewable remains.
func TestBatchConfirmReady(t *testing.T) {
	srv, _ := ingestServer(t, &recordingAcquirer{})
	b := decodeBatch(t, postJSON(t, srv.URL+"/ingest/batches", map[string]any{
		"scans": []map[string]any{{"isbn": isbnPiranesi}, {"isbn": isbnDune}},
	}))

	resp := postJSON(t, srv.URL+"/ingest/batches/"+itoa(b.ID)+"/confirm-ready", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm-ready status = %d, want 200", resp.StatusCode)
	}
	defer resp.Body.Close()
	var got confirmReadyResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Confirmed != 1 {
		t.Fatalf("confirmed = %d, want 1 (only the ready item)", got.Confirmed)
	}
	// The review item is still outstanding, so the batch stays open.
	if got.Batch.Status != store.BatchOpen {
		t.Fatalf("batch status = %q, want open (review outstanding)", got.Batch.Status)
	}
}

// Confirming/skipping candidates one at a time (what the web verify view does)
// must close the batch once nothing reviewable remains — otherwise a fully
// resolved batch lingers open with no visible candidates.
func TestSingleConfirmAndSkipCloseBatch(t *testing.T) {
	srv, _ := ingestServer(t, &recordingAcquirer{})
	b := decodeBatch(t, postJSON(t, srv.URL+"/ingest/batches", map[string]any{
		"scans": []map[string]any{{"isbn": isbnPiranesi}, {"isbn": isbnDune}},
	}))
	id := map[string]int64{}
	for _, c := range b.Candidates {
		id[c.ISBN] = c.ID
	}

	batchStatus := func() string {
		return decodeBatch(t, mustGet(t, srv.URL+"/ingest/batches/"+itoa(b.ID))).Status
	}

	// Confirm the ready item: the review item is still outstanding, so the batch
	// must stay open.
	resp := postJSON(t, srv.URL+"/ingest/candidates/"+itoa(id[isbnPiranesi])+"/confirm", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
	if s := batchStatus(); s != store.BatchOpen {
		t.Fatalf("batch status = %q after confirming 1 of 2, want open", s)
	}

	// Skip the last reviewable candidate: now nothing reviewable remains, so the
	// batch auto-closes to confirmed.
	resp = postJSON(t, srv.URL+"/ingest/candidates/"+itoa(id[isbnDune])+"/skip", map[string]any{})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("skip status = %d, want 204", resp.StatusCode)
	}
	resp.Body.Close()
	if s := batchStatus(); s != store.BatchConfirmed {
		t.Fatalf("batch status = %q after resolving all candidates, want confirmed", s)
	}
}

// A scan whose ISBN is already a shelved library book is flagged duplicate, not
// re-resolved.
func TestBatchDedupeAgainstLibrary(t *testing.T) {
	srv, s := ingestServer(t, nil)
	ctx := context.Background()
	book, err := s.InsertBook(ctx, store.Book{Title: "Piranesi", FilePath: "/blob/x", FileHash: "hash-x"})
	if err != nil {
		t.Fatalf("seed book: %v", err)
	}
	if _, err := s.LinkBookToInventory(ctx, isbnPiranesi, "", book.ID, "Piranesi", "Susanna Clarke"); err != nil {
		t.Fatalf("seed inventory link: %v", err)
	}

	b := decodeBatch(t, postJSON(t, srv.URL+"/ingest/batches", map[string]any{
		"scans": []map[string]any{{"isbn": isbnPiranesi}},
	}))
	if b.Candidates[0].Status != store.CandidateDuplicate {
		t.Fatalf("status = %q, want duplicate (already in library)", b.Candidates[0].Status)
	}
}

// Add-by-title: search returns editions, and adding the picked ISBN appends a
// ready candidate tagged as a title source.
func TestSearchAndAddByTitle(t *testing.T) {
	srv, _ := ingestServer(t, nil)

	sresp, err := http.Get(srv.URL + "/ingest/search?q=Dune")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer sresp.Body.Close()
	var sr struct {
		Editions []store.Edition `json:"editions"`
	}
	if err := json.NewDecoder(sresp.Body).Decode(&sr); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(sr.Editions) != 1 || sr.Editions[0].ID != isbnDune {
		t.Fatalf("search editions = %+v", sr.Editions)
	}

	b := decodeBatch(t, postJSON(t, srv.URL+"/ingest/batches", map[string]any{"scans": []map[string]any{}}))
	resp := postJSON(t, srv.URL+"/ingest/batches/"+itoa(b.ID)+"/candidates",
		map[string]any{"isbn": sr.Editions[0].ID, "source": "title"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add candidate status = %d, want 201", resp.StatusCode)
	}
	defer resp.Body.Close()
	var got candidateJSON
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Source != store.SourceTitle {
		t.Fatalf("source = %q, want title", got.Source)
	}
	if got.Status != store.CandidateReview && got.Status != store.CandidateReady {
		t.Fatalf("status = %q, want a resolved status", got.Status)
	}
}

// Skipping a candidate removes it from review.
func TestCandidateSkip(t *testing.T) {
	srv, _ := ingestServer(t, nil)
	b := decodeBatch(t, postJSON(t, srv.URL+"/ingest/batches", map[string]any{
		"scans": []map[string]any{{"isbn": isbnPiranesi}},
	}))
	resp := postJSON(t, srv.URL+"/ingest/candidates/"+itoa(b.Candidates[0].ID)+"/skip", map[string]any{})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("skip status = %d, want 204", resp.StatusCode)
	}
	resp.Body.Close()

	got := decodeBatch(t, mustGet(t, srv.URL+"/ingest/batches/"+itoa(b.ID)))
	if got.Candidates[0].Status != store.CandidateSkipped {
		t.Fatalf("status = %q, want skipped", got.Candidates[0].Status)
	}
}

func mustGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}
