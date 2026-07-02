package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/magos/lyceum/internal/delivery"
	"github.com/magos/lyceum/internal/store"
)

// BookSender is the slice of delivery behaviour the dispatcher needs. It is an
// interface so tests can substitute a capturing fake for the real SMTP sender.
type BookSender interface {
	SendBook(ctx context.Context, toAddr string, book delivery.Book) error
}

// dispatcherStore is the store surface the async worker writes through.
type dispatcherStore interface {
	CreateDelivery(ctx context.Context, bookID int64, toAddr string) (store.Delivery, error)
	UpdateDeliveryStatus(ctx context.Context, id int64, status, errMsg string) (store.Delivery, error)
}

// deliveryJob is one unit of queued work. It carries the blob path (not the
// bytes) so the queue stays cheap; the worker reads the EPUB when it runs.
type deliveryJob struct {
	deliveryID int64
	toAddr     string
	title      string
	filename   string
	filePath   string
}

// Dispatcher runs "Send to Kindle" deliveries off the request path: Enqueue
// records a queued row and hands the job to a small worker pool, which reads
// the EPUB, sends it, and records the terminal status. A single in-process
// queue is plenty for Lyceum's personal scale.
//
// Shutdown is race-free: Close flips closed under mu (so no further Enqueue
// touches the channel), waits for in-flight senders to drain, then closes the
// job channel and waits for the workers. Sends are bounded — workers cap each
// delivery at the per-send timeout — so a full buffer applies backpressure
// rather than deadlocking.
type Dispatcher struct {
	store   dispatcherStore
	sender  BookSender
	jobs    chan deliveryJob
	timeout time.Duration

	workers sync.WaitGroup
	senders sync.WaitGroup
	mu      sync.Mutex
	closed  bool
}

// NewDispatcher builds and starts a Dispatcher with the given worker count and
// per-send timeout. Call Close to drain and stop it.
func NewDispatcher(st dispatcherStore, sender BookSender, workers int, timeout time.Duration) *Dispatcher {
	if workers <= 0 {
		workers = 2
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	d := &Dispatcher{
		store:   st,
		sender:  sender,
		jobs:    make(chan deliveryJob, 256),
		timeout: timeout,
	}
	for i := 0; i < workers; i++ {
		d.workers.Add(1)
		go d.worker()
	}
	return d
}

// Enqueue records a queued delivery for book to toAddr and schedules it. The
// queued row is returned; the actual send happens asynchronously. After Close
// the delivery is recorded as failed instead of scheduled.
func (d *Dispatcher) Enqueue(ctx context.Context, book store.Book, toAddr string) (store.Delivery, error) {
	rec, err := d.store.CreateDelivery(ctx, book.ID, toAddr)
	if err != nil {
		return store.Delivery{}, err
	}

	// Register as an in-flight sender under the lock so Close cannot close the
	// channel between our check and our send.
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		if updated, uerr := d.store.UpdateDeliveryStatus(ctx, rec.ID, store.DeliveryFailed, "dispatcher shut down"); uerr == nil {
			rec = updated
		} else {
			log.Printf("api: delivery %d mark failed on shutdown: %v", rec.ID, uerr)
		}
		return rec, nil
	}
	d.senders.Add(1)
	d.mu.Unlock()
	defer d.senders.Done()

	d.jobs <- deliveryJob{
		deliveryID: rec.ID,
		toAddr:     toAddr,
		title:      book.Title,
		filename:   fmt.Sprintf("book-%d.epub", book.ID),
		filePath:   book.FilePath,
	}
	return rec, nil
}

// Close stops accepting work and waits for in-flight deliveries to finish.
func (d *Dispatcher) Close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	d.mu.Unlock()

	d.senders.Wait() // no producer is mid-send past this point
	close(d.jobs)
	d.workers.Wait()
}

func (d *Dispatcher) worker() {
	defer d.workers.Done()
	for job := range d.jobs {
		d.run(job)
	}
}

// run executes a single job: read the EPUB, send it, and persist the outcome.
// It uses a fresh background context (the originating request is long gone)
// bounded by the dispatcher timeout.
func (d *Dispatcher) run(job deliveryJob) {
	ctx, cancel := context.WithTimeout(context.Background(), d.timeout)
	defer cancel()

	fail := func(stage string, err error) {
		if _, uerr := d.store.UpdateDeliveryStatus(ctx, job.deliveryID, store.DeliveryFailed, stage+": "+err.Error()); uerr != nil {
			log.Printf("api: delivery %d mark failed: %v", job.deliveryID, uerr)
		}
	}

	content, err := os.ReadFile(job.filePath)
	if err != nil {
		fail("read epub", err)
		return
	}
	if err := d.sender.SendBook(ctx, job.toAddr, delivery.Book{
		Title:    job.title,
		Filename: job.filename,
		Content:  content,
	}); err != nil {
		fail("send", err)
		return
	}
	if _, err := d.store.UpdateDeliveryStatus(ctx, job.deliveryID, store.DeliverySent, ""); err != nil {
		log.Printf("api: delivery %d mark sent: %v", job.deliveryID, err)
	}
}

// deliveryConfig captures the upload-hook policy installed via WithDeliveries.
type deliveryConfig struct {
	dispatcher *Dispatcher
	kindleAddr string // global default recipient
	autoSend   bool   // deliver every new upload automatically
}

// WithDeliveries installs the delivery dispatcher plus the global Kindle
// address and auto-send policy. Without it the send-to-kindle route reports the
// feature unconfigured and uploads trigger no delivery.
func WithDeliveries(dispatcher *Dispatcher, kindleAddr string, autoSend bool) Option {
	return func(a *API) {
		a.delivery = &deliveryConfig{
			dispatcher: dispatcher,
			kindleAddr: kindleAddr,
			autoSend:   autoSend,
		}
	}
}

// deliveryJSON is the wire shape of a delivery record.
type deliveryJSON struct {
	ID        int64     `json:"id"`
	BookID    int64     `json:"book_id"`
	ToAddr    string    `json:"to_addr"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toDeliveryJSON(d store.Delivery) deliveryJSON {
	return deliveryJSON{
		ID:        d.ID,
		BookID:    d.BookID,
		ToAddr:    d.ToAddr,
		Status:    d.Status,
		Error:     d.Error,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	}
}

// maybeAutoDeliver enqueues an automatic delivery for a freshly uploaded book
// when auto-send is configured. It is best-effort: a failure to enqueue is
// logged but never fails the upload.
func (a *API) maybeAutoDeliver(ctx context.Context, book store.Book) {
	if a.delivery == nil || a.delivery.dispatcher == nil {
		return
	}
	if !a.delivery.autoSend || a.delivery.kindleAddr == "" {
		return
	}
	if _, err := a.delivery.dispatcher.Enqueue(ctx, book, a.delivery.kindleAddr); err != nil {
		log.Printf("api: auto-deliver book %d: %v", book.ID, err)
	}
}

// sendRequest is the optional body of POST /books/{id}/send-to-kindle.
type sendRequest struct {
	ToAddr string `json:"to_addr"`
}

// handleSendToKindle manually enqueues a delivery for a book. The recipient
// comes from the request body, falling back to the configured global Kindle
// address. Responds 202 with the queued delivery record.
func (a *API) handleSendToKindle(w http.ResponseWriter, r *http.Request) {
	if a.delivery == nil || a.delivery.dispatcher == nil {
		http.Error(w, "delivery is not configured", http.StatusServiceUnavailable)
		return
	}
	b, ok := a.lookupBook(w, r)
	if !ok {
		return
	}

	var req sendRequest
	// The body is optional; tolerate an empty one.
	if r.Body != nil && r.ContentLength != 0 {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
	}
	toAddr := req.ToAddr
	if toAddr == "" {
		toAddr = a.delivery.kindleAddr
	}
	if toAddr == "" {
		http.Error(w, "no recipient: supply to_addr or configure a Kindle address", http.StatusBadRequest)
		return
	}

	rec, err := a.delivery.dispatcher.Enqueue(r.Context(), b, toAddr)
	if err != nil {
		serverError(w, "enqueue delivery", err)
		return
	}
	writeJSON(w, http.StatusAccepted, toDeliveryJSON(rec))
}

// handleListDeliveries returns a book's delivery history, most recent first.
func (a *API) handleListDeliveries(w http.ResponseWriter, r *http.Request) {
	b, ok := a.lookupBook(w, r)
	if !ok {
		return
	}
	ds, err := a.store.ListDeliveriesByBook(r.Context(), b.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		serverError(w, "list deliveries", err)
		return
	}
	out := make([]deliveryJSON, 0, len(ds))
	for _, d := range ds {
		out = append(out, toDeliveryJSON(d))
	}
	writeJSON(w, http.StatusOK, out)
}
