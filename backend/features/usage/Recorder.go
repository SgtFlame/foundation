package usage

import (
	"context"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/usage/settings"
	"gorm.io/gorm"
)

var log = logging.In("usage")

// Prices returns the model catalog to price records with.
type Prices func(ctx context.Context) (aisettings.Models, error)

// CaptureSettings returns the usage.capture settings.
type CaptureSettings func(ctx context.Context) (usagesettings.Capture, error)

// Recorder prices records and writes them in batches from one goroutine, so
// recording never blocks a reply. Close flushes what is queued.
type Recorder struct {
	db      *gorm.DB
	prices  Prices
	events  *Events
	capture CaptureSettings

	queue     chan entry
	done      chan struct{}
	closeOnce sync.Once
}

// entry is a queued record and, when it was captured, its text.
type entry struct {
	record  Record
	capture *Capture
}

const (
	queueSize  = 1024
	batchSize  = 100
	flushEvery = time.Second
	purgeEvery = 10 * time.Minute
)

// NewRecorder starts the writer. prices, events and capture may be nil; with
// no capture settings, nothing is captured.
func NewRecorder(gdb *gorm.DB, prices Prices, events *Events, capture CaptureSettings) *Recorder {
	r := &Recorder{db: gdb, prices: prices, events: events, capture: capture, queue: make(chan entry, queueSize), done: make(chan struct{})}
	go r.run()
	return r
}

// Record queues rec, priced now. When the queue is full the record is
// dropped with a warning rather than delaying the caller.
func (r *Recorder) Record(ctx context.Context, rec Record, tokens aisettings.Tokens) {
	r.record(ctx, rec, tokens, nil)
}

// Capturing reports whether calls' text is captured now.
func (r *Recorder) Capturing(ctx context.Context) bool {
	if r == nil || r.capture == nil {
		return false
	}
	settings, err := r.capture(ctx)
	if err != nil {
		log.Error(ctx, err, "cannot read the capture settings; nothing is captured")
		return false
	}
	return settings.Enabled
}

// RecordCaptured queues rec with the text of its call.
func (r *Recorder) RecordCaptured(ctx context.Context, rec Record, tokens aisettings.Tokens, capture Capture) {
	r.record(ctx, rec, tokens, &capture)
}

func (r *Recorder) record(ctx context.Context, rec Record, tokens aisettings.Tokens, capture *Capture) {
	if r == nil {
		return
	}
	r.price(ctx, &rec, tokens)
	if capture != nil {
		if rec.ID == 0 {
			rec.ID = features.NewTSID()
		}
		capture.RecordID, capture.TenantID = rec.ID, rec.TenantID
		rec.Captured = true
	}
	select {
	case r.queue <- entry{rec, capture}:
	default:
		log.With("feature", rec.Feature, "model", rec.ModelName).Warn(ctx, "usage queue full; a record was dropped")
	}
}

func (r *Recorder) price(ctx context.Context, rec *Record, tokens aisettings.Tokens) {
	if r.prices == nil {
		return
	}
	catalog, err := r.prices(ctx)
	if err != nil {
		log.Error(ctx, err, "cannot read the model catalog; the call is recorded unpriced")
		return
	}
	model, ok := catalog.Find(rec.Provider, rec.ModelName)
	if !ok {
		return
	}
	rec.CostMicros = model.Pricing.CostMicros(tokens)
	rec.Currency = model.Pricing.Currency
	if rec.Currency == "" {
		rec.Currency = "USD"
	}
}

// Close stops accepting records and waits until the queued ones are written.
func (r *Recorder) Close() {
	r.closeOnce.Do(func() { close(r.queue) })
	<-r.done
}

func (r *Recorder) run() {
	defer close(r.done)
	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()
	purge := time.NewTicker(purgeEvery)
	defer purge.Stop()
	r.purge()
	batch := make([]entry, 0, batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		r.write(batch)
		batch = make([]entry, 0, batchSize)
	}
	for {
		select {
		case e, ok := <-r.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, e)
			if len(batch) == batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-purge.C:
			r.purge()
		}
	}
}

// write stores a batch of records with their captures, and publishes the
// records once they are stored.
func (r *Recorder) write(batch []entry) {
	records := make([]Record, len(batch))
	var captures []Capture
	for i, e := range batch {
		records[i] = e.record
		if e.capture != nil {
			captures = append(captures, *e.capture)
		}
	}
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&records).Error; err != nil {
			return err
		}
		if len(captures) > 0 {
			return tx.Create(&captures).Error
		}
		return nil
	})
	if err != nil {
		log.With("records", len(records)).Error(context.Background(), err, "cannot store %d usage records", len(records))
		return
	}
	for _, rec := range records {
		r.events.Publish(rec)
	}
}

// purge deletes the captures older than the retention. Turning capture off
// stops new captures; the existing ones still expire on time.
func (r *Recorder) purge() {
	if r.capture == nil {
		return
	}
	ctx := context.Background()
	settings, err := r.capture(ctx)
	if err != nil {
		log.Error(ctx, err, "cannot read the capture settings; captures are not purged")
		return
	}
	res := r.db.Where("created_at < ?", time.Now().Add(-settings.Retention())).Delete(&Capture{})
	if res.Error != nil {
		log.Error(ctx, res.Error, "cannot purge expired captures")
	} else if res.RowsAffected > 0 {
		log.With("captures", res.RowsAffected).Info(ctx, "purged %d expired captures", res.RowsAffected)
	}
}
