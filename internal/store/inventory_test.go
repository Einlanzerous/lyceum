package store

import (
	"context"
	"errors"
	"testing"
)

func TestUpsertInventoryCreateAndFind(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	// First scan: creates an owned entry.
	inv, err := s.UpsertInventory(ctx, Inventory{ISBN: "9780140449334", Title: "The Odyssey", Author: "Homer"})
	if err != nil {
		t.Fatalf("UpsertInventory: %v", err)
	}
	if inv.ID == 0 || inv.State != StateOwned {
		t.Fatalf("first upsert = %+v, want owned with id", inv)
	}

	// Move it to wanted, then re-scan: title/author backfill must not clobber,
	// and the state must be preserved (not reset to owned).
	if _, err := s.SetInventoryState(ctx, "9780140449334", StateWanted); err != nil {
		t.Fatalf("SetInventoryState: %v", err)
	}
	again, err := s.UpsertInventory(ctx, Inventory{ISBN: "9780140449334", Title: "Ignored", Author: "Ignored"})
	if err != nil {
		t.Fatalf("UpsertInventory (again): %v", err)
	}
	if again.ID != inv.ID {
		t.Fatalf("re-scan created a new row: %d vs %d", again.ID, inv.ID)
	}
	if again.Title != "The Odyssey" || again.Author != "Homer" {
		t.Fatalf("re-scan clobbered metadata: %+v", again)
	}
	if again.State != StateWanted {
		t.Fatalf("re-scan changed state to %q, want wanted", again.State)
	}
}

func TestLinkBookToInventory(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	book, err := s.InsertBook(ctx, sampleBook("hash-link"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}

	// Linking with no prior inventory row creates one in the ingested state.
	inv, err := s.LinkBookToInventory(ctx, "9780140449334", book.ID, "The Odyssey", "Homer")
	if err != nil {
		t.Fatalf("LinkBookToInventory: %v", err)
	}
	if inv.State != StateIngested {
		t.Fatalf("state = %q, want ingested", inv.State)
	}
	if inv.BookID == nil || *inv.BookID != book.ID {
		t.Fatalf("book_id = %v, want %d", inv.BookID, book.ID)
	}

	// Deleting the book nulls the link but keeps the ownership record.
	if _, err := s.pool.Exec(ctx, `DELETE FROM books WHERE id = $1`, book.ID); err != nil {
		t.Fatalf("delete book: %v", err)
	}
	got, err := s.GetInventoryByISBN(ctx, "9780140449334")
	if err != nil {
		t.Fatalf("GetInventoryByISBN: %v", err)
	}
	if got.BookID != nil {
		t.Fatalf("book_id = %v after book delete, want nil", got.BookID)
	}
}

func TestLinkBackfillsExistingEntry(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	// A prior scan recorded the title; ingest later supplies a book + author.
	if _, err := s.UpsertInventory(ctx, Inventory{ISBN: "9780140449334", Title: "Scanned Title"}); err != nil {
		t.Fatalf("UpsertInventory: %v", err)
	}
	book, err := s.InsertBook(ctx, sampleBook("hash-backfill"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	inv, err := s.LinkBookToInventory(ctx, "9780140449334", book.ID, "EPUB Title", "Homer")
	if err != nil {
		t.Fatalf("LinkBookToInventory: %v", err)
	}
	if inv.Title != "Scanned Title" {
		t.Fatalf("title = %q, want the scanned one preserved", inv.Title)
	}
	if inv.Author != "Homer" {
		t.Fatalf("author = %q, want backfilled from EPUB", inv.Author)
	}
	if inv.State != StateIngested {
		t.Fatalf("state = %q, want ingested", inv.State)
	}
}

func TestInventoryNotFound(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	if _, err := s.GetInventoryByISBN(ctx, "9780000000002"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetInventoryByISBN = %v, want ErrNotFound", err)
	}
	if _, err := s.SetInventoryState(ctx, "9780000000002", StateWanted); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetInventoryState = %v, want ErrNotFound", err)
	}
}

func TestListInventory(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for _, code := range []string{"9780140449334", "9780804429573"} {
		if _, err := s.UpsertInventory(ctx, Inventory{ISBN: code}); err != nil {
			t.Fatalf("UpsertInventory %s: %v", code, err)
		}
	}
	got, err := s.ListInventory(ctx)
	if err != nil {
		t.Fatalf("ListInventory: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListInventory returned %d, want 2", len(got))
	}
}
