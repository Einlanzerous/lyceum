package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Delivery status values. A delivery is created queued, then the async worker
// moves it to sent or failed.
const (
	DeliveryQueued = "queued"
	DeliverySent   = "sent"
	DeliveryFailed = "failed"
)

// Delivery is a single "Send to Kindle" attempt for a book. Error is populated
// only when Status is DeliveryFailed.
type Delivery struct {
	ID        int64
	BookID    int64
	ToAddr    string
	Status    string
	Error     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

const deliveryColumns = `id, book_id, to_addr, status, COALESCE(error, ''), created_at, updated_at`

func scanDelivery(row pgx.Row) (Delivery, error) {
	var d Delivery
	err := row.Scan(&d.ID, &d.BookID, &d.ToAddr, &d.Status, &d.Error, &d.CreatedAt, &d.UpdatedAt)
	return d, err
}

// CreateDelivery records a new queued delivery for a book and returns it.
func (s *Store) CreateDelivery(ctx context.Context, bookID int64, toAddr string) (Delivery, error) {
	d, err := scanDelivery(s.pool.QueryRow(ctx,
		`INSERT INTO deliveries (book_id, to_addr, status)
		 VALUES ($1, $2, $3)
		 RETURNING `+deliveryColumns,
		bookID, toAddr, DeliveryQueued))
	if err != nil {
		return Delivery{}, fmt.Errorf("store: create delivery: %w", err)
	}
	return d, nil
}

// UpdateDeliveryStatus moves a delivery to status, stamping updated_at and
// recording errMsg (cleared when empty). The updated row is returned.
func (s *Store) UpdateDeliveryStatus(ctx context.Context, id int64, status, errMsg string) (Delivery, error) {
	d, err := scanDelivery(s.pool.QueryRow(ctx,
		`UPDATE deliveries
		   SET status = $2, error = $3, updated_at = now()
		 WHERE id = $1
		 RETURNING `+deliveryColumns,
		id, status, nullString(errMsg)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, ErrNotFound
	}
	if err != nil {
		return Delivery{}, fmt.Errorf("store: update delivery status: %w", err)
	}
	return d, nil
}

// GetDelivery returns the delivery with the given id, or ErrNotFound.
func (s *Store) GetDelivery(ctx context.Context, id int64) (Delivery, error) {
	d, err := scanDelivery(s.pool.QueryRow(ctx,
		`SELECT `+deliveryColumns+` FROM deliveries WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, ErrNotFound
	}
	if err != nil {
		return Delivery{}, fmt.Errorf("store: get delivery: %w", err)
	}
	return d, nil
}

// ListDeliveriesByBook returns a book's deliveries, most recent first.
func (s *Store) ListDeliveriesByBook(ctx context.Context, bookID int64) ([]Delivery, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+deliveryColumns+`
		 FROM deliveries WHERE book_id = $1
		 ORDER BY created_at DESC, id DESC`, bookID)
	if err != nil {
		return nil, fmt.Errorf("store: list deliveries: %w", err)
	}
	defer rows.Close()

	var ds []Delivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan delivery: %w", err)
		}
		ds = append(ds, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list deliveries: %w", err)
	}
	return ds, nil
}
