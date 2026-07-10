package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Batch statuses. A batch is open while its candidates are being reviewed, and
// becomes confirmed (all ready items shelved) or discarded.
const (
	BatchOpen      = "open"
	BatchConfirmed = "confirmed"
	BatchDiscarded = "discarded"
)

// Candidate statuses mirror the ISBN-ingest handoff's pipeline contract: a scan
// resolves to ready (a single high-confidence edition), review (several editions
// to disambiguate), no_match (no DRM edition found), or duplicate (already in the
// library / seen earlier in the batch). confirmed and skipped are terminal review
// outcomes.
const (
	CandidateReady     = "ready"
	CandidateReview    = "review"
	CandidateNoMatch   = "no_match"
	CandidateDuplicate = "duplicate"
	CandidateConfirmed = "confirmed"
	CandidateSkipped   = "skipped"
)

// Scan sources: a camera decode, a manually typed ISBN, or a scanner-free
// add-by-title pick.
const (
	SourceCamera = "camera"
	SourceManual = "manual"
	SourceTitle  = "title"
)

// Edition is one candidate match for a scanned ISBN: the metadata a Resolver
// returns for a book. It is persisted verbatim in a candidate's editions JSONB
// so the desktop reviewer can pick between several (reissues, box sets,
// translations) without a second lookup. ID is stable within a candidate's set
// (the edition's ISBN-13 when known, else the resolver's own key).
type Edition struct {
	ID        string `json:"id"`
	ISBN13    string `json:"isbn13,omitempty"`
	Title     string `json:"title"`
	Author    string `json:"author,omitempty"`
	Publisher string `json:"publisher,omitempty"`
	Year      string `json:"year,omitempty"`
	Pages     int    `json:"pages,omitempty"`
	Language  string `json:"language,omitempty"`
	CoverURL  string `json:"cover_url,omitempty"`
}

// Batch is one upload of scanned ISBNs awaiting review (migration 0007).
type Batch struct {
	ID           int64
	SourceDevice string
	Status       string
	CreatedAt    time.Time
}

// Candidate is a single scan resolved to a review row: its normalized ISBN, the
// matched Editions and chosen one, a confidence score, and the series-assignment
// intent a Confirm carries forward. It is transient review state — confirming it
// produces the durable inventory row, after which the candidate is done.
type Candidate struct {
	ID              int64
	BatchID         int64
	ISBN            string
	CapturedAt      time.Time
	Source          string
	Status          string
	Confidence      float64
	ChosenEditionID string
	Editions        []Edition
	Title           string
	Author          string
	CoverURL        string
	Series          string
	SeriesIndex     float64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ChosenEdition returns the edition matching ChosenEditionID, or — when nothing
// was explicitly picked but exactly one edition was matched — that sole edition.
// ok is false when there is no unambiguous choice (a review row with several
// editions and no pick, or a no_match with none).
func (c Candidate) ChosenEdition() (Edition, bool) {
	if c.ChosenEditionID != "" {
		for _, e := range c.Editions {
			if e.ID == c.ChosenEditionID {
				return e, true
			}
		}
		return Edition{}, false
	}
	if len(c.Editions) == 1 {
		return c.Editions[0], true
	}
	return Edition{}, false
}

const batchColumns = `id, source_device, status, created_at`

func scanBatch(row pgx.Row) (Batch, error) {
	var b Batch
	err := row.Scan(&b.ID, &b.SourceDevice, &b.Status, &b.CreatedAt)
	return b, err
}

// CreateBatch opens a new review batch for scans uploaded from sourceDevice.
func (s *Store) CreateBatch(ctx context.Context, sourceDevice string) (Batch, error) {
	b, err := scanBatch(s.pool.QueryRow(ctx,
		`INSERT INTO ingest_batches (source_device) VALUES ($1)
		 RETURNING `+batchColumns, sourceDevice))
	if err != nil {
		return Batch{}, fmt.Errorf("store: create batch: %w", err)
	}
	return b, nil
}

// GetBatch returns the batch with id, or ErrNotFound.
func (s *Store) GetBatch(ctx context.Context, id int64) (Batch, error) {
	b, err := scanBatch(s.pool.QueryRow(ctx,
		`SELECT `+batchColumns+` FROM ingest_batches WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Batch{}, ErrNotFound
	}
	if err != nil {
		return Batch{}, fmt.Errorf("store: get batch: %w", err)
	}
	return b, nil
}

// ListBatches returns all batches, most recent first.
func (s *Store) ListBatches(ctx context.Context) ([]Batch, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+batchColumns+` FROM ingest_batches ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("store: list batches: %w", err)
	}
	defer rows.Close()

	var out []Batch
	for rows.Next() {
		b, err := scanBatch(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan batch: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list batches: %w", err)
	}
	return out, nil
}

// SetBatchStatus transitions a batch to status, returning the updated row or
// ErrNotFound.
func (s *Store) SetBatchStatus(ctx context.Context, id int64, status string) (Batch, error) {
	b, err := scanBatch(s.pool.QueryRow(ctx,
		`UPDATE ingest_batches SET status = $2 WHERE id = $1 RETURNING `+batchColumns,
		id, status))
	if errors.Is(err, pgx.ErrNoRows) {
		return Batch{}, ErrNotFound
	}
	if err != nil {
		return Batch{}, fmt.Errorf("store: set batch status: %w", err)
	}
	return b, nil
}

const candidateColumns = `id, batch_id, isbn, captured_at, source, status,
	confidence, chosen_edition_id, editions, title, author, cover_url,
	series, series_index, created_at, updated_at`

func scanCandidate(row pgx.Row) (Candidate, error) {
	var c Candidate
	var editionsJSON []byte
	err := row.Scan(&c.ID, &c.BatchID, &c.ISBN, &c.CapturedAt, &c.Source,
		&c.Status, &c.Confidence, &c.ChosenEditionID, &editionsJSON,
		&c.Title, &c.Author, &c.CoverURL, &c.Series, &c.SeriesIndex,
		&c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return Candidate{}, err
	}
	if len(editionsJSON) > 0 {
		if err := json.Unmarshal(editionsJSON, &c.Editions); err != nil {
			return Candidate{}, fmt.Errorf("store: decode editions: %w", err)
		}
	}
	return c, nil
}

// AddCandidate inserts a resolved scan as a review row and returns it. A zero
// CapturedAt defaults to now().
func (s *Store) AddCandidate(ctx context.Context, c Candidate) (Candidate, error) {
	editions, err := marshalEditions(c.Editions)
	if err != nil {
		return Candidate{}, err
	}
	saved, err := scanCandidate(s.pool.QueryRow(ctx,
		`INSERT INTO ingest_candidates
		   (batch_id, isbn, captured_at, source, status, confidence,
		    chosen_edition_id, editions, title, author, cover_url, series, series_index)
		 VALUES ($1, $2, COALESCE($3, now()), $4, $5, $6, $7, $8::jsonb, $9, $10, $11, $12, $13)
		 RETURNING `+candidateColumns,
		c.BatchID, c.ISBN, nullTime(c.CapturedAt), c.Source, c.Status, c.Confidence,
		c.ChosenEditionID, editions, c.Title, c.Author, c.CoverURL, c.Series, c.SeriesIndex))
	if err != nil {
		return Candidate{}, fmt.Errorf("store: add candidate: %w", err)
	}
	return saved, nil
}

// GetCandidate returns the candidate with id, or ErrNotFound.
func (s *Store) GetCandidate(ctx context.Context, id int64) (Candidate, error) {
	c, err := scanCandidate(s.pool.QueryRow(ctx,
		`SELECT `+candidateColumns+` FROM ingest_candidates WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("store: get candidate: %w", err)
	}
	return c, nil
}

// ListCandidates returns a batch's candidates in insertion order (the order they
// were scanned).
func (s *Store) ListCandidates(ctx context.Context, batchID int64) ([]Candidate, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+candidateColumns+` FROM ingest_candidates
		 WHERE batch_id = $1 ORDER BY id ASC`, batchID)
	if err != nil {
		return nil, fmt.Errorf("store: list candidates: %w", err)
	}
	defer rows.Close()

	var out []Candidate
	for rows.Next() {
		c, err := scanCandidate(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan candidate: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list candidates: %w", err)
	}
	return out, nil
}

// UpdateCandidate writes a candidate's mutable review fields (status, confidence,
// chosen edition, resolved metadata, series intent) by ID and returns the stored
// row, or ErrNotFound. It never moves a candidate between batches.
func (s *Store) UpdateCandidate(ctx context.Context, c Candidate) (Candidate, error) {
	editions, err := marshalEditions(c.Editions)
	if err != nil {
		return Candidate{}, err
	}
	saved, err := scanCandidate(s.pool.QueryRow(ctx,
		`UPDATE ingest_candidates SET
		   status = $2, confidence = $3, chosen_edition_id = $4, editions = $5::jsonb,
		   title = $6, author = $7, cover_url = $8, series = $9, series_index = $10,
		   updated_at = now()
		 WHERE id = $1
		 RETURNING `+candidateColumns,
		c.ID, c.Status, c.Confidence, c.ChosenEditionID, editions,
		c.Title, c.Author, c.CoverURL, c.Series, c.SeriesIndex))
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("store: update candidate: %w", err)
	}
	return saved, nil
}

// marshalEditions encodes an edition slice for the JSONB column, using an empty
// array (not SQL NULL) for none so the column's NOT NULL default holds.
func marshalEditions(eds []Edition) (string, error) {
	if len(eds) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(eds)
	if err != nil {
		return "", fmt.Errorf("store: encode editions: %w", err)
	}
	return string(b), nil
}

// nullTime maps a zero time to SQL NULL so an INSERT's COALESCE(..., now())
// default applies; a set time is passed through.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
