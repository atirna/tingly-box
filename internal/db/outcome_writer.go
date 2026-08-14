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
	// usageQueueSize bounds the queue of usage rows awaiting commit. On
	// overflow RecordOutcome inserts the row itself rather than dropping it.
	usageQueueSize = 1024
	// outcomeMaxBatch is the largest number of usage rows committed in one
	// transaction.
	outcomeMaxBatch = 128
	// outcomeFlushInterval is the longest a buffered outcome waits before
	// being persisted. This is also the crash-loss window: outcomes buffered
	// but not yet flushed are lost if the process dies — the accepted trade
	// for taking two per-request statements off the request path
	// (.design/db.md tracks the decision).
	outcomeFlushInterval = time.Second
)

// outcomeWriter moves the per-request stats and usage writes off the request
// path, committing many requests' rows per SQLite transaction instead of one
// transaction per request (see RecordRequestOutcome for the synchronous
// shape it replaces).
//
// The two record kinds are buffered differently because their semantics are
// opposite:
//
//   - A stats row is a cumulative snapshot keyed by provider:model, so a
//     newer snapshot fully supersedes an older one. These coalesce into
//     StatsStore.pending — enqueueing is O(1), never fails, and never needs
//     ordering. Keeping them under the stats lock is also what lets
//     ClearAll/HydrateRules stay consistent with what is still in flight.
//   - A usage row is an append-only audit record that must not be lost or
//     merged, so those go through a bounded queue.
type outcomeWriter struct {
	stats *StatsStore
	usage *UsageStore

	usageCh chan *UsageRecord
	done    chan struct{}
	wg      sync.WaitGroup

	// mu is held across the send on usageCh so a send can never race past
	// close's final drain; closed is the flag it guards.
	mu     sync.Mutex
	closed bool
}

func newOutcomeWriter(stats *StatsStore, usage *UsageStore) *outcomeWriter {
	w := &outcomeWriter{
		stats:   stats,
		usage:   usage,
		usageCh: make(chan *UsageRecord, usageQueueSize),
		done:    make(chan struct{}),
	}
	w.wg.Add(1)
	go w.run()
	return w
}

// enqueue hands one request's rows to the flusher without blocking. It
// returns the usage row the writer could not take — nil when it took it (or
// when there was none) — and whether the writer is still alive. A closed
// writer takes nothing at all.
func (w *outcomeWriter) enqueue(stats *ServiceStatsRecord, usage *UsageRecord) (rejected *UsageRecord, alive bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return usage, false
	}

	w.stats.markPending(stats)
	if usage == nil {
		return nil, true
	}
	select {
	case w.usageCh <- usage:
		return nil, true
	default:
		return usage, true
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

// run is the flusher: it buffers usage rows and commits them — together with
// whatever stats snapshots have accumulated — when the batch fills or the
// flush interval elapses, whichever comes first.
func (w *outcomeWriter) run() {
	defer w.wg.Done()

	// A timer armed only while rows are buffered, rather than a ticker: an
	// idle gateway then costs no periodic wakeups at all.
	timer := time.NewTimer(outcomeFlushInterval)
	timer.Stop()
	defer timer.Stop()

	batch := make([]*UsageRecord, 0, outcomeMaxBatch)
	flushBatch := func() {
		w.flush(batch)
		clear(batch) // drop references the reslice would otherwise pin
		batch = batch[:0]
		timer.Stop()
	}

	for {
		select {
		case record := <-w.usageCh:
			if len(batch) == 0 {
				timer.Reset(outcomeFlushInterval)
			}
			batch = append(batch, record)
			if len(batch) >= outcomeMaxBatch {
				flushBatch()
			}
		case <-timer.C:
			flushBatch()
		case <-w.done:
			// enqueue holds w.mu across its send and close takes w.mu before
			// closing done, so no further sends are possible and the queue
			// length is stable here.
			for len(w.usageCh) > 0 {
				batch = append(batch, <-w.usageCh)
			}
			w.flush(batch)
			return
		}
	}
}

// flush commits the buffered usage rows plus every pending stats snapshot.
// A stats-only flush is normal: snapshots accumulate even when no usage row
// is queued.
func (w *outcomeWriter) flush(usageRecords []*UsageRecord) {
	w.stats.mu.Lock()
	defer w.stats.mu.Unlock()
	w.usage.mu.Lock()
	defer w.usage.mu.Unlock()

	// Taking the snapshots and committing them under one acquisition of
	// stats.mu is what keeps ClearAll atomic against the flusher.
	statsRecords := w.stats.takePendingLocked()
	if err := commitOutcomesLocked(w.stats, w.usage, statsRecords, usageRecords); err != nil {
		// Same contract as the synchronous path, whose error
		// persistRequestOutcome also discards: these rows are telemetry, not
		// billing — log and move on.
		logrus.WithError(err).Warnf("outcome writer: failed to flush %d stats and %d usage rows",
			len(statsRecords), len(usageRecords))
	}
}

// commitOutcomesLocked writes already-built stats snapshots and usage rows in
// a single transaction. It is the one place that encodes both the stats→usage
// lock order and the assumption that the two stores share one *gorm.DB (always
// true via StoreManager). Callers must hold both stores' mutexes.
func commitOutcomesLocked(stats *StatsStore, usage *UsageStore, statsRecords []*ServiceStatsRecord, usageRecords []*UsageRecord) error {
	if len(statsRecords) == 0 && len(usageRecords) == 0 {
		return nil
	}
	return stats.db.Transaction(func(tx *gorm.DB) error {
		for _, record := range statsRecords {
			if err := tx.Save(record).Error; err != nil {
				return err
			}
		}
		if len(usageRecords) > 0 {
			if err := tx.CreateInBatches(usageRecords, len(usageRecords)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// RecordOutcome persists a request's service stats and usage row through the
// batched writer, falling back to the synchronous single-transaction
// RecordRequestOutcome when the writer is unavailable. This is the production
// entry point for the per-request hot path; see .design/db.md.
func (sm *StoreManager) RecordOutcome(service *loadbalance.Service, usage *UsageRecord) error {
	sm.mu.RLock()
	writer := sm.outcomeWriter
	stats := sm.statsStore
	usageStore := sm.usageStore
	sm.mu.RUnlock()

	if writer == nil || stats == nil || usageStore == nil {
		return RecordRequestOutcome(stats, usageStore, service, usage)
	}

	// Build both records here, on the request goroutine: the stats record
	// snapshots the service's live counters and the usage row's timestamp is
	// the request's completion time. Only the SQLite write is deferred.
	statsRecord := buildStatsRecordFromService(service)
	if usage != nil {
		prepareUsageRecord(usage)
	}
	if statsRecord == nil && usage == nil {
		return nil
	}

	rejected, alive := writer.enqueue(statsRecord, usage)
	if !alive {
		// Writer closed, so nothing of ours sits behind anything buffered:
		// commit both rows synchronously, reusing what we already built.
		stats.mu.Lock()
		defer stats.mu.Unlock()
		usageStore.mu.Lock()
		defer usageStore.mu.Unlock()
		return commitOutcomesLocked(stats, usageStore, oneOf(statsRecord), oneOf(rejected))
	}
	if rejected != nil {
		// Usage queue saturated. The stats snapshot was still coalesced, and
		// a usage row is an independent INSERT, so writing just this one row
		// is trivially safe.
		return usageStore.RecordUsage(rejected)
	}
	return nil
}

// oneOf wraps a possibly-nil record as a slice for commitOutcomesLocked,
// which takes batches.
func oneOf[T any](record *T) []*T {
	if record == nil {
		return nil
	}
	return []*T{record}
}
