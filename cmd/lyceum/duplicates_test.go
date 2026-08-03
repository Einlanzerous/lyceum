package main

import (
	"testing"

	"github.com/magos/lyceum/internal/dedup"
	"github.com/magos/lyceum/internal/store"
)

// TestFindDuplicatePairsReportsEachPairOnce: matching every book against every
// other would report a pair twice, once from each side, and a report that
// double-counts is one nobody trusts. Each book is checked only against those
// added before it.
func TestFindDuplicatePairsReportsEachPairOnce(t *testing.T) {
	pairs := findDuplicatePairs([]store.BookIdentity{
		{ID: 3, Title: "Piranesi", Author: "Susanna Clarke"},
		{ID: 1, Title: "Piranesi", Author: "Susanna Clarke"},
		{ID: 2, Title: "Dune", Author: "Frank Herbert"},
	})

	if len(pairs) != 1 {
		t.Fatalf("got %d pairs, want 1: %+v", len(pairs), pairs)
	}
	// Reported under the later id: that is the copy someone is likelier to drop,
	// and it keeps the output stable regardless of what order the rows came back.
	if pairs[0].later.ID != 3 || pairs[0].earlier.ID != 1 {
		t.Errorf("pair = later %d / earlier %d, want later 3 / earlier 1",
			pairs[0].later.ID, pairs[0].earlier.ID)
	}
	if pairs[0].reason != dedup.ReasonTitleAuthor {
		t.Errorf("reason = %q, want %q", pairs[0].reason, dedup.ReasonTitleAuthor)
	}
}

// TestFindDuplicatePairsCleanLibrary: the expected answer on a healthy shelf.
func TestFindDuplicatePairsCleanLibrary(t *testing.T) {
	pairs := findDuplicatePairs([]store.BookIdentity{
		{ID: 1, Title: "Piranesi", Author: "Susanna Clarke"},
		{ID: 2, Title: "Dune", Author: "Frank Herbert"},
		{ID: 3, Title: "Dune Messiah", Author: "Frank Herbert"},
	})
	if len(pairs) != 0 {
		t.Errorf("clean library reported %d pairs: %+v", len(pairs), pairs)
	}
}

// TestFindDuplicatePairsTripleCopy: three copies of one book report as two
// pairs, both pointing at the original, so deleting the two later ids resolves
// the lot in one pass.
func TestFindDuplicatePairsTripleCopy(t *testing.T) {
	pairs := findDuplicatePairs([]store.BookIdentity{
		{ID: 1, Title: "Dune", Author: "Frank Herbert"},
		{ID: 2, Title: "Dune", Author: "Frank Herbert"},
		{ID: 3, Title: "Dune", Author: "Frank Herbert"},
	})
	if len(pairs) != 2 {
		t.Fatalf("got %d pairs, want 2: %+v", len(pairs), pairs)
	}
	for _, p := range pairs {
		if p.earlier.ID != 1 {
			t.Errorf("pair for book %d points at %d, want the original (1)", p.later.ID, p.earlier.ID)
		}
	}
}
