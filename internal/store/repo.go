package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned by the Get* methods when no matching row exists.
var ErrNotFound = errors.New("store: not found")

// Book is a single ingested EPUB. The on-disk blobs live at FilePath (the
// EPUB) and CoverPath (the extracted cover, if any); the row is deduplicated
// on FileHash.
type Book struct {
	ID        int64
	Title     string
	Author    string
	CoverPath string
	FilePath  string
	FileHash  string
	SizeBytes int64
	// SourcePath is the watched-tree path this book was folder-ingested from,
	// or "" for HTTP uploads. It gives folder-ingested books a stable identity
	// so a re-stamped file updates in place instead of duplicating (LYCM-66).
	SourcePath string
	// Series and SeriesIndex describe the book's place in a multi-book series,
	// read from EPUB metadata at ingest (LYCM-36). Series is "" when the book
	// belongs to none; SeriesIndex is 0 when the position is unknown.
	Series      string
	SeriesIndex float64
	AddedAt     time.Time
	// FinishedAt is when the book was marked read, or nil when it is not
	// finished. It is an explicit signal independent of reading progress, since
	// epub.js progress rarely reaches 1.0 (back matter inflates the denominator).
	FinishedAt *time.Time
	// ReviewState is the ingest-QC lifecycle (LYCM-58): ReviewPublished (on the
	// shelf) or ReviewPending (held for review because ingest flagged an issue).
	// ReviewFlags carries the detected issue codes for a pending book.
	ReviewState string
	ReviewFlags []string
}

// Ingest-QC review states (LYCM-58). A flagged new ingest lands ReviewPending and
// is kept off the shelf until approved; everything else is ReviewPublished.
const (
	ReviewPending   = "pending"
	ReviewPublished = "published"
)

// Device is a client that syncs reading positions. Reading positions key off
// the device's string identifier (ReadingPosition.DeviceID), which need not be
// a row in this table.
type Device struct {
	ID       int64
	Name     string
	LastSeen *time.Time
}

// ReadingPosition is one user's bookmark within a book, on one device. The
// triple (BookID, UserID, DeviceID) is unique: a person has at most one position
// per book per device, and two people reading the same book on a shared device
// each keep their own (LYCM-801).
type ReadingPosition struct {
	ID        int64
	BookID    int64
	UserID    int64
	DeviceID  string
	CFI       string
	Progress  float64
	UpdatedAt time.Time
}

// Store is the Postgres-backed repository for Lyceum. It owns a connection
// pool and the data directory under which EPUB and cover blobs are written.
type Store struct {
	pool    *pgxpool.Pool
	dataDir string
}

// New builds a Store over an existing pool. dataDir is where SaveBlobs writes
// EPUB and cover files; it is created on demand.
func New(pool *pgxpool.Pool, dataDir string) *Store {
	return &Store{pool: pool, dataDir: dataDir}
}

// Pool exposes the underlying connection pool for callers that need it (e.g.
// migrations or health checks).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// bookColumns is the canonical SELECT projection for a Book row. Nullable
// text columns are coalesced so they scan cleanly into Go strings.
const bookColumns = `id, title, COALESCE(author, ''), COALESCE(cover_path, ''),
	file_path, file_hash, COALESCE(size_bytes, 0), COALESCE(source_path, ''),
	COALESCE(series, ''), COALESCE(series_index, 0), added_at, finished_at,
	COALESCE(review_state, 'published'), review_flags`

func scanBook(row pgx.Row) (Book, error) {
	var b Book
	var reviewFlags []byte
	err := row.Scan(&b.ID, &b.Title, &b.Author, &b.CoverPath,
		&b.FilePath, &b.FileHash, &b.SizeBytes, &b.SourcePath,
		&b.Series, &b.SeriesIndex, &b.AddedAt, &b.FinishedAt,
		&b.ReviewState, &reviewFlags)
	if err != nil {
		return Book{}, err
	}
	if len(reviewFlags) > 0 {
		if err := json.Unmarshal(reviewFlags, &b.ReviewFlags); err != nil {
			return Book{}, fmt.Errorf("store: decode review flags: %w", err)
		}
	}
	return b, nil
}

// InsertBook inserts b and returns the stored row. It is idempotent on
// FileHash: if a book with the same hash already exists, the existing row is
// returned unchanged and no new row is created. The insert-or-select spans two
// statements and therefore runs inside a single transaction.
func (s *Store) InsertBook(ctx context.Context, b Book) (Book, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Book{}, fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inserted, err := scanBook(tx.QueryRow(ctx,
		`INSERT INTO books (title, author, cover_path, file_path, file_hash, size_bytes, source_path, series, series_index, review_state, review_flags)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, COALESCE(NULLIF($10, ''), 'published'), $11::jsonb)
		 ON CONFLICT (file_hash) DO NOTHING
		 RETURNING `+bookColumns,
		b.Title, nullString(b.Author), nullString(b.CoverPath),
		b.FilePath, b.FileHash, b.SizeBytes, nullString(b.SourcePath),
		nullString(b.Series), nullFloat(b.SeriesIndex),
		b.ReviewState, marshalFlags(b.ReviewFlags)))
	switch {
	case err == nil:
		if err := tx.Commit(ctx); err != nil {
			return Book{}, fmt.Errorf("store: commit: %w", err)
		}
		return inserted, nil
	case errors.Is(err, pgx.ErrNoRows):
		// Hash already present: fetch and return the existing row.
		existing, err := scanBook(tx.QueryRow(ctx,
			`SELECT `+bookColumns+` FROM books WHERE file_hash = $1`, b.FileHash))
		if err != nil {
			return Book{}, fmt.Errorf("store: select existing book: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Book{}, fmt.Errorf("store: commit: %w", err)
		}
		return existing, nil
	default:
		return Book{}, fmt.Errorf("store: insert book: %w", err)
	}
}

// marshalFlags encodes a review-flag slice for the JSONB column, using an empty
// array (never SQL NULL) so the column's NOT NULL default holds.
func marshalFlags(flags []string) string {
	if len(flags) == 0 {
		return "[]"
	}
	b, err := json.Marshal(flags)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ListBooks returns the shelf: published books only, most recently added first.
// Pending-review books (LYCM-58) are held back until approved — see
// ListPendingBooks for the review queue.
func (s *Store) ListBooks(ctx context.Context) ([]Book, error) {
	return s.listBooks(ctx,
		`SELECT `+bookColumns+` FROM books
		 WHERE review_state = 'published'
		 ORDER BY added_at DESC, id DESC`)
}

// ListPendingBooks returns the ingest-QC review queue: books held for review,
// most recently added first.
func (s *Store) ListPendingBooks(ctx context.Context) ([]Book, error) {
	return s.listBooks(ctx,
		`SELECT `+bookColumns+` FROM books
		 WHERE review_state = 'pending'
		 ORDER BY added_at DESC, id DESC`)
}

// listBooks runs a book-projection query and scans the rows.
func (s *Store) listBooks(ctx context.Context, query string) ([]Book, error) {
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("store: list books: %w", err)
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		b, err := scanBook(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan book: %w", err)
		}
		books = append(books, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list books: %w", err)
	}
	return books, nil
}

// GetBook returns the book with the given id, or ErrNotFound.
func (s *Store) GetBook(ctx context.Context, id int64) (Book, error) {
	b, err := scanBook(s.pool.QueryRow(ctx,
		`SELECT `+bookColumns+` FROM books WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	if err != nil {
		return Book{}, fmt.Errorf("store: get book: %w", err)
	}
	return b, nil
}

// GetBookByHash returns the book with the given content hash, or ErrNotFound.
// Callers use it to reject duplicate uploads before writing any blobs.
func (s *Store) GetBookByHash(ctx context.Context, hash string) (Book, error) {
	b, err := scanBook(s.pool.QueryRow(ctx,
		`SELECT `+bookColumns+` FROM books WHERE file_hash = $1`, hash))
	if errors.Is(err, pgx.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	if err != nil {
		return Book{}, fmt.Errorf("store: get book by hash: %w", err)
	}
	return b, nil
}

const positionColumns = `id, book_id, user_id, device_id, cfi, COALESCE(progress, 0), updated_at`

func scanPosition(row pgx.Row) (ReadingPosition, error) {
	var p ReadingPosition
	err := row.Scan(&p.ID, &p.BookID, &p.UserID, &p.DeviceID, &p.CFI, &p.Progress, &p.UpdatedAt)
	return p, err
}

// UpsertPosition inserts a reading position, or updates the existing one for
// the same (BookID, UserID, DeviceID) triple. updated_at is refreshed to now()
// on every call. The stored row is returned.
func (s *Store) UpsertPosition(ctx context.Context, p ReadingPosition) (ReadingPosition, error) {
	saved, err := scanPosition(s.pool.QueryRow(ctx,
		`INSERT INTO reading_positions (book_id, user_id, device_id, cfi, progress, updated_at)
		 VALUES ($1, $2, $3, $4, $5, now())
		 ON CONFLICT (book_id, user_id, device_id) DO UPDATE
		   SET cfi = EXCLUDED.cfi,
		       progress = EXCLUDED.progress,
		       updated_at = now()
		 RETURNING `+positionColumns,
		p.BookID, p.UserID, p.DeviceID, p.CFI, p.Progress))
	if err != nil {
		return ReadingPosition{}, fmt.Errorf("store: upsert position: %w", err)
	}
	return saved, nil
}

// UpsertPositionLWW inserts a reading position, or reconciles it against the
// existing row for the same (BookID, UserID, DeviceID) triple using
// last-write-wins: the incoming row only replaces the stored one when its
// UpdatedAt is greater than or equal to the stored updated_at. The unchanged
// (older) write is a no-op. Either way the current winning row is returned.
// Unlike UpsertPosition, this honours the client-supplied UpdatedAt rather than
// stamping now(), so the /sync endpoint can resolve cross-device conflicts by
// the clients' own clocks.
func (s *Store) UpsertPositionLWW(ctx context.Context, p ReadingPosition) (ReadingPosition, error) {
	saved, err := scanPosition(s.pool.QueryRow(ctx,
		`INSERT INTO reading_positions (book_id, user_id, device_id, cfi, progress, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (book_id, user_id, device_id) DO UPDATE
		   SET cfi = EXCLUDED.cfi,
		       progress = EXCLUDED.progress,
		       updated_at = EXCLUDED.updated_at
		   WHERE EXCLUDED.updated_at >= reading_positions.updated_at
		 RETURNING `+positionColumns,
		p.BookID, p.UserID, p.DeviceID, p.CFI, p.Progress, p.UpdatedAt))
	switch {
	case err == nil:
		return saved, nil
	case errors.Is(err, pgx.ErrNoRows):
		// The WHERE guard rejected a stale write (incoming updated_at older than
		// the stored row): the existing row wins. Return it unchanged.
		return s.GetPosition(ctx, p.BookID, p.UserID, p.DeviceID)
	default:
		return ReadingPosition{}, fmt.Errorf("store: upsert position (lww): %w", err)
	}
}

// GetPosition returns one user's reading position for a book on a specific
// device, or ErrNotFound.
func (s *Store) GetPosition(ctx context.Context, bookID, userID int64, deviceID string) (ReadingPosition, error) {
	p, err := scanPosition(s.pool.QueryRow(ctx,
		`SELECT `+positionColumns+`
		 FROM reading_positions
		 WHERE book_id = $1 AND user_id = $2 AND device_id = $3`,
		bookID, userID, deviceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReadingPosition{}, ErrNotFound
	}
	if err != nil {
		return ReadingPosition{}, fmt.Errorf("store: get position: %w", err)
	}
	return p, nil
}

// GetFurthestPosition returns the reading position with the greatest progress
// for a book across all of one user's devices (ties broken by recency), or
// ErrNotFound when that user has never opened the book. This is the resume and
// progress-display anchor: ordering by progress rather than write time means a
// device that saved an earlier spot more recently — e.g. a still-open reader
// that flushed a pre-pagination progress=0 on navigation — can't clobber the
// furthest read on another device (LYCM sync fix).
//
// Since LYCM-801 the sweep is scoped to one user: housemates share the shelf but
// not each other's bookmarks, so one person finishing a book does not show the
// next person as finished (LYCM-802 will surface who read what, deliberately).
func (s *Store) GetFurthestPosition(ctx context.Context, bookID, userID int64) (ReadingPosition, error) {
	p, err := scanPosition(s.pool.QueryRow(ctx,
		`SELECT `+positionColumns+`
		 FROM reading_positions WHERE book_id = $1 AND user_id = $2
		 ORDER BY COALESCE(progress, 0) DESC, updated_at DESC, id DESC LIMIT 1`,
		bookID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReadingPosition{}, ErrNotFound
	}
	if err != nil {
		return ReadingPosition{}, fmt.Errorf("store: get furthest position: %w", err)
	}
	return p, nil
}

// SaveBlobs writes the EPUB bytes and optional cover bytes under the store's
// data directory, namespaced by fileHash so identical content shares a path.
// It returns the EPUB path and the cover path (empty when cover is nil). The
// returned paths are what callers persist in the books row.
func (s *Store) SaveBlobs(fileHash string, epub, cover []byte) (filePath, coverPath string, err error) {
	if fileHash == "" {
		return "", "", errors.New("store: SaveBlobs requires a non-empty fileHash")
	}
	dir := filepath.Join(s.dataDir, fileHash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("store: mkdir blob dir: %w", err)
	}

	filePath = filepath.Join(dir, "book.epub")
	if err := os.WriteFile(filePath, epub, 0o644); err != nil {
		return "", "", fmt.Errorf("store: write epub: %w", err)
	}

	if cover != nil {
		coverPath = filepath.Join(dir, "cover.bin")
		if err := os.WriteFile(coverPath, cover, 0o644); err != nil {
			return "", "", fmt.Errorf("store: write cover: %w", err)
		}
	}
	return filePath, coverPath, nil
}

// SaveCoverAt writes (or overwrites) cover bytes at an exact path, creating the
// parent directory if needed. Unlike SaveBlobs it leaves the EPUB blob untouched
// and does not derive the path from a content hash: it exists for the cover
// backfill, which must write to a book's actual served cover location (its
// stored cover_path or the directory its EPUB lives in) rather than a
// hash-derived path, since a book's recorded file_hash may not match its blob
// directory.
func (s *Store) SaveCoverAt(coverPath string, cover []byte) error {
	if coverPath == "" {
		return errors.New("store: SaveCoverAt requires a non-empty path")
	}
	if len(cover) == 0 {
		return errors.New("store: SaveCoverAt requires non-empty cover bytes")
	}
	if err := os.MkdirAll(filepath.Dir(coverPath), 0o755); err != nil {
		return fmt.Errorf("store: mkdir cover dir: %w", err)
	}
	if err := os.WriteFile(coverPath, cover, 0o644); err != nil {
		return fmt.Errorf("store: write cover: %w", err)
	}
	return nil
}

// SetCoverPath updates a book's cover_path column. It is used by the backfill to
// record a newly-fetched cover for a book that previously had none. Returns
// ErrNotFound if no row has the given id.
func (s *Store) SetCoverPath(ctx context.Context, bookID int64, coverPath string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE books SET cover_path = $2 WHERE id = $1`, bookID, nullString(coverPath))
	if err != nil {
		return fmt.Errorf("store: set cover path: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetBookBySourcePath returns the book folder-ingested from sourcePath, or
// ErrNotFound. Only folder-ingested books carry a source_path (uploads have
// none), so an empty path never matches. Matching is case-insensitive: the
// acquisition pipeline re-cases folder names between imports (LYCM-68), and a
// re-cased path must still resolve to the same book instead of duplicating it.
// ORDER BY id keeps the result deterministic (the original row wins) for legacy
// rows that already duplicated across casings.
func (s *Store) GetBookBySourcePath(ctx context.Context, sourcePath string) (Book, error) {
	if sourcePath == "" {
		return Book{}, ErrNotFound
	}
	b, err := scanBook(s.pool.QueryRow(ctx,
		`SELECT `+bookColumns+` FROM books WHERE lower(source_path) = lower($1)
		  ORDER BY id LIMIT 1`, sourcePath))
	if errors.Is(err, pgx.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	if err != nil {
		return Book{}, fmt.Errorf("store: get book by source path: %w", err)
	}
	return b, nil
}

// SetBookSourcePath records the watched-tree path a book was ingested from. It
// lets books ingested before source_path existed (or via a newly-watched path)
// be adopted, so a later re-stamp updates them in place. Returns ErrNotFound if
// id is gone; a unique-violation surfaces if another book already claims the
// path (callers treat that as best-effort).
func (s *Store) SetBookSourcePath(ctx context.Context, id int64, sourcePath string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE books SET source_path = $2 WHERE id = $1`, id, nullString(sourcePath))
	if err != nil {
		return fmt.Errorf("store: set source path: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateBookContent replaces a book's content-derived fields in place, keeping
// its id (and thus reading positions), source_path, and added_at. The folder
// watcher uses it when a watched file is re-stamped (new content hash) so the
// book updates rather than duplicating (LYCM-66). Returns ErrNotFound if id is
// gone.
func (s *Store) UpdateBookContent(ctx context.Context, id int64, b Book) (Book, error) {
	updated, err := scanBook(s.pool.QueryRow(ctx,
		`UPDATE books
		    SET title = $2, author = $3, cover_path = $4,
		        file_path = $5, file_hash = $6, size_bytes = $7,
		        series = $8, series_index = $9
		  WHERE id = $1
		  RETURNING `+bookColumns,
		id, b.Title, nullString(b.Author), nullString(b.CoverPath),
		b.FilePath, b.FileHash, b.SizeBytes,
		nullString(b.Series), nullFloat(b.SeriesIndex)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	if err != nil {
		return Book{}, fmt.Errorf("store: update book content: %w", err)
	}
	return updated, nil
}

// UpdateBookSeries sets (or clears) a book's series name and position. It backs
// the `set-series` CLI tool for libraries whose EPUBs carry no series metadata
// (LYCM-36). An empty series clears both columns; a zero index stores NULL so an
// unknown position never reads back as a 0th volume. Returns ErrNotFound if the
// id is gone.
func (s *Store) UpdateBookSeries(ctx context.Context, id int64, series string, index float64) (Book, error) {
	b, err := scanBook(s.pool.QueryRow(ctx,
		`UPDATE books SET series = $2, series_index = $3 WHERE id = $1
		 RETURNING `+bookColumns,
		id, nullString(strings.TrimSpace(series)), nullFloat(index)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	if err != nil {
		return Book{}, fmt.Errorf("store: update book series: %w", err)
	}
	return b, nil
}

// ApproveBook publishes a pending-review book to the shelf (LYCM-58), clearing
// its review flags. Idempotent on an already-published book. Returns the updated
// row, or ErrNotFound if the id is gone.
func (s *Store) ApproveBook(ctx context.Context, id int64) (Book, error) {
	b, err := scanBook(s.pool.QueryRow(ctx,
		`UPDATE books SET review_state = 'published', review_flags = '[]'::jsonb
		 WHERE id = $1 RETURNING `+bookColumns, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	if err != nil {
		return Book{}, fmt.Errorf("store: approve book: %w", err)
	}
	return b, nil
}

// UpdateBookMeta edits a book's title and author (LYCM-58 QC override for
// mangled MOBI metadata). Title is required (the column is NOT NULL); an empty
// author clears it. It does not change review state — approving is separate.
// Returns the updated row, or ErrNotFound if the id is gone.
func (s *Store) UpdateBookMeta(ctx context.Context, id int64, title, author string) (Book, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Book{}, fmt.Errorf("store: update book meta: title is required")
	}
	b, err := scanBook(s.pool.QueryRow(ctx,
		`UPDATE books SET title = $2, author = $3 WHERE id = $1
		 RETURNING `+bookColumns,
		id, title, nullString(strings.TrimSpace(author))))
	if errors.Is(err, pgx.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	if err != nil {
		return Book{}, fmt.Errorf("store: update book meta: %w", err)
	}
	return b, nil
}

// SetBookFinished marks a book read (finished_at = now) or clears it (NULL),
// returning the updated row. Idempotent re-marks refresh the timestamp. Returns
// ErrNotFound if the id is gone.
func (s *Store) SetBookFinished(ctx context.Context, id int64, finished bool) (Book, error) {
	var at any
	if finished {
		at = time.Now().UTC()
	}
	b, err := scanBook(s.pool.QueryRow(ctx,
		`UPDATE books SET finished_at = $2 WHERE id = $1 RETURNING `+bookColumns, id, at))
	if errors.Is(err, pgx.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	if err != nil {
		return Book{}, fmt.Errorf("store: set book finished: %w", err)
	}
	return b, nil
}

// DeleteBook removes a book row and returns the deleted row so the caller can
// clean up its blobs. Dependents are handled by the schema FKs:
// reading_positions/deliveries cascade, inventory.book_id is set NULL. Returns
// ErrNotFound if id is gone.
func (s *Store) DeleteBook(ctx context.Context, id int64) (Book, error) {
	b, err := scanBook(s.pool.QueryRow(ctx,
		`DELETE FROM books WHERE id = $1 RETURNING `+bookColumns, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	if err != nil {
		return Book{}, fmt.Errorf("store: delete book: %w", err)
	}
	return b, nil
}

// RemoveBlobs deletes the on-disk blob directory backing a book — the
// hash-named dir holding its book.epub and cover. It is a no-op on an empty
// path, and defensively refuses any dir whose name is not a 64-char hex hash,
// so a malformed path can never remove an unrelated directory (e.g. the data
// dir itself).
func (s *Store) RemoveBlobs(filePath string) error {
	if filePath == "" {
		return nil
	}
	dir := filepath.Dir(filePath) // .../<hash>
	if !isBlobHashDir(filepath.Base(dir)) {
		return fmt.Errorf("store: refusing to remove non-blob dir %q", dir)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("store: remove blobs %q: %w", dir, err)
	}
	return nil
}

// isBlobHashDir reports whether name is a 64-char lowercase-hex SHA-256, the
// shape SaveBlobs gives every blob directory.
func isBlobHashDir(name string) bool {
	if len(name) != 64 {
		return false
	}
	for _, c := range name {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// nullString maps "" to a SQL NULL so empty optional text columns stay NULL
// rather than storing empty strings.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullFloat maps 0 to a SQL NULL so an unknown series index stays NULL rather
// than storing a meaningless 0th position.
func nullFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}
