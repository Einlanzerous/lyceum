package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/magos/lyceum/internal/dedup"
	"time"

	"github.com/jackc/pgx/v5"
)

// Acquisition states for an inventory entry. A title starts owned (you have the
// physical book / legitimately own it), may be marked wanted (a digital copy
// has been requested) and acquiring (a grab is in flight), and becomes ingested
// once an EPUB lands in the library and is linked via BookID.
const (
	StateOwned     = "owned"
	StateWanted    = "wanted"
	StateAcquiring = "acquiring"
	StateIngested  = "ingested"
)

// ValidState reports whether s is one of the known acquisition states. Callers
// validate before SetInventoryState so a bad value yields a 400, not a DB
// CHECK-constraint error.
func ValidState(s string) bool {
	switch s {
	case StateOwned, StateWanted, StateAcquiring, StateIngested:
		return true
	default:
		return false
	}
}

// Inventory is an owned title, keyed by its canonical ISBN-13. It exists
// independently of any EPUB: a scanned physical book has an inventory row with
// no BookID until a digital copy is acquired and ingested. See migration 0003.
type Inventory struct {
	ID     int64
	ISBN   string // the primary/first-seen ISBN-13; see inventory_isbns for the full set
	Title  string
	Author string
	WorkID string // OpenLibrary work key grouping print/ebook editions (LYCM-35); "" if unknown
	State  string
	BookID *int64 // the ingested EPUB, once one is linked
	// Series and SeriesIndex carry the series intent assigned at ingest confirm
	// (LYCM-82). Ingest applies them to the linked book when the EPUB itself
	// declares no series; "" / 0 when no intent was recorded.
	Series      string
	SeriesIndex float64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const inventoryColumns = `id, isbn, COALESCE(title, ''), COALESCE(author, ''),
	COALESCE(work_id, ''), acquisition_state, book_id,
	COALESCE(series, ''), COALESCE(series_index, 0), created_at, updated_at`

// inventoryColumnsQ is inventoryColumns table-qualified, for queries that JOIN
// inventory_isbns (whose created_at would otherwise be ambiguous).
const inventoryColumnsQ = `inventory.id, inventory.isbn, COALESCE(inventory.title, ''),
	COALESCE(inventory.author, ''), COALESCE(inventory.work_id, ''),
	inventory.acquisition_state, inventory.book_id,
	COALESCE(inventory.series, ''), COALESCE(inventory.series_index, 0),
	inventory.created_at, inventory.updated_at`

func scanInventory(row pgx.Row) (Inventory, error) {
	var inv Inventory
	err := row.Scan(&inv.ID, &inv.ISBN, &inv.Title, &inv.Author, &inv.WorkID,
		&inv.State, &inv.BookID, &inv.Series, &inv.SeriesIndex,
		&inv.CreatedAt, &inv.UpdatedAt)
	return inv, err
}

// UpsertInventory records ownership of a title by ISBN (create-or-find). On a
// new ISBN it inserts an `owned` row; on an existing ISBN it backfills a missing
// title/author but never overwrites a non-empty one and never changes the
// acquisition state (so re-scanning an already-ingested title stays ingested).
// The stored row is returned.
func (s *Store) UpsertInventory(ctx context.Context, inv Inventory) (Inventory, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Inventory{}, fmt.Errorf("store: upsert inventory: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	saved, err := scanInventory(tx.QueryRow(ctx,
		`INSERT INTO inventory (isbn, title, author, work_id, acquisition_state)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (isbn) DO UPDATE
		   SET title = COALESCE(NULLIF(inventory.title, ''), EXCLUDED.title),
		       author = COALESCE(NULLIF(inventory.author, ''), EXCLUDED.author),
		       work_id = COALESCE(inventory.work_id, EXCLUDED.work_id),
		       updated_at = now()
		 RETURNING `+inventoryColumns,
		inv.ISBN, nullString(inv.Title), nullString(inv.Author), nullString(inv.WorkID), StateOwned))
	if err != nil {
		return Inventory{}, fmt.Errorf("store: upsert inventory: %w", err)
	}
	if err := registerISBN(ctx, tx, inv.ISBN, saved.ID); err != nil {
		return Inventory{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Inventory{}, fmt.Errorf("store: upsert inventory: commit: %w", err)
	}
	return saved, nil
}

// SetInventoryState transitions an existing entry to state, returning the
// updated row or ErrNotFound. Callers should validate state with ValidState
// first. It does not create a row — use UpsertInventory for that.
func (s *Store) SetInventoryState(ctx context.Context, isbn, state string) (Inventory, error) {
	inv, err := scanInventory(s.pool.QueryRow(ctx,
		`UPDATE inventory SET acquisition_state = $2, updated_at = now()
		 WHERE isbn = $1
		 RETURNING `+inventoryColumns, isbn, state))
	if errors.Is(err, pgx.ErrNoRows) {
		return Inventory{}, ErrNotFound
	}
	if err != nil {
		return Inventory{}, fmt.Errorf("store: set inventory state: %w", err)
	}
	return inv, nil
}

// SetInventorySeries records the series intent for the entry whose primary ISBN
// is isbn (LYCM-82). Confirm is an explicit user action, so a re-confirm
// overwrites earlier intent; an empty series clears it (index stores NULL for
// unknown positions, matching books.series_index semantics). Returns the
// updated row, or ErrNotFound when no entry has the ISBN.
func (s *Store) SetInventorySeries(ctx context.Context, isbn, series string, index float64) (Inventory, error) {
	inv, err := scanInventory(s.pool.QueryRow(ctx,
		`UPDATE inventory SET series = $2, series_index = $3, updated_at = now()
		 WHERE isbn = $1
		 RETURNING `+inventoryColumns, isbn, nullString(series), nullFloat(index)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Inventory{}, ErrNotFound
	}
	if err != nil {
		return Inventory{}, fmt.Errorf("store: set inventory series: %w", err)
	}
	return inv, nil
}

// ErrInventoryFulfilled is returned by FulfilInventory when the entry already
// has a different book linked: one row per work, so a second ebook for it is a
// duplicate to resolve, not a link to overwrite.
var ErrInventoryFulfilled = errors.New("store: inventory entry already fulfilled by another book")

// LinkBookToInventory records that bookID is the ingested EPUB for code: it sets
// book_id and flips the state to ingested. It reconciles print↔ebook editions
// (LYCM-35): the book joins the entry that already knows this ISBN, else a
// sibling edition of the same work (workID), else — since the resolver can be
// blind to an ebook ISBN altogether (LYCM-128: Open Library has no record of
// the Pottermore HP #1, so it produced no work key) — an entry still waiting
// for a book whose title and author match the EPUB's, else a fresh row is
// created (a direct upload or a Bindery grab that no scan preceded). code is
// always registered as a known ISBN of the resulting row, so a later lookup by
// the ebook number finds the same entry a print scan created. An existing
// non-empty title/author is preserved. The stored row is returned.
func (s *Store) LinkBookToInventory(ctx context.Context, code, workID string, bookID int64, title, author string) (Inventory, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Inventory{}, fmt.Errorf("store: link book to inventory: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	targetID, found, err := findInventoryTarget(ctx, tx, code, workID, title, author)
	if err != nil {
		return Inventory{}, err
	}

	var inv Inventory
	if found {
		inv, err = scanInventory(tx.QueryRow(ctx,
			`UPDATE inventory SET
			   book_id = $2,
			   acquisition_state = $3,
			   title = COALESCE(NULLIF(title, ''), $4),
			   author = COALESCE(NULLIF(author, ''), $5),
			   work_id = COALESCE(work_id, $6),
			   updated_at = now()
			 WHERE id = $1
			 RETURNING `+inventoryColumns,
			targetID, bookID, StateIngested, nullString(title), nullString(author), nullString(workID)))
	} else {
		inv, err = scanInventory(tx.QueryRow(ctx,
			`INSERT INTO inventory (isbn, title, author, work_id, acquisition_state, book_id)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 RETURNING `+inventoryColumns,
			code, nullString(title), nullString(author), nullString(workID), StateIngested, bookID))
	}
	if err != nil {
		return Inventory{}, fmt.Errorf("store: link book to inventory: %w", err)
	}
	if err := registerISBN(ctx, tx, code, inv.ID); err != nil {
		return Inventory{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Inventory{}, fmt.Errorf("store: link book to inventory: commit: %w", err)
	}
	return inv, nil
}

// findInventoryTarget picks the inventory row an ingest of code (work workID,
// titled title by author) should join: first an entry that already maps this
// exact ISBN, then a sibling edition of the same work, then an entry with no
// book yet whose title and author match. found is false when none exists (a
// fresh title).
func findInventoryTarget(ctx context.Context, tx pgx.Tx, code, workID, title, author string) (int64, bool, error) {
	var id int64
	switch err := tx.QueryRow(ctx,
		`SELECT inventory_id FROM inventory_isbns WHERE isbn13 = $1`, code).Scan(&id); {
	case err == nil:
		return id, true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return 0, false, fmt.Errorf("store: find inventory by isbn: %w", err)
	}

	if workID != "" {
		switch err := tx.QueryRow(ctx,
			`SELECT id FROM inventory WHERE work_id = $1 ORDER BY id ASC LIMIT 1`, workID).Scan(&id); {
		case err == nil:
			return id, true, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return 0, false, fmt.Errorf("store: find inventory by work: %w", err)
		}
	}

	return findUnfulfilledByTitleAuthor(ctx, tx, title, author)
}

// findUnfulfilledByTitleAuthor is the last resort before creating a row: an
// entry still waiting for its book (book_id NULL — owned or wanted) whose
// title and author both match the EPUB's under dedup's normalisation
// (case, accents, punctuation, a leading article, "Surname, Given"). Both must
// match — a title alone joins the wrong author's same-named book, and an
// author alone joins the wrong volume — and an entry already holding a book
// is never a target, so a second copy of a work stays a duplicate for review
// rather than silently taking over the row. Entries with no title or author
// recorded cannot match. Ties go to the oldest entry.
func findUnfulfilledByTitleAuthor(ctx context.Context, tx pgx.Tx, title, author string) (int64, bool, error) {
	wantTitle, wantAuthor := dedup.NormalizeTitle(title), dedup.NormalizeAuthor(author)
	if wantTitle == "" || wantAuthor == "" {
		return 0, false, nil
	}
	rows, err := tx.Query(ctx,
		`SELECT id, COALESCE(title, ''), COALESCE(author, '') FROM inventory
		 WHERE book_id IS NULL ORDER BY id ASC`)
	if err != nil {
		return 0, false, fmt.Errorf("store: find inventory by title/author: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var t, a string
		if err := rows.Scan(&id, &t, &a); err != nil {
			return 0, false, fmt.Errorf("store: scan inventory by title/author: %w", err)
		}
		if dedup.NormalizeTitle(t) == wantTitle && dedup.NormalizeAuthor(a) == wantAuthor {
			return id, true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return 0, false, fmt.Errorf("store: find inventory by title/author: %w", err)
	}
	return 0, false, nil
}

// FulfilInventory links bookID to the inventory entry with id by hand
// (LYCM-128): the reviewer's "this held book is that wanted title" for an
// EPUB that carried no ISBN to join on. It sets book_id and flips the state to
// ingested; code, when the book has one, is registered as a known ISBN of the
// entry. An entry already holding a different book returns
// ErrInventoryFulfilled; re-linking the same book is a no-op. Returns
// ErrNotFound for an unknown id. The stored row is returned.
func (s *Store) FulfilInventory(ctx context.Context, id, bookID int64, code string) (Inventory, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Inventory{}, fmt.Errorf("store: fulfil inventory: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current *int64
	switch err := tx.QueryRow(ctx, `SELECT book_id FROM inventory WHERE id = $1 FOR UPDATE`, id).Scan(&current); {
	case errors.Is(err, pgx.ErrNoRows):
		return Inventory{}, ErrNotFound
	case err != nil:
		return Inventory{}, fmt.Errorf("store: fulfil inventory: %w", err)
	}
	if current != nil && *current != bookID {
		return Inventory{}, ErrInventoryFulfilled
	}
	inv, err := scanInventory(tx.QueryRow(ctx,
		`UPDATE inventory SET book_id = $2, acquisition_state = $3, updated_at = now()
		 WHERE id = $1
		 RETURNING `+inventoryColumns, id, bookID, StateIngested))
	if err != nil {
		return Inventory{}, fmt.Errorf("store: fulfil inventory: %w", err)
	}
	if err := registerISBN(ctx, tx, code, inv.ID); err != nil {
		return Inventory{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Inventory{}, fmt.Errorf("store: fulfil inventory: commit: %w", err)
	}
	return inv, nil
}

// registerISBN records code as a known ISBN of inventoryID. It is idempotent and
// first-writer-wins: an ISBN already mapped to some entry keeps that mapping.
func registerISBN(ctx context.Context, tx pgx.Tx, code string, inventoryID int64) error {
	if code == "" {
		return nil
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO inventory_isbns (isbn13, inventory_id) VALUES ($1, $2)
		 ON CONFLICT (isbn13) DO NOTHING`, code, inventoryID); err != nil {
		return fmt.Errorf("store: register inventory isbn: %w", err)
	}
	return nil
}

// GetInventoryByISBN returns the entry whose primary ISBN is isbn, or
// ErrNotFound. Use GetInventoryByAnyISBN to also match secondary (e.g. ebook)
// ISBNs recorded during reconciliation.
func (s *Store) GetInventoryByISBN(ctx context.Context, isbn string) (Inventory, error) {
	inv, err := scanInventory(s.pool.QueryRow(ctx,
		`SELECT `+inventoryColumns+` FROM inventory WHERE isbn = $1`, isbn))
	if errors.Is(err, pgx.ErrNoRows) {
		return Inventory{}, ErrNotFound
	}
	if err != nil {
		return Inventory{}, fmt.Errorf("store: get inventory by isbn: %w", err)
	}
	return inv, nil
}

// GetInventoryByAnyISBN returns the entry that knows code as any of its ISBNs
// (primary or a reconciled print/ebook alternate), or ErrNotFound.
func (s *Store) GetInventoryByAnyISBN(ctx context.Context, code string) (Inventory, error) {
	inv, err := scanInventory(s.pool.QueryRow(ctx,
		`SELECT `+inventoryColumnsQ+` FROM inventory
		 JOIN inventory_isbns ii ON ii.inventory_id = inventory.id
		 WHERE ii.isbn13 = $1`, code))
	if errors.Is(err, pgx.ErrNoRows) {
		return Inventory{}, ErrNotFound
	}
	if err != nil {
		return Inventory{}, fmt.Errorf("store: get inventory by any isbn: %w", err)
	}
	return inv, nil
}

// ListInventory returns all inventory entries, most recently updated first.
func (s *Store) ListInventory(ctx context.Context) ([]Inventory, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+inventoryColumns+` FROM inventory ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("store: list inventory: %w", err)
	}
	defer rows.Close()

	var out []Inventory
	for rows.Next() {
		inv, err := scanInventory(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan inventory: %w", err)
		}
		out = append(out, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list inventory: %w", err)
	}
	return out, nil
}
