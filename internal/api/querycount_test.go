package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/magos/lyceum/internal/store"
)

// queryCounter is a pgx tracer that counts query round-trips. It exists so a
// test can assert on how many a request costs, not just what it returns —
// LYCM-115 is a shape bug that every output-level assertion passes right
// through.
type queryCounter struct {
	mu sync.Mutex
	n  int
}

func (q *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	q.mu.Lock()
	q.n++
	q.mu.Unlock()
	return ctx
}

func (q *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (q *queryCounter) take() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := q.n
	q.n = 0
	return n
}

// tracedStore is testStore with a query counter wired into the pool. It uses its
// own Postgres schema so the counter sees this test's traffic alone.
func tracedStore(t *testing.T) (*store.Store, *queryCounter) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping DB-backed test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	counter := &queryCounter{}
	pool, err := connectSchemaWith(ctx, dsn, "lyceum_test_api_qc", func(cfg *pgxpool.Config) {
		cfg.ConnConfig.Tracer = counter
		// One connection: a second would re-prepare this test's statements on
		// first use, and while that does not raise the trace count it makes the
		// two halves of the comparison run against different connection state
		// for no reason.
		cfg.MaxConns = 1
	})
	if err != nil {
		t.Fatalf("connectSchemaWith: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	truncate(ctx, t, pool)
	return store.New(pool, t.TempDir()), counter
}

// TestLibraryQueryCountIsFlat pins the fix in LYCM-115. GET /library listed the
// books in one query and then ran two more per book — the caller's furthest
// position, and after LYCM-112 their read mark — so the cost grew as 1+2N.
//
// The assertion is that rendering five books costs the same number of queries as
// rendering one. That is the property; a fixed expected number would also pass
// while quietly encoding whatever the handler happens to do today.
func TestLibraryQueryCountIsFlat(t *testing.T) {
	s, counter := tracedStore(t)
	ctx := context.Background()
	owner := ownerID(ctx, t, s)
	srv := newServer(t, s)

	// Each book gets a position and a read mark, so the per-book queries the old
	// shape ran would all have had rows to find. A shelf of untouched books would
	// have hidden nothing, but it would prove less.
	seed := func(hash, title string) {
		t.Helper()
		b := seedBook(t, s, hash, title, "Ursula K. Le Guin", nil)
		if _, err := s.UpsertPosition(ctx, store.ReadingPosition{
			BookID: b.ID, UserID: owner, DeviceID: "kobo-1", CFI: "/6/4", Progress: 0.4,
		}); err != nil {
			t.Fatalf("UpsertPosition: %v", err)
		}
		if err := s.SetBookFinished(ctx, b.ID, owner, true); err != nil {
			t.Fatalf("SetBookFinished: %v", err)
		}
	}

	render := func(wantBooks int) int {
		t.Helper()
		counter.take() // discard seeding traffic
		resp, err := http.Get(srv.URL + "/library")
		if err != nil {
			t.Fatalf("GET /library: %v", err)
		}
		defer resp.Body.Close()
		var books []bookJSON
		if err := json.NewDecoder(resp.Body).Decode(&books); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(books) != wantBooks {
			t.Fatalf("shelf has %d books, want %d", len(books), wantBooks)
		}
		// The state has to actually be there — a handler that returned bare rows
		// would score a perfect query count.
		for _, b := range books {
			if b.Progress == nil || *b.Progress != 0.4 {
				t.Fatalf("book %d progress = %v, want 0.4", b.ID, b.Progress)
			}
			if !b.Finished {
				t.Fatalf("book %d not reported finished", b.ID)
			}
		}
		return counter.take()
	}

	// Warm the memoised owner row before counting anything: with user auth off
	// every request resolves the caller through it, and it loads lazily, so a
	// cold first render carries one extra query unrelated to shelf size.
	var warm []bookJSON
	getJSON(t, srv.URL+"/library", &warm)

	// An empty shelf must not load reader state at all: there is nothing to fold
	// it into, and loading it sweeps the caller's whole history to render `[]`.
	// This is the steady state of /ingest/review, which shares the code path.
	counter.take()
	var empty []bookJSON
	getJSON(t, srv.URL+"/library", &empty)
	if n := counter.take(); n != 1 {
		t.Errorf("empty shelf cost %d queries, want 1 (list books, no reader state)", n)
	}

	seed("qc-hash-1", "A Wizard of Earthsea")
	one := render(1)

	for i := 2; i <= 5; i++ {
		seed(fmt.Sprintf("qc-hash-%d", i), fmt.Sprintf("Earthsea %d", i))
	}
	five := render(5)

	if five != one {
		t.Errorf("GET /library ran %d queries for 5 books and %d for 1; the per-book queries are back (1+2N would be %d vs %d)",
			five, one, one+8, one)
	}
	// Guard the other direction: a handler that fetched everything for every
	// request in one giant query would also be flat, but this endpoint should
	// stay in single digits.
	if one > 10 {
		t.Errorf("GET /library ran %d queries for a single book; expected a handful", one)
	}
	t.Logf("GET /library: %d queries for 1 book, %d for 5", one, five)
}
