package store

import (
	"context"
	"errors"
	"testing"
)

func TestPendingBookHiddenFromShelfUntilApproved(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	// A clean book publishes straight to the shelf.
	clean := sampleBook("hash-clean")
	if _, err := s.InsertBook(ctx, clean); err != nil {
		t.Fatalf("InsertBook clean: %v", err)
	}

	// A flagged book lands pending and must stay off the shelf.
	flagged := sampleBook("hash-flagged")
	flagged.Title = "D&D - Dragonlance - Chronicles 03"
	flagged.ReviewState = ReviewPending
	flagged.ReviewFlags = []string{"no_isbn", "suspicious_title"}
	pending, err := s.InsertBook(ctx, flagged)
	if err != nil {
		t.Fatalf("InsertBook flagged: %v", err)
	}
	if pending.ReviewState != ReviewPending {
		t.Fatalf("review_state = %q, want pending", pending.ReviewState)
	}
	if len(pending.ReviewFlags) != 2 {
		t.Fatalf("review_flags = %v, want the two detected codes", pending.ReviewFlags)
	}

	shelf, err := s.ListBooks(ctx)
	if err != nil {
		t.Fatalf("ListBooks: %v", err)
	}
	if len(shelf) != 1 || shelf[0].FileHash != "hash-clean" {
		t.Fatalf("shelf = %+v, want only the clean book", shelf)
	}

	queue, err := s.ListPendingBooks(ctx)
	if err != nil {
		t.Fatalf("ListPendingBooks: %v", err)
	}
	if len(queue) != 1 || queue[0].ID != pending.ID {
		t.Fatalf("queue = %+v, want the flagged book", queue)
	}

	// Approving publishes it and clears its flags.
	approved, err := s.ApproveBook(ctx, pending.ID)
	if err != nil {
		t.Fatalf("ApproveBook: %v", err)
	}
	if approved.ReviewState != ReviewPublished || len(approved.ReviewFlags) != 0 {
		t.Fatalf("approved = %+v, want published with no flags", approved)
	}

	shelf, _ = s.ListBooks(ctx)
	if len(shelf) != 2 {
		t.Fatalf("shelf has %d after approve, want 2", len(shelf))
	}
	if queue, _ = s.ListPendingBooks(ctx); len(queue) != 0 {
		t.Fatalf("queue not empty after approve: %+v", queue)
	}
}

func TestUpdateBookMeta(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	b, err := s.InsertBook(ctx, sampleBook("hash-meta"))
	if err != nil {
		t.Fatalf("InsertBook: %v", err)
	}

	updated, err := s.UpdateBookMeta(ctx, b.ID, "  Corrected Title  ", "  Corrected Author  ")
	if err != nil {
		t.Fatalf("UpdateBookMeta: %v", err)
	}
	if updated.Title != "Corrected Title" || updated.Author != "Corrected Author" {
		t.Fatalf("updated = %q / %q, want trimmed corrected values", updated.Title, updated.Author)
	}

	// An empty title is rejected (the column is NOT NULL and a blank title is
	// never a valid correction).
	if _, err := s.UpdateBookMeta(ctx, b.ID, "   ", "x"); err == nil {
		t.Fatal("UpdateBookMeta with blank title: want error, got nil")
	}

	if _, err := s.UpdateBookMeta(ctx, 999999, "Title", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateBookMeta unknown id = %v, want ErrNotFound", err)
	}
	if _, err := s.ApproveBook(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ApproveBook unknown id = %v, want ErrNotFound", err)
	}
}
