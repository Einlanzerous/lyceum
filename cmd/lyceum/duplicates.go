package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/magos/lyceum/internal/dedup"
	"github.com/magos/lyceum/internal/store"
)

// runFindDuplicates reports books already on the shelf that look like copies of
// each other (LYCM-113).
//
// Ingest gained duplicate detection, but it only runs on arrival — every book
// ingested before it, which is the whole existing library, was never asked. This
// closes that gap without waiting for each one to be re-ingested:
//
//	lyceum find-duplicates
//
// It only reports. Nothing is flagged, held, or deleted, because the decision it
// would be making is the one the review queue exists to hand to a person: two
// files of one work are often deliberate, and the tool cannot tell a better scan
// from a redundant one. Act on the output with the review UI or `DELETE
// /books/{id}`.
//
// Config (DB, data dir) comes from the same env as the server.
func runFindDuplicates(args []string) {
	if len(args) > 0 {
		log.Fatalf("find-duplicates: unexpected argument %q; this command takes none", args[0])
	}

	cfg := loadBackfillConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := store.Connect(ctx, cfg.databaseURL)
	if err != nil {
		log.Fatalf("find-duplicates: connect: %v", err)
	}
	defer pool.Close()

	s := store.New(pool, cfg.dataDir)
	candidates, err := s.ListDedupCandidates(ctx)
	if err != nil {
		log.Fatalf("find-duplicates: list books: %v", err)
	}

	pairs := findDuplicatePairs(candidates)
	if len(pairs) == 0 {
		fmt.Printf("No suspected duplicates among %d books.\n", len(candidates))
		return
	}

	fmt.Printf("%d suspected duplicate(s) among %d books:\n\n", len(pairs), len(candidates))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MATCHED\tKEEPS\tWHY\tTITLE")
	for _, p := range pairs {
		fmt.Fprintf(w, "%d\t%d\t%s\t%s — %s\n",
			p.later.ID, p.earlier.ID, p.reason, p.later.Title, p.later.Author)
	}
	_ = w.Flush()
	fmt.Println("\nNothing was changed. Delete the copy you don't want with the review UI or DELETE /books/{id}.")
}

// duplicatePair is one suspected duplicate: the later book and the earlier one
// it looks like a copy of.
type duplicatePair struct {
	later   dedup.Candidate
	earlier dedup.Candidate
	reason  string
}

// findDuplicatePairs runs every book against the ones added before it, so a pair
// is reported once — under the later id, which is the copy a person is likelier
// to want gone — rather than twice, once from each side.
//
// Ids ascend with insertion, so the prefix of the sorted slice is "everything
// that was already here". Each book is matched against that prefix only.
func findDuplicatePairs(candidates []dedup.Candidate) []duplicatePair {
	sorted := make([]dedup.Candidate, len(candidates))
	copy(sorted, candidates)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	byID := make(map[int64]dedup.Candidate, len(sorted))
	for _, c := range sorted {
		byID[c.ID] = c
	}

	var pairs []duplicatePair
	for i, c := range sorted {
		if m, ok := dedup.Find(c, sorted[:i]); ok {
			pairs = append(pairs, duplicatePair{later: c, earlier: byID[m.BookID], reason: m.Reason})
		}
	}
	return pairs
}
