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
	inv, err := s.LinkBookToInventory(ctx, "9780140449334", "", book.ID, "The Odyssey", "Homer")
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
	inv, err := s.LinkBookToInventory(ctx, "9780140449334", "", book.ID, "EPUB Title", "Homer")
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

func TestReconcilePrintAndEbookByWork(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	// The Dinosaur Lords: a scan records the print ISBN under its work; the
	// acquired EPUB later ingests under a *different* ebook ISBN of the same work.
	const (
		printISBN = "9780765382115"
		ebookISBN = "9781429966115"
		work      = "/works/OL20125776W"
	)

	owned, err := s.UpsertInventory(ctx, Inventory{ISBN: printISBN, Title: "The Dinosaur Lords", Author: "Victor Milán", WorkID: work})
	if err != nil {
		t.Fatalf("UpsertInventory: %v", err)
	}

	book, err := s.InsertBook(ctx, sampleBook("hash-dino"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}

	// Ingesting the ebook edition must join the existing owned row, not create a
	// second one.
	linked, err := s.LinkBookToInventory(ctx, ebookISBN, work, book.ID, "The Dinosaur Lords", "Victor Milán")
	if err != nil {
		t.Fatalf("LinkBookToInventory: %v", err)
	}
	if linked.ID != owned.ID {
		t.Fatalf("ebook ingest created a new row (%d) instead of joining the print row (%d)", linked.ID, owned.ID)
	}
	if linked.State != StateIngested || linked.BookID == nil || *linked.BookID != book.ID {
		t.Fatalf("linked row = %+v, want ingested with book %d", linked, book.ID)
	}

	// One logical title, reachable by either ISBN.
	all, err := s.ListInventory(ctx)
	if err != nil {
		t.Fatalf("ListInventory: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("inventory has %d rows, want 1 after reconciliation", len(all))
	}
	for _, code := range []string{printISBN, ebookISBN} {
		got, err := s.GetInventoryByAnyISBN(ctx, code)
		if err != nil {
			t.Fatalf("GetInventoryByAnyISBN(%s): %v", code, err)
		}
		if got.ID != owned.ID {
			t.Fatalf("GetInventoryByAnyISBN(%s) = row %d, want %d", code, got.ID, owned.ID)
		}
	}

	// Re-ingesting the same ebook ISBN is idempotent — it updates in place via the
	// known-ISBN mapping (no work lookup needed).
	again, err := s.LinkBookToInventory(ctx, ebookISBN, "", book.ID, "", "")
	if err != nil {
		t.Fatalf("LinkBookToInventory (again): %v", err)
	}
	if again.ID != owned.ID {
		t.Fatalf("re-ingest split into row %d, want %d", again.ID, owned.ID)
	}
}

func TestLinkFreshEbookRegistersISBN(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	// A Bindery grab that no scan preceded: no prior row, no work match. A fresh
	// row is created and the ISBN is registered so a later lookup finds it.
	book, err := s.InsertBook(ctx, sampleBook("hash-fresh"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}
	inv, err := s.LinkBookToInventory(ctx, "9781429966115", "/works/OLxW", book.ID, "Title", "Author")
	if err != nil {
		t.Fatalf("LinkBookToInventory: %v", err)
	}
	got, err := s.GetInventoryByAnyISBN(ctx, "9781429966115")
	if err != nil {
		t.Fatalf("GetInventoryByAnyISBN: %v", err)
	}
	if got.ID != inv.ID || got.WorkID != "/works/OLxW" {
		t.Fatalf("lookup = %+v, want row %d with work stamped", got, inv.ID)
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
