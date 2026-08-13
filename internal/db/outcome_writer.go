package db

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/tingly-dev/tingly-box/internal/loadbalance"
)

// Outcome-writer tuning. Values are package constants rather than
// per-instance options because there is exactly one writer per process
// (owned by StoreManager) and no call site has ever needed different
// numbers; promote them to StoreManagerConfig if that changes.
const (
	// outcomeQueueSize bounds the enqueue channel. When the queue is full,
	// RecordOutcome degrades to the old synchronous single-transaction write
	// instead of dropping data or blocking the request goroutine on the
	// flusher.
	outcomeQueueSize = 1024
	// outcomeMaxBatch is the largest number of outcomes committed in one
	// transaction.
	outcomeMaxBatch = 128
	// outcomeFlushInterval is the longest a buffered outcome waits before
	// being persisted. This is also the crash-loss window: outcomes buffered
	// but not yet flushed are lost if the process dies — the accepted trade
	// for taking two per-request statements off the request path
	// (.design/db.md tracks the decision).
	outcomeFlushInterval = time.Second
)

// queuedOutcome is one request's already-built records. Records are built at
// enqueue time, on the request goroutine — buildStatsRecordFromService reads
// the service's live stats, so deferring the build would snapshot the wrong
// moment. Only the SQLite write is deferred.
type queuedOutcome struct {
	stats *ServiceStatsRecord
	usage *UsageRecord
}

// outcomeWriter batches per-request stats+usage writes into fewer, larger
// SQLite transactions. One LLM request used to cost one transaction with two
// statements (see RecordRequestOutcome); under load the writer coalesces up
// to outcomeMaxBatch requests into a single transaction, and stats rows —
// cumulative snapshots keyed by provider:model — dedupe to just the newest
// snapshot per service.
type outcomeWriter struct {
	stats *StatsStore
	usage *UsageStore

	ch   chan queuedOutcome
	done chan struct{}
	wg   sync.WaitGroup

	mu     sync.Mutex // guards closed
	closed bool
}

func newOutcomeWriter(stats *StatsStore, usage *UsageStore) *outcomeWriter {
	w := &outcomeWriter{
		stats: stats,
		usage: usage,
		ch:    make(chan queuedOutcome, outcomeQueueSize),
		done:  make(chan struct{}),
	}
	w.wg.Add(1)
	go w.run()
	return w
}

// enqueueResult tells RecordOutcome which fallback the caller owes, because
// "queue full" and "writer gone" need different handling: only the former
// leaves older outcomes buffered behind the caller's.
type enqueueResult int

const (
	enqueued    enqueueResult = iota // handed to the flusher
	queueFull                        // writer alive, but the queue is saturated
	writerClosed                     // writer stopped; nothing is buffered
)

// enqueue hands one outcome to the flusher. It never blocks.
func (w *outcomeWriter) enqueue(o queuedOutcome) enqueueResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return writerClosed
	}
	select {
	case w.ch <- o:
		return enqueued
	default:
		return queueFull
	}
}

// close stops the writer, flushes everything still buffered, and waits for
// the flusher goroutine to exit. Idempotent.
func (w *outcomeWriter) close() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	w.mu.Unlock()

	close(w.done)
	w.wg.Wait()
}

// run is the flusher: it buffers queued outcomes and commits them when the
// batch fills or the flush interval elapses, whichever comes first.
func (w *outcomeWriter) run() {
	defer w.wg.Done()

	ticker := time.NewTicker(outcomeFlushInterval)
	defer ticker.Stop()

	batch := make([]queuedOutcome, 0, outcomeMaxBatch)
	for {
		select {
		case o := <-w.ch:
			batch = append(batch, o)
			if len(batch) >= outcomeMaxBatch {
				w.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				w.flush(batch)
				batch = batch[:0]
			}
		case <-w.done:
			// Drain whatever raced in before close, then flush once.
			for {
				select {
				case o := <-w.ch:
					batch = append(batch, o)
				default:
					if len(batch) > 0 {
						w.flush(batch)
					}
					return
				}
			}
		}
	}
}

// flush commits one batch in a single transaction, holding both store
// mutexes exactly like RecordRequestOutcome so batched writes serialize
// correctly against each store's own methods.
func (w *outcomeWriter) flush(batch []queuedOutcome) {
	// Stats rows are cumulative snapshots keyed by (provider, model): within
	// a batch only the newest snapshot per key needs saving. Usage rows are
	// per-request audit records and are all kept.
	statsByKey := make(map[string]*ServiceStatsRecord)
	statsOrder := make([]string, 0, len(batch))
	usageRows := make([]*UsageRecord, 0, len(batch))
	for _, o := range batch {
		if o.stats != nil {
			key := o.stats.Provider + ":" + o.stats.Model
			if _, seen := statsByKey[key]; !seen {
				statsOrder = append(statsOrder, key)
			}
			statsByKey[key] = o.stats
		}
		if o.usage != nil {
			usageRows = append(usageRows, o.usage)
		}
	}
	if len(statsByKey) == 0 && len(usageRows) == 0 {
		return
	}

	w.stats.mu.Lock()
	defer w.stats.mu.Unlock()
	w.usage.mu.Lock()
	defer w.usage.mu.Unlock()

	err := w.stats.db.Transaction(func(tx *gorm.DB) error {
		for _, key := range statsOrder {
			if err := tx.Save(statsByKey[key]).Error; err != nil {
				return err
			}
		}
		if len(usageRows) > 0 {
			if err := tx.CreateInBatches(usageRows, len(usageRows)).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		// Same contract as the previous synchronous path, where
		// persistRequestOutcome discarded the error: stats/usage rows are
		// telemetry, not billing — log and move on.
		logrus.WithError(err).Warnf("outcome writer: failed to flush %d outcomes", len(batch))
	}
}

// RecordOutcome persists a request's service stats and usage row through the
// batched writer, falling back to the synchronous single-transaction
// RecordRequestOutcome when the writer is unavailable (closed, queue full,
// or stores missing). This is the production entry point for the per-request
// hot path; see .design/db.md for the batching decision.
func (sm *StoreManager) RecordOutcome(service *loadbalance.Service, usage *UsageRecord) error {
	sm.mu.RLock()
	writer := sm.outcomeWriter
	stats := sm.statsStore
	usageStore := sm.usageStore
	sm.mu.RUnlock()

	if writer != nil && stats != nil && usageStore != nil {
		// Build records now (on the request goroutine): the stats record
		// snapshots the service's live counters, and the usage record's
		// timestamp should be the request's completion time, not flush time.
		o := queuedOutcome{stats: buildStatsRecordFromService(service)}
		if usage != nil {
			prepareUsageRecord(usage)
			o.usage = usage
		}
		if o.stats == nil && o.usage == nil {
			return nil
		}
		switch writer.enqueue(o) {
		case enqueued:
			return nil
		case queueFull:
			// Older outcomes are still buffered ahead of this one. Committing
			// this stats snapshot now would let the flusher overwrite it with
			// those older cumulative counters, walking service_stats
			// backwards. Stats snapshots are cumulative, so dropping this one
			// is safe -- the service's next outcome supersedes it, exactly
			// like the per-batch dedupe in flush. The usage row is an
			// append-only audit record, so it still has to be written.
			if o.usage != nil {
				return usageStore.RecordUsage(o.usage)
			}
			return nil
		}
		// writerClosed: nothing is buffered (close drains and flushes), so
		// the synchronous path below is safe and complete.
	}

	return RecordRequestOutcome(stats, usageStore, service, usage)
}
