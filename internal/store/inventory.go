package store

import (
	"context"
	"errors"
	"fmt"
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
	ID        int64
	ISBN      string
	Title     string
	Author    string
	State     string
	BookID    *int64 // the ingested EPUB, once one is linked
	CreatedAt time.Time
	UpdatedAt time.Time
}

const inventoryColumns = `id, isbn, COALESCE(title, ''), COALESCE(author, ''),
	acquisition_state, book_id, created_at, updated_at`

func scanInventory(row pgx.Row) (Inventory, error) {
	var inv Inventory
	err := row.Scan(&inv.ID, &inv.ISBN, &inv.Title, &inv.Author,
		&inv.State, &inv.BookID, &inv.CreatedAt, &inv.UpdatedAt)
	return inv, err
}

// UpsertInventory records ownership of a title by ISBN (create-or-find). On a
// new ISBN it inserts an `owned` row; on an existing ISBN it backfills a missing
// title/author but never overwrites a non-empty one and never changes the
// acquisition state (so re-scanning an already-ingested title stays ingested).
// The stored row is returned.
func (s *Store) UpsertInventory(ctx context.Context, inv Inventory) (Inventory, error) {
	saved, err := scanInventory(s.pool.QueryRow(ctx,
		`INSERT INTO inventory (isbn, title, author, acquisition_state)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (isbn) DO UPDATE
		   SET title = COALESCE(NULLIF(inventory.title, ''), EXCLUDED.title),
		       author = COALESCE(NULLIF(inventory.author, ''), EXCLUDED.author),
		       updated_at = now()
		 RETURNING `+inventoryColumns,
		inv.ISBN, nullString(inv.Title), nullString(inv.Author), StateOwned))
	if err != nil {
		return Inventory{}, fmt.Errorf("store: upsert inventory: %w", err)
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

// LinkBookToInventory records that bookID is the ingested EPUB for isbn: it sets
// book_id and flips the state to ingested, creating the inventory row if the
// ISBN was not previously known (a direct upload or a Bindery grab that no scan
// preceded). An existing non-empty title/author is preserved. The stored row is
// returned.
func (s *Store) LinkBookToInventory(ctx context.Context, isbn string, bookID int64, title, author string) (Inventory, error) {
	inv, err := scanInventory(s.pool.QueryRow(ctx,
		`INSERT INTO inventory (isbn, title, author, acquisition_state, book_id)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (isbn) DO UPDATE
		   SET book_id = EXCLUDED.book_id,
		       acquisition_state = $4,
		       title = COALESCE(NULLIF(inventory.title, ''), EXCLUDED.title),
		       author = COALESCE(NULLIF(inventory.author, ''), EXCLUDED.author),
		       updated_at = now()
		 RETURNING `+inventoryColumns,
		isbn, nullString(title), nullString(author), StateIngested, bookID))
	if err != nil {
		return Inventory{}, fmt.Errorf("store: link book to inventory: %w", err)
	}
	return inv, nil
}

// GetInventoryByISBN returns the entry for isbn, or ErrNotFound.
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
