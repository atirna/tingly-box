package db

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/tingly-dev/tingly-box/internal/loadbalance"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

const defaultServiceTimeWindow = 300

// ServiceStatsRecord is the GORM model for persisting service statistics
type ServiceStatsRecord struct {
	// Composite primary key: provider + model (stats are global, not per-rule)
	Provider             string    `gorm:"primaryKey;column:provider"`
	Model                string    `gorm:"primaryKey;column:model"`
	ServiceID            string    `gorm:"column:service_id"`
	RequestCount         int64     `gorm:"column:request_count"`
	LastUsed             time.Time `gorm:"column:last_used"`
	WindowStart          time.Time `gorm:"column:window_start"`
	WindowRequestCount   int64     `gorm:"column:window_request_count"`
	WindowTokensConsumed int64     `gorm:"column:window_tokens_consumed"`
	WindowInputTokens    int64     `gorm:"column:window_input_tokens"`
	WindowOutputTokens   int64     `gorm:"column:window_output_tokens"`
	TimeWindow           int       `gorm:"column:time_window"`
}

// TableName specifies the table name for GORM
func (ServiceStatsRecord) TableName() string {
	return "service_stats"
}

// StatsStore persists service usage statistics in SQLite using GORM.
type StatsStore struct {
	storeConn
	mu sync.Mutex

	// pending holds stats snapshots the outcome writer has accepted but not
	// yet committed, keyed by ServiceKey. Rows here are cumulative snapshots,
	// so a newer snapshot simply replaces an older one -- coalescing at the
	// source instead of queueing every snapshot and deduping at flush time.
	//
	// pendingMu is deliberately separate from mu: markPending runs on the
	// request path and must never wait on mu, which flush holds for the
	// length of a SQLite transaction. Lock order is mu -> pendingMu; nothing
	// acquires them the other way round.
	//
	// Keeping the buffer on the store (rather than inside the writer) is what
	// makes it visible to every reader and mutator of service_stats:
	// ClearAll/ClearService drop pending rows, and HydrateRules/Get read them
	// over the table. A buffer the store could not see is what let a cleared
	// stat come back a second later.
	pendingMu sync.Mutex
	pending   map[string]*ServiceStatsRecord
}

// NewStatsStore creates or loads a stats store over its own connection to
// the shared tingly.db.
func NewStatsStore(baseDir string) (*StatsStore, error) {
	db, err := openTinglyDB(baseDir)
	if err != nil {
		return nil, fmt.Errorf("stats store: %w", err)
	}
	return newStatsStore(ownedConn(db))
}

// newStatsStore finishes setting up a StatsStore (migrate) over an
// already-open connection, shared by NewStatsStore and
// StoreManager.initStatsStore.
func newStatsStore(conn storeConn) (*StatsStore, error) {
	if err := conn.db.AutoMigrate(&ServiceStatsRecord{}); err != nil {
		return nil, fmt.Errorf("failed to migrate stats database: %w", err)
	}
	return &StatsStore{storeConn: conn}, nil
}

// ServiceKey builds a unique key for a provider/model combination.
func (ss *StatsStore) ServiceKey(provider, model string) string {
	return serviceKey(provider, model)
}

func serviceKey(provider, model string) string {
	return provider + ":" + model
}

// markPending records a cumulative snapshot for later commit, replacing any
// snapshot still pending for the same service. Runs on the request path, so
// it takes only pendingMu and never blocks on a flush in progress.
func (ss *StatsStore) markPending(record *ServiceStatsRecord) {
	if ss == nil || record == nil {
		return
	}
	ss.pendingMu.Lock()
	defer ss.pendingMu.Unlock()
	if ss.pending == nil {
		ss.pending = make(map[string]*ServiceStatsRecord)
	}
	ss.pending[serviceKey(record.Provider, record.Model)] = record
}

// takePendingLocked removes and returns the pending snapshots. Callers must
// hold ss.mu and must commit the returned rows before releasing it: ClearAll
// takes ss.mu too, so holding it across take-and-commit is what stops a
// snapshot from landing after a clear.
func (ss *StatsStore) takePendingLocked() []*ServiceStatsRecord {
	ss.pendingMu.Lock()
	defer ss.pendingMu.Unlock()

	if len(ss.pending) == 0 {
		return nil
	}
	records := make([]*ServiceStatsRecord, 0, len(ss.pending))
	for _, record := range ss.pending {
		records = append(records, record)
	}
	clear(ss.pending)
	return records
}

// pendingLocked returns the snapshot for a service, if one is buffered.
// Callers must hold ss.mu.
func (ss *StatsStore) pendingLocked(key string) (*ServiceStatsRecord, bool) {
	ss.pendingMu.Lock()
	defer ss.pendingMu.Unlock()
	record, ok := ss.pending[key]
	return record, ok
}

// Get returns stats for a specific provider/model combination, preferring a
// snapshot still pending commit over the (older) persisted row.
func (ss *StatsStore) Get(provider, model string) (loadbalance.ServiceStats, bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if pending, ok := ss.pendingLocked(serviceKey(provider, model)); ok {
		return pending.toServiceStats(), true
	}

	var record ServiceStatsRecord
	err := ss.db.Where("provider = ? AND model = ?", provider, model).
		First(&record).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return loadbalance.ServiceStats{}, false
	}
	if err != nil {
		return loadbalance.ServiceStats{}, false
	}

	return record.toServiceStats(), true
}

// UpdateFromService stores the current stats from a service into the store.
func (ss *StatsStore) UpdateFromService(service *loadbalance.Service) error {
	record := buildStatsRecordFromService(service)
	if record == nil {
		return nil
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	return ss.db.Save(record).Error
}

// buildStatsRecordFromService builds the ServiceStatsRecord UpdateFromService
// persists, without touching the database -- split out so
// RecordRequestOutcome can save it in the same transaction as a UsageStore
// write. Still mutates service.Stats via InitializeStats/GetStats.
func buildStatsRecordFromService(service *loadbalance.Service) *ServiceStatsRecord {
	if service == nil {
		return nil
	}

	service.InitializeStats()
	stat := service.Stats.GetStats()

	record := &ServiceStatsRecord{
		Provider:             service.Provider,
		Model:                service.Model,
		ServiceID:            stat.ServiceID,
		RequestCount:         stat.RequestCount,
		LastUsed:             stat.LastUsed,
		WindowStart:          stat.WindowStart,
		WindowRequestCount:   stat.WindowRequestCount,
		WindowTokensConsumed: stat.WindowTokensConsumed,
		WindowInputTokens:    stat.WindowInputTokens,
		WindowOutputTokens:   stat.WindowOutputTokens,
		TimeWindow:           stat.TimeWindow,
	}

	// Normalize time window if needed
	if record.TimeWindow == 0 {
		if service.TimeWindow > 0 {
			record.TimeWindow = service.TimeWindow
		} else {
			record.TimeWindow = defaultServiceTimeWindow
		}
	}
	if record.ServiceID == "" {
		record.ServiceID = service.ServiceID()
	}
	if record.WindowStart.IsZero() {
		record.WindowStart = time.Now()
	}

	return record
}

// HydrateRules injects stored stats into the provided rules and initializes missing entries.
func (ss *StatsStore) HydrateRules(rules []typ.Rule) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	var records []ServiceStatsRecord
	if err := ss.db.Find(&records).Error; err != nil {
		return err
	}

	// Build lookup map by provider:model, then overlay snapshots still
	// pending commit -- those are newer than the persisted rows, and
	// re-seeding a rule's in-memory counters from the older table value is
	// what made a hot-reload look like traffic had been lost.
	statsMap := make(map[string]*ServiceStatsRecord)
	for i := range records {
		record := &records[i]
		statsMap[serviceKey(record.Provider, record.Model)] = record
	}
	ss.pendingMu.Lock()
	for key, record := range ss.pending {
		statsMap[key] = record
	}
	ss.pendingMu.Unlock()

	// Collect rows for services with no stored stats and insert them in one
	// batch at the end. HydrateRules runs on every config load/hot-reload,
	// and the previous per-row Create inside the loop was an N+1 insert over
	// rules x services on first boot or after adding a rule.
	var missing []*ServiceStatsRecord
	for i := range rules {
		rule := &rules[i]
		for j := range rule.Services {
			service := rule.Services[j]
			key := serviceKey(service.Provider, service.Model)

			if record, ok := statsMap[key]; ok {
				service.Stats = record.toServiceStats()
			} else if record := buildStatsRecordFromService(service); record != nil {
				// buildStatsRecordFromService initializes service.Stats and
				// applies the same defaults this branch used to duplicate
				// inline. Register the row in statsMap so other services
				// with the same provider:model reuse it.
				statsMap[key] = record
				missing = append(missing, record)
			}
		}
	}

	if len(missing) > 0 {
		if err := ss.db.CreateInBatches(missing, 100).Error; err != nil {
			return err
		}
	}

	return nil
}

// ClearAll removes all persisted stats.
func (ss *StatsStore) ClearAll() error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	// Drop buffered snapshots under ss.mu, which flush also holds across
	// take-and-commit: a snapshot taken before the clear would otherwise be
	// committed afterwards, putting the counters the user just cleared
	// straight back.
	ss.pendingMu.Lock()
	clear(ss.pending)
	ss.pendingMu.Unlock()

	return ss.db.Exec("DELETE FROM service_stats").Error
}

// ClearService removes persisted stats for a single provider:model. No error
// if no rows matched (the service simply had no recorded stats).
func (ss *StatsStore) ClearService(provider, model string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	// See ClearAll: a buffered snapshot would otherwise resurrect the row.
	ss.pendingMu.Lock()
	delete(ss.pending, serviceKey(provider, model))
	ss.pendingMu.Unlock()

	return ss.db.Where("provider = ? AND model = ?", provider, model).
		Delete(&ServiceStatsRecord{}).Error
}

// toServiceStats converts a ServiceStatsRecord to ServiceStats.
func (r *ServiceStatsRecord) toServiceStats() loadbalance.ServiceStats {
	return loadbalance.ServiceStats{
		ServiceID:            r.ServiceID,
		RequestCount:         r.RequestCount,
		LastUsed:             r.LastUsed,
		WindowStart:          r.WindowStart,
		WindowRequestCount:   r.WindowRequestCount,
		WindowTokensConsumed: r.WindowTokensConsumed,
		WindowInputTokens:    r.WindowInputTokens,
		WindowOutputTokens:   r.WindowOutputTokens,
		TimeWindow:           r.TimeWindow,
	}
}
