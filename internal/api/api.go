// Package api exposes Lyceum's read-side HTTP surface: the library listing and
// the cover/EPUB blob endpoints. It is mounted by cmd/lyceum on top of the
// existing /healthz route.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/magos/lyceum/internal/coverart"
	"github.com/magos/lyceum/internal/invite"
	"github.com/magos/lyceum/internal/store"
)

// Store is the slice of *store.Store behaviour the API depends on. Keeping it
// as an interface makes the handlers trivially testable with a fake.
type Store interface {
	ListBooks(ctx context.Context) ([]store.Book, error)
	GetBook(ctx context.Context, id int64) (store.Book, error)
	GetBookByHash(ctx context.Context, hash string) (store.Book, error)
	GetFurthestPosition(ctx context.Context, bookID, userID int64) (store.ReadingPosition, error)
	IsBookFinished(ctx context.Context, bookID, userID int64) (bool, error)
	SetBookFinished(ctx context.Context, bookID, userID int64, finished bool) error

	// Batch forms of the two above, so listing a shelf costs a fixed number of
	// queries rather than two per book (LYCM-115).
	ListFurthestPositions(ctx context.Context, userID int64) (map[int64]store.ReadingPosition, error)
	ListFinishedBooks(ctx context.Context, userID int64) (map[int64]struct{}, error)

	// Duplicate detection at ingest (LYCM-113): the shelf to match against, and
	// the pointer recording what a held book matched.
	ListDedupCandidates(ctx context.Context) ([]store.BookIdentity, error)
	SetDuplicateOf(ctx context.Context, id, duplicateOf int64) error
	GetPosition(ctx context.Context, bookID, userID int64, deviceID string) (store.ReadingPosition, error)
	UpsertPositionLWW(ctx context.Context, p store.ReadingPosition) (store.ReadingPosition, error)
	InsertBook(ctx context.Context, b store.Book) (store.Book, error)
	SaveBlobs(fileHash string, epub, cover []byte) (filePath, coverPath string, err error)
	ListDeliveriesByBook(ctx context.Context, bookID int64) ([]store.Delivery, error)

	// Book lifecycle (LYCM-66): stable-identity folder ingest + delete.
	GetBookBySourcePath(ctx context.Context, sourcePath string) (store.Book, error)
	SetBookSourcePath(ctx context.Context, id int64, sourcePath string) error
	UpdateBookContent(ctx context.Context, id int64, b store.Book) (store.Book, error)
	DeleteBook(ctx context.Context, id int64) (store.Book, error)
	RemoveBlobs(filePath string) error

	// Delete tombstones (LYCM-109): deleting a book does not touch the watched
	// media tree, so the deletion is recorded and folder ingest honours it —
	// otherwise the next scan brings the book straight back.
	TombstoneSource(ctx context.Context, sourcePath, fileHash string) error
	IsSourceTombstoned(ctx context.Context, sourcePath, fileHash string) (bool, error)
	ClearTombstone(ctx context.Context, fileHash string) error

	// Ingest QC review queue (LYCM-58): hold flagged ingests, then approve/edit.
	ListPendingBooks(ctx context.Context) ([]store.Book, error)
	ApproveBook(ctx context.Context, id int64) (store.Book, error)
	UpdateBookMeta(ctx context.Context, id int64, title, author string) (store.Book, error)
	SaveCoverAt(coverPath string, cover []byte) error
	SetCoverPath(ctx context.Context, id int64, coverPath string) error

	// Inventory (LYCM-601): ownership/acquisition state keyed by ISBN.
	UpsertInventory(ctx context.Context, inv store.Inventory) (store.Inventory, error)
	SetInventoryState(ctx context.Context, isbn, state string) (store.Inventory, error)
	SetInventorySeries(ctx context.Context, isbn, series string, index float64) (store.Inventory, error)
	LinkBookToInventory(ctx context.Context, isbn, workID string, bookID int64, title, author string) (store.Inventory, error)
	ListInventory(ctx context.Context) ([]store.Inventory, error)
	GetInventoryByISBN(ctx context.Context, isbn string) (store.Inventory, error)
	GetInventoryByAnyISBN(ctx context.Context, isbn string) (store.Inventory, error)

	// Series intent application (LYCM-82): the confirm-time series lands on the
	// book once its EPUB ingests (or immediately, when one is already linked).
	UpdateBookSeries(ctx context.Context, id int64, series string, index float64) (store.Book, error)

	// ISBN ingest batch review (LYCM-603): scans -> candidates -> confirm.
	CreateBatch(ctx context.Context, sourceDevice string) (store.Batch, error)
	GetBatch(ctx context.Context, id int64) (store.Batch, error)
	ListBatches(ctx context.Context) ([]store.Batch, error)
	SetBatchStatus(ctx context.Context, id int64, status string) (store.Batch, error)
	AddCandidate(ctx context.Context, c store.Candidate) (store.Candidate, error)
	GetCandidate(ctx context.Context, id int64) (store.Candidate, error)
	ListCandidates(ctx context.Context, batchID int64) ([]store.Candidate, error)
	UpdateCandidate(ctx context.Context, c store.Candidate) (store.Candidate, error)

	// Accounts (LYCM-801): the household's users plus the invite/session tokens
	// that authenticate them. See session.go.
	CreateUser(ctx context.Context, email, displayName string) (store.User, error)
	GetUser(ctx context.Context, id int64) (store.User, error)
	// GetUserByEmail matches a Cloudflare Access-verified email to an account
	// (case-insensitive) for SSO sign-in (LYCM-803); MintToken issues the session
	// that sign-in yields.
	GetUserByEmail(ctx context.Context, email string) (store.User, error)
	MintToken(ctx context.Context, userID int64, kind, label string, expiresAt *time.Time) (string, error)
	GetOwner(ctx context.Context) (store.User, error)
	ListUsers(ctx context.Context) ([]store.User, error)
	UpdateDisplayName(ctx context.Context, id int64, displayName string) (store.User, error)
	DeleteUser(ctx context.Context, id int64) error
	// MintInvite issues a single-use invite plus a short pairing code that stands
	// for the same invite (LYCM-88), returning both plaintexts once.
	MintInvite(ctx context.Context, userID int64, label string, expiresAt *time.Time) (token string, code string, err error)
	// RevokeUnredeemedInvites retires a user's outstanding invites carrying a
	// label, so minting a replacement device key doesn't leave the old one live
	// (LYCM-105).
	RevokeUnredeemedInvites(ctx context.Context, userID int64, label string) (int64, error)
	UserByToken(ctx context.Context, plaintext string) (store.User, error)
	RedeemInvite(ctx context.Context, plaintext, deviceLabel string) (store.User, string, error)
	// RedeemPairingCode exchanges a short pairing code for a session, the
	// pairing-code analogue of RedeemInvite (LYCM-88).
	RedeemPairingCode(ctx context.Context, code, deviceLabel string) (store.User, string, error)
	RevokeToken(ctx context.Context, plaintext string) error
	ListSessions(ctx context.Context, userID int64, currentPlaintext string) ([]store.Session, error)
	RevokeSession(ctx context.Context, userID, id int64) error
	ListMembers(ctx context.Context) ([]store.Member, error)
}

// API bundles the dependencies the handlers need.
type API struct {
	store    Store
	dataDir  string
	auth     *TokenAuth       // bearer-token table for the /eidolon + delivery routes
	delivery *deliveryConfig  // "Send to Kindle" dispatcher + policy (nil when unconfigured)
	acquirer Acquirer         // ISBN -> DRM-free copy requester (logging no-op by default)
	covers   coverart.Fetcher // ISBN -> canonical cover art (nil = use embedded covers only)
	resolver Resolver         // ISBN/title -> candidate editions (no-op no-match by default)

	// wantSem bounds concurrent background acquisition dispatches (LYCM-79) so a
	// big batch confirm doesn't hammer the backend all at once; wantWG lets tests
	// (and a graceful shutdown) wait for in-flight dispatches.
	wantSem chan struct{}
	wantWG  sync.WaitGroup

	// ownerUser memoises the owner account (LYCM-801). With user auth off it is
	// resolved on every request, so re-querying it would cost a round-trip per
	// cover blob; see (*API).owner in session.go.
	ownerMu   sync.RWMutex
	ownerUser store.User

	normalizeCovers bool // trim/aspect/downscale stored covers at ingest (LYCM-65)
	ingestQC        bool // hold flagged new ingests for review (LYCM-58); off unless wired
	userAuth        bool // require a session token on the reader core (LYCM-801)

	// cfAccess verifies Cloudflare Access JWTs for the browser SSO sign-in
	// (LYCM-803). nil when CF_ACCESS_* is unconfigured, which disables the
	// /auth/sso/cloudflare endpoint (it returns sso_disabled).
	cfAccess *CFAccessVerifier

	// pairingLimiter caps pairing-code sign-in attempts per client IP (LYCM-88).
	pairingLimiter *ipRateLimiter

	// mobileBaseURL is the origin minted invites advertise (LYCM-102). Empty
	// leaves sign_in_url out of the payload; see WithMobileBaseURL.
	mobileBaseURL string
}

// blobCacheControl is the caching policy for the cover and EPUB blob routes.
//
// The bytes are content-addressed and immutable, so they can be cached hard. But
// once the routes require a session (LYCM-801) the response must not be marked
// `public`: a shared cache in front of Lyceum — a reverse proxy, or the
// Cloudflare edge the LYCM-803 work puts us behind — would happily store one
// and then serve it to a caller with no session at all, routing straight around
// the gate. `private` keeps it in the browser's own cache, where it belongs.
func (a *API) blobCacheControl() string {
	if a.userAuth {
		return "private, max-age=31536000, immutable"
	}
	return "public, max-age=31536000, immutable"
}

// Option configures an API at construction time.
type Option func(*API)

// WithAuth installs the bearer-token table guarding the ecosystem hooks
// (/eidolon/*) and the send-to-kindle route. Without it those routes are
// closed (every request 401s); core reader routes are unaffected.
func WithAuth(auth *TokenAuth) Option {
	return func(a *API) { a.auth = auth }
}

// WithCoverFetcher installs a source of canonical cover art (e.g. Open Library)
// consulted at ingest. When set, a freshly-ingested book with a usable ISBN
// prefers the fetched cover over its embedded one, falling back to the embedded
// cover when no art is found. Without it, ingest uses embedded covers only.
func WithCoverFetcher(f coverart.Fetcher) Option {
	return func(a *API) { a.covers = f }
}

// WithCoverNormalize toggles the ingest-time cover normalization pass (LYCM-65):
// trimming uniform frames, padding to the shelf aspect, and downscaling the
// stored cover. It is on by default (see New); pass false to store cover bytes
// verbatim. Normalization never changes which cover is chosen — only the bytes
// that get written — and is best-effort, so a cover it can't process is stored
// unchanged.
func WithCoverNormalize(enabled bool) Option {
	return func(a *API) { a.normalizeCovers = enabled }
}

// WithIngestQC toggles the ingestion QC review queue (LYCM-58). When on, a new
// ingest that trips a detector (no ISBN, poor source cover, mangled title) is
// held pending-review and kept off the shelf until approved; clean ingests
// publish straight through. It is off by default (see New) so existing callers
// and tests are unaffected; main.go turns it on via LYCEUM_INGEST_QC.
func WithIngestQC(enabled bool) Option {
	return func(a *API) { a.ingestQC = enabled }
}

// WithUserAuth toggles session-token enforcement on the reader core (LYCM-801).
//
// It is off by default, and main.go leaves it off unless LYCEUM_AUTH is set.
// While off, every request to a gated route is served as the owner — the exact
// behaviour Lyceum had before accounts existed — so a server whose clients don't
// yet sign in keeps working. The clients gain a sign-in screen in a follow-up,
// and flipping this on is what actually closes the door.
//
// It never affects the /eidolon and send-to-kindle routes: those are guarded by
// the separate service-token scopes (see auth.go) and are closed either way.
func WithUserAuth(enabled bool) Option {
	return func(a *API) { a.userAuth = enabled }
}

// WithCFAccess installs the Cloudflare Access JWT verifier that backs browser
// SSO sign-in (LYCM-803). When set, POST /auth/sso/cloudflare exchanges a
// tunnel-injected Cf-Access-Jwt-Assertion for a Lyceum session; without it that
// route returns sso_disabled and clients fall back to invite/pairing sign-in.
// main.go installs it only when CF_ACCESS_TEAM_DOMAIN and CF_ACCESS_AUD are set.
func WithCFAccess(v *CFAccessVerifier) Option {
	return func(a *API) { a.cfAccess = v }
}

// WithMobileBaseURL sets the origin that minted invites advertise to clients
// (LYCM-102) — the public, bearer-authenticated hostname a phone can actually
// reach, which is not the Cloudflare-gated one the owner's browser is on.
//
// When set, every invite reveal carries a ready-made `sign_in_url`; when unset
// the field is omitted and each client falls back to building the link from the
// origin it knows, which is right for LAN and dev. main.go supplies it from
// LYCEUM_MOBILE_BASE_URL.
//
// The base is normalized and validated once here rather than per mint, and an
// unusable one is dropped rather than stored: clients prefer this URL over the
// one they would have built themselves, so keeping a malformed value would put
// out a QR that scans nowhere *and* suppress the fallback that still worked.
// main.go validates first so it can say so in the log; this keeps direct callers
// (and tests) honest too.
func WithMobileBaseURL(base string) Option {
	return func(a *API) {
		normalized, err := invite.NormalizeBase(base)
		if err != nil {
			return
		}
		a.mobileBaseURL = normalized
	}
}

// New builds an API over the given store. dataDir is retained for symmetry with
// the store's blob layout; the handlers serve whatever absolute or relative
// paths the book rows carry, so it is informational only.
func New(s Store, dataDir string, opts ...Option) *API {
	a := &API{
		store: s, dataDir: dataDir, acquirer: logAcquirer{}, resolver: nullResolver{},
		wantSem:         make(chan struct{}, maxConcurrentWants),
		normalizeCovers: true,
		pairingLimiter:  newIPRateLimiter(pairingRateWindow, pairingRateBurst),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Handler returns a ServeMux wired with the library and blob routes. Callers
// mount it (it does not register /healthz, which main.go owns).
func (a *API) Handler() *http.ServeMux {
	mux := http.NewServeMux()

	// Sign-in (LYCM-801). Redeeming an invite is the one route that must stay
	// reachable without a session — it is how a client gets one.
	mux.HandleFunc("POST /auth/session", a.handleAuthSession)
	// Cloudflare Access SSO (LYCM-803). The browser SPA, sitting behind the CF
	// edge, calls this on load to trade its tunnel-verified identity for a
	// session — no second login. Like /auth/session it must stay reachable
	// without a session; the CF JWT is the credential.
	mux.HandleFunc("POST /auth/sso/cloudflare", a.handleAuthCFAccess)
	mux.HandleFunc("DELETE /auth/session", a.requireUser(a.handleAuthSignOut))
	mux.HandleFunc("GET /auth/me", a.requireUser(a.handleAuthMe))
	mux.HandleFunc("PATCH /auth/me", a.requireUser(a.handleAuthUpdateMe))
	// "Add a device" (LYCM-105) — a key for yourself, not for a housemate, so it
	// needs no ownership. See handleSelfInvite for why it is not an /admin route.
	mux.HandleFunc("POST /auth/invite", a.requireUser(a.handleSelfInvite))

	// Your devices. A password-free session never expires, so the only real risk
	// is a lost or lent device staying signed in forever — this is how its owner
	// sees it and cuts it off.
	mux.HandleFunc("GET /auth/sessions", a.requireUser(a.handleSessionList))
	mux.HandleFunc("DELETE /auth/sessions/{id}", a.requireUser(a.handleSessionRevoke))

	// Household administration (LYCM-801). Owner only. POST /admin/users is the
	// hook Purser's `lyceum` connector calls (SERV-38).
	mux.HandleFunc("POST /admin/users", a.requireOwner(a.handleAdminUserCreate))
	mux.HandleFunc("GET /admin/users", a.requireOwner(a.handleAdminUserList))
	mux.HandleFunc("POST /admin/users/{id}/invite", a.requireOwner(a.handleAdminUserInvite))
	mux.HandleFunc("DELETE /admin/users/{id}", a.requireOwner(a.handleAdminUserDelete))

	// The reader core. Every route below is the household's own read/write
	// surface and requires a signed-in user (LYCM-801) — distinct from the
	// scoped service tokens that guard the ecosystem hooks further down.
	mux.HandleFunc("POST /upload", a.requireUser(a.handleUpload))
	mux.HandleFunc("GET /library", a.requireUser(a.handleLibrary))

	// Inventory (LYCM-601): the scan/capture surface LYCM-602 feeds.
	mux.HandleFunc("POST /inventory", a.requireUser(a.handleInventoryCreate))
	mux.HandleFunc("GET /inventory", a.requireUser(a.handleInventoryList))

	// ISBN ingest batch review (LYCM-603): upload scans, verify matches on the
	// desktop, confirm into inventory.
	mux.HandleFunc("POST /ingest/batches", a.requireUser(a.handleBatchCreate))
	mux.HandleFunc("GET /ingest/batches", a.requireUser(a.handleBatchList))
	mux.HandleFunc("GET /ingest/batches/{id}", a.requireUser(a.handleBatchGet))
	mux.HandleFunc("POST /ingest/batches/{id}/candidates", a.requireUser(a.handleBatchAddCandidate))
	mux.HandleFunc("POST /ingest/batches/{id}/confirm-ready", a.requireUser(a.handleBatchConfirmReady))
	mux.HandleFunc("POST /ingest/candidates/{id}/pick", a.requireUser(a.handleCandidatePick))
	mux.HandleFunc("POST /ingest/candidates/{id}/confirm", a.requireUser(a.handleCandidateConfirm))
	mux.HandleFunc("POST /ingest/candidates/{id}/skip", a.requireUser(a.handleCandidateSkip))
	mux.HandleFunc("GET /ingest/search", a.requireUser(a.handleIngestSearch))

	mux.HandleFunc("PUT /sync", a.requireUser(a.handleSyncPut))
	mux.HandleFunc("GET /sync", a.requireUser(a.handleSyncGet))
	mux.HandleFunc("GET /books/{id}", a.requireUser(a.handleGetBook))
	mux.HandleFunc("GET /books/{id}/cover", a.requireUser(a.handleCover))
	mux.HandleFunc("GET /books/{id}/file", a.requireUser(a.handleFile))
	mux.HandleFunc("DELETE /books/{id}", a.requireUser(a.handleDelete))
	mux.HandleFunc("PATCH /books/{id}", a.requireUser(a.handleUpdateBook))
	mux.HandleFunc("PUT /books/{id}/finished", a.requireUser(a.handleSetFinished))

	// Ingest QC review queue (LYCM-58): held books plus approve / replace-cover.
	mux.HandleFunc("GET /ingest/review", a.requireUser(a.handleReviewList))
	mux.HandleFunc("POST /books/{id}/approve", a.requireUser(a.handleApprove))
	mux.HandleFunc("POST /books/{id}/cover", a.requireUser(a.handleReplaceCover))
	mux.HandleFunc("POST /books/{id}/cover/refetch", a.requireUser(a.handleRefetchCover))

	// "Send to Kindle" (LYCM-402). Both routes require the delivery:send scope.
	mux.HandleFunc("POST /books/{id}/send-to-kindle", a.requireScope(ScopeDeliverySend, a.handleSendToKindle))
	mux.HandleFunc("GET /books/{id}/deliveries", a.requireScope(ScopeDeliverySend, a.handleListDeliveries))

	// Project Eidolon hooks (LYCM-403/404). Read-only; require eidolon:read.
	mux.HandleFunc("GET /eidolon/books/{id}/location", a.requireScope(ScopeEidolonRead, a.handleEidolonLocation))
	mux.HandleFunc("GET /eidolon/books/{id}/chapter", a.requireScope(ScopeEidolonRead, a.handleEidolonChapter))
	return mux
}

// bookJSON is the wire shape for a single library entry.
type bookJSON struct {
	ID       int64    `json:"id"`
	Title    string   `json:"title"`
	Author   string   `json:"author"`
	CoverURL string   `json:"cover_url"`
	Progress *float64 `json:"progress,omitempty"`
	// AddedAt (RFC3339) backs the "recently added" library sort. Series and
	// SeriesIndex drive series roll-up in the library grid; both are omitted for
	// standalone books (LYCM-36 / LYCM-62).
	AddedAt     string   `json:"added_at"`
	Series      string   `json:"series,omitempty"`
	SeriesIndex *float64 `json:"series_index,omitempty"`
	// ReadAt (RFC3339) is when the book's latest reading position was recorded;
	// it lets the client pin the most-recently-read book to the top of the
	// shelf. Omitted when the book has never been opened.
	ReadAt string `json:"read_at,omitempty"`
	// Finished is true when the caller has explicitly marked the book read,
	// regardless of reading progress (LYCM mark-as-read). Like Progress it is the
	// signed-in user's own, not the household's (LYCM-112).
	Finished bool `json:"finished,omitempty"`
	// ReviewState and ReviewFlags surface the ingest-QC status (LYCM-58). Shelf
	// entries are always "published" so both are omitted there; the review queue
	// carries "pending" plus the detected issue codes.
	ReviewState string   `json:"review_state,omitempty"`
	ReviewFlags []string `json:"review_flags,omitempty"`
	// DuplicateOf names the book a possible_duplicate entry in ReviewFlags is
	// about (LYCM-113), so the review UI can show the two side by side. Omitted
	// unless the book is held as a suspected duplicate, and omitted again once
	// the book it pointed at is deleted.
	DuplicateOf int64 `json:"duplicate_of,omitempty"`
}

func coverURL(id int64) string { return fmt.Sprintf("/books/%d/cover", id) }

// readerState is one signed-in person's per-book state: how far they got in each
// book and which ones they have marked read. The shelf is shared but this is
// not (LYCM-801, LYCM-112), so it is always one user's.
//
// Assembling a list response reads it once up front (LYCM-115) instead of
// querying per book, which made a shelf render 1+2N queries. Its zero value is a
// reader with no history, which is also what every lookup on it returns for a
// book they have never opened.
type readerState struct {
	positions map[int64]store.ReadingPosition
	finished  map[int64]struct{}
}

// readerStateAll loads the caller's state across every book, for list responses.
// ctx must come from a request handled behind requireUser.
func (a *API) readerStateAll(ctx context.Context) (readerState, error) {
	uid := userFrom(ctx).ID
	positions, err := a.store.ListFurthestPositions(ctx, uid)
	if err != nil {
		return readerState{}, err
	}
	finished, err := a.store.ListFinishedBooks(ctx, uid)
	if err != nil {
		return readerState{}, err
	}
	return readerState{positions: positions, finished: finished}, nil
}

// readerStateOne loads the caller's state for a single book. Single-book
// responses keep the per-book path: two indexed lookups beat sweeping a
// reader's whole history to answer for one book.
func (a *API) readerStateOne(ctx context.Context, bookID int64) (readerState, error) {
	uid := userFrom(ctx).ID
	st := readerState{}
	if pos, err := a.store.GetFurthestPosition(ctx, bookID, uid); err == nil {
		st.positions = map[int64]store.ReadingPosition{bookID: pos}
	} else if !errors.Is(err, store.ErrNotFound) {
		return readerState{}, err
	}
	done, err := a.store.IsBookFinished(ctx, bookID, uid)
	if err != nil {
		return readerState{}, err
	}
	if done {
		st.finished = map[int64]struct{}{bookID: {}}
	}
	return st, nil
}

// bookJSONFor assembles the wire shape for one book, folding in its cover URL,
// series fields, latest reading position, and finished state.
//
// Progress, ReadAt and Finished come from st and are the signed-in user's; the
// caller is responsible for having loaded st for that user.
func (a *API) bookJSONFor(b store.Book, st readerState) bookJSON {
	entry := bookJSON{
		ID:      b.ID,
		Title:   b.Title,
		Author:  b.Author,
		AddedAt: b.AddedAt.UTC().Format(time.RFC3339),
		Series:  b.Series,
	}
	if b.ReviewState == store.ReviewPending {
		entry.ReviewState = b.ReviewState
		entry.ReviewFlags = b.ReviewFlags
		entry.DuplicateOf = b.DuplicateOf
	}
	if b.CoverPath != "" {
		entry.CoverURL = coverURL(b.ID)
	}
	if b.SeriesIndex != 0 {
		idx := b.SeriesIndex
		entry.SeriesIndex = &idx
	}
	if pos, ok := st.positions[b.ID]; ok {
		p := pos.Progress
		entry.Progress = &p
		entry.ReadAt = pos.UpdatedAt.UTC().Format(time.RFC3339)
	}
	_, entry.Finished = st.finished[b.ID]
	return entry
}

func (a *API) handleLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	books, err := a.store.ListBooks(ctx)
	if err != nil {
		serverError(w, "list books", err)
		return
	}
	// An empty shelf needs no reader state, and loading it would sweep the
	// caller's whole history to answer for nothing.
	st := readerState{}
	if len(books) > 0 {
		st, err = a.readerStateAll(ctx)
		if err != nil {
			serverError(w, "read reader state", err)
			return
		}
	}

	out := make([]bookJSON, 0, len(books))
	for _, b := range books {
		out = append(out, a.bookJSONFor(b, st))
	}

	writeJSON(w, http.StatusOK, out)
}

// handleGetBook returns a single book's wire shape (used by the reader to read
// finished/progress state without loading the whole library).
func (a *API) handleGetBook(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid book id", http.StatusBadRequest)
		return
	}
	b, err := a.store.GetBook(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "book not found", http.StatusNotFound)
		return
	case err != nil:
		serverError(w, "get book", err)
		return
	}
	st, err := a.readerStateOne(r.Context(), b.ID)
	if err != nil {
		serverError(w, "build book json", err)
		return
	}
	writeJSON(w, http.StatusOK, a.bookJSONFor(b, st))
}

func (a *API) handleCover(w http.ResponseWriter, r *http.Request) {
	b, ok := a.lookupBook(w, r)
	if !ok {
		return
	}
	if b.CoverPath == "" {
		http.Error(w, "no cover", http.StatusNotFound)
		return
	}
	// Covers are content-addressed and effectively immutable, so they can be
	// cached aggressively. Content-Type is sniffed since the blob is stored
	// extensionless (cover.bin).
	w.Header().Set("Cache-Control", a.blobCacheControl())
	serveBlob(w, r, b.CoverPath, "")
}

func (a *API) handleFile(w http.ResponseWriter, r *http.Request) {
	b, ok := a.lookupBook(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", a.blobCacheControl())
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("book-%d.epub", b.ID)))
	serveBlob(w, r, b.FilePath, "application/epub+zip")
}

// handleSetFinished marks a book read or unread for the signed-in user, whose
// mark is theirs alone (LYCM-112). Body: {"finished": bool}.
func (a *API) handleSetFinished(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid book id", http.StatusBadRequest)
		return
	}
	var req struct {
		Finished bool `json:"finished"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	switch err := a.store.SetBookFinished(ctx, id, userFrom(ctx).ID, req.Finished); {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "book not found", http.StatusNotFound)
		return
	case err != nil:
		serverError(w, "set finished", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDelete removes a book and its on-disk blobs (LYCM-66). It responds 204
// on success, 404 if no book has the id. Dependent rows are handled by the
// schema FKs (reading_positions/book_reads/deliveries cascade, inventory link
// nulled), so this is safe without an explicit cleanup pass.
func (a *API) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid book id", http.StatusBadRequest)
		return
	}
	deleted, err := a.store.DeleteBook(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "book not found", http.StatusNotFound)
		return
	case err != nil:
		serverError(w, "delete book", err)
		return
	}
	// Remember the deletion before cleaning up. Lyceum owns its blobs but not the
	// watched media tree, so a folder-ingested book's file outlives the row and
	// the watcher would re-ingest it on the next restart (LYCM-109). An uploaded
	// book has no watched file and so is not tombstoned — see TombstoneSource.
	// Logged, not fatal: the row really is gone either way, and failing the
	// request would wrongly suggest the book is still there.
	if err := a.store.TombstoneSource(r.Context(), deleted.SourcePath, deleted.FileHash); err != nil {
		log.Printf("api: delete book %d: tombstone source: %v", deleted.ID, err)
	}
	// The row is gone; a leftover blob dir is only wasted disk, so a cleanup
	// failure is logged, not surfaced as an error to the caller.
	if err := a.store.RemoveBlobs(deleted.FilePath); err != nil {
		log.Printf("api: delete book %d: remove blobs: %v", deleted.ID, err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// lookupBook parses the {id} path value and loads the book, writing the
// appropriate 4xx/5xx response and returning ok=false on any failure.
func (a *API) lookupBook(w http.ResponseWriter, r *http.Request) (store.Book, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid book id", http.StatusBadRequest)
		return store.Book{}, false
	}
	b, err := a.store.GetBook(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "book not found", http.StatusNotFound)
		return store.Book{}, false
	}
	if err != nil {
		serverError(w, "get book", err)
		return store.Book{}, false
	}
	return b, true
}

// serveBlob streams the file at path. If contentType is non-empty it is used
// verbatim; otherwise http.ServeContent sniffs it from the bytes. ServeContent
// also gives us Range support, Last-Modified and conditional requests for free.
func serveBlob(w http.ResponseWriter, r *http.Request, path, contentType string) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "blob missing", http.StatusNotFound)
			return
		}
		serverError(w, "open blob", err)
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		serverError(w, "stat blob", err)
		return
	}
	if contentType == "" {
		// Blobs are stored extensionless (e.g. cover.bin), so let ServeContent's
		// extension-based guess fall through to content sniffing.
		var err error
		if contentType, err = sniffContentType(f); err != nil {
			serverError(w, "sniff content type", err)
			return
		}
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// sniffContentType reads the leading bytes of f to detect its media type, then
// rewinds f so the subsequent ServeContent streams from the start.
func sniffContentType(f *os.File) (string, error) {
	var buf [512]byte
	n, err := f.Read(buf[:])
	if err != nil && err != io.EOF {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return http.DetectContentType(buf[:n]), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: encode response: %v", err)
	}
}

func serverError(w http.ResponseWriter, what string, err error) {
	log.Printf("api: %s: %v", what, err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
