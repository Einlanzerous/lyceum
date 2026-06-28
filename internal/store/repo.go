package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	AddedAt   time.Time
}

// Device is a client that syncs reading positions. Reading positions key off
// the device's string identifier (ReadingPosition.DeviceID), which need not be
// a row in this table.
type Device struct {
	ID       int64
	Name     string
	LastSeen *time.Time
}

// ReadingPosition is a per-device bookmark within a book. The pair
// (BookID, DeviceID) is unique: a device has at most one position per book.
type ReadingPosition struct {
	ID        int64
	BookID    int64
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
	file_path, file_hash, COALESCE(size_bytes, 0), added_at`

func scanBook(row pgx.Row) (Book, error) {
	var b Book
	err := row.Scan(&b.ID, &b.Title, &b.Author, &b.CoverPath,
		&b.FilePath, &b.FileHash, &b.SizeBytes, &b.AddedAt)
	return b, err
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
		`INSERT INTO books (title, author, cover_path, file_path, file_hash, size_bytes)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (file_hash) DO NOTHING
		 RETURNING `+bookColumns,
		b.Title, nullString(b.Author), nullString(b.CoverPath),
		b.FilePath, b.FileHash, b.SizeBytes))
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

// ListBooks returns all books, most recently added first.
func (s *Store) ListBooks(ctx context.Context) ([]Book, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+bookColumns+` FROM books ORDER BY added_at DESC, id DESC`)
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

const positionColumns = `id, book_id, device_id, cfi, COALESCE(progress, 0), updated_at`

func scanPosition(row pgx.Row) (ReadingPosition, error) {
	var p ReadingPosition
	err := row.Scan(&p.ID, &p.BookID, &p.DeviceID, &p.CFI, &p.Progress, &p.UpdatedAt)
	return p, err
}

// UpsertPosition inserts a reading position, or updates the existing one for
// the same (BookID, DeviceID) pair. updated_at is refreshed to now() on every
// call. The stored row is returned.
func (s *Store) UpsertPosition(ctx context.Context, p ReadingPosition) (ReadingPosition, error) {
	saved, err := scanPosition(s.pool.QueryRow(ctx,
		`INSERT INTO reading_positions (book_id, device_id, cfi, progress, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (book_id, device_id) DO UPDATE
		   SET cfi = EXCLUDED.cfi,
		       progress = EXCLUDED.progress,
		       updated_at = now()
		 RETURNING `+positionColumns,
		p.BookID, p.DeviceID, p.CFI, p.Progress))
	if err != nil {
		return ReadingPosition{}, fmt.Errorf("store: upsert position: %w", err)
	}
	return saved, nil
}

// UpsertPositionLWW inserts a reading position, or reconciles it against the
// existing row for the same (BookID, DeviceID) pair using last-write-wins:
// the incoming row only replaces the stored one when its UpdatedAt is greater
// than or equal to the stored updated_at. The unchanged (older) write is a
// no-op. Either way the current winning row is returned. Unlike UpsertPosition,
// this honours the client-supplied UpdatedAt rather than stamping now(), so the
// /sync endpoint can resolve cross-device conflicts by the clients' own clocks.
func (s *Store) UpsertPositionLWW(ctx context.Context, p ReadingPosition) (ReadingPosition, error) {
	saved, err := scanPosition(s.pool.QueryRow(ctx,
		`INSERT INTO reading_positions (book_id, device_id, cfi, progress, updated_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (book_id, device_id) DO UPDATE
		   SET cfi = EXCLUDED.cfi,
		       progress = EXCLUDED.progress,
		       updated_at = EXCLUDED.updated_at
		   WHERE EXCLUDED.updated_at >= reading_positions.updated_at
		 RETURNING `+positionColumns,
		p.BookID, p.DeviceID, p.CFI, p.Progress, p.UpdatedAt))
	switch {
	case err == nil:
		return saved, nil
	case errors.Is(err, pgx.ErrNoRows):
		// The WHERE guard rejected a stale write (incoming updated_at older than
		// the stored row): the existing row wins. Return it unchanged.
		return s.GetPosition(ctx, p.BookID, p.DeviceID)
	default:
		return ReadingPosition{}, fmt.Errorf("store: upsert position (lww): %w", err)
	}
}

// GetPosition returns the reading position for a specific book and device, or
// ErrNotFound.
func (s *Store) GetPosition(ctx context.Context, bookID int64, deviceID string) (ReadingPosition, error) {
	p, err := scanPosition(s.pool.QueryRow(ctx,
		`SELECT `+positionColumns+`
		 FROM reading_positions WHERE book_id = $1 AND device_id = $2`,
		bookID, deviceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReadingPosition{}, ErrNotFound
	}
	if err != nil {
		return ReadingPosition{}, fmt.Errorf("store: get position: %w", err)
	}
	return p, nil
}

// GetLatestPosition returns the most recently updated reading position for a
// book across all devices, or ErrNotFound when the book has no positions.
func (s *Store) GetLatestPosition(ctx context.Context, bookID int64) (ReadingPosition, error) {
	p, err := scanPosition(s.pool.QueryRow(ctx,
		`SELECT `+positionColumns+`
		 FROM reading_positions WHERE book_id = $1
		 ORDER BY updated_at DESC, id DESC LIMIT 1`, bookID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReadingPosition{}, ErrNotFound
	}
	if err != nil {
		return ReadingPosition{}, fmt.Errorf("store: get latest position: %w", err)
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

// nullString maps "" to a SQL NULL so empty optional text columns stay NULL
// rather than storing empty strings.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
