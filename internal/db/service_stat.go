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
	return fmt.Sprintf("%s:%s", provider, model)
}

// Get returns stats for a specific provider/model combination.
func (ss *StatsStore) Get(provider, model string) (loadbalance.ServiceStats, bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

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

// RecordUsage records usage for a service and persists the updated stats.
func (ss *StatsStore) RecordUsage(service *loadbalance.Service, inputTokens, outputTokens int) (loadbalance.ServiceStats, error) {
	if service == nil {
		return loadbalance.ServiceStats{}, nil
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	// Get or create record
	var record ServiceStatsRecord
	err := ss.db.Where("provider = ? AND model = ?", service.Provider, service.Model).
		First(&record).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Create new record
		record = ServiceStatsRecord{
			Provider:  service.Provider,
			Model:     service.Model,
			ServiceID: service.ServiceID(),
			TimeWindow: func() int {
				if service.TimeWindow > 0 {
					return service.TimeWindow
				}
				return defaultServiceTimeWindow
			}(),
			WindowStart: time.Now(),
		}
	} else if err != nil {
		return loadbalance.ServiceStats{}, err
	}

	// Update stats
	now := time.Now()
	if now.Sub(record.WindowStart) >= time.Duration(record.TimeWindow)*time.Second {
		record.WindowStart = now
		record.WindowRequestCount = 0
		record.WindowTokensConsumed = 0
		record.WindowInputTokens = 0
		record.WindowOutputTokens = 0
	}

	record.RequestCount++
	record.WindowRequestCount++
	record.WindowInputTokens += int64(inputTokens)
	record.WindowOutputTokens += int64(outputTokens)
	record.WindowTokensConsumed += int64(inputTokens + outputTokens)
	record.LastUsed = now

	if err := ss.db.Save(&record).Error; err != nil {
		return loadbalance.ServiceStats{}, err
	}

	return record.toServiceStats(), nil
}

// HydrateRules injects stored stats into the provided rules and initializes missing entries.
func (ss *StatsStore) HydrateRules(rules []typ.Rule) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	var records []ServiceStatsRecord
	if err := ss.db.Find(&records).Error; err != nil {
		return err
	}

	// Build lookup map by provider:model
	statsMap := make(map[string]*ServiceStatsRecord)
	for i := range records {
		record := &records[i]
		key := ss.ServiceKey(record.Provider, record.Model)
		statsMap[key] = record
	}

	for i := range rules {
		rule := &rules[i]
		for j := range rule.Services {
			service := rule.Services[j]
			key := ss.ServiceKey(service.Provider, service.Model)

			if record, ok := statsMap[key]; ok {
				service.Stats = record.toServiceStats()
			} else {
				service.InitializeStats()
				statCopy := service.Stats.GetStats()
				record := &ServiceStatsRecord{
					Provider:             service.Provider,
					Model:                service.Model,
					ServiceID:            statCopy.ServiceID,
					RequestCount:         statCopy.RequestCount,
					LastUsed:             statCopy.LastUsed,
					WindowStart:          statCopy.WindowStart,
					WindowRequestCount:   statCopy.WindowRequestCount,
					WindowTokensConsumed: statCopy.WindowTokensConsumed,
					WindowInputTokens:    statCopy.WindowInputTokens,
					WindowOutputTokens:   statCopy.WindowOutputTokens,
					TimeWindow:           statCopy.TimeWindow,
				}
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
				if err := ss.db.Create(record).Error; err != nil {
					return err
				}
				// Add to statsMap so other services with same provider:model find it
				statsMap[key] = record
			}
		}
	}

	return nil
}

// ClearAll removes all persisted stats.
func (ss *StatsStore) ClearAll() error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	return ss.db.Exec("DELETE FROM service_stats").Error
}

// ClearService removes persisted stats for a single provider:model. No error
// if no rows matched (the service simply had no recorded stats).
func (ss *StatsStore) ClearService(provider, model string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

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
