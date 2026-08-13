package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
)

// Settings represents ImBot configuration (exported for use by remote_coder module)
type Settings struct {
	UUID          string            `json:"uuid,omitempty"`
	Name          string            `json:"name,omitempty"`
	Token         string            `json:"token,omitempty"` // Legacy: for backward compatibility
	Platform      string            `json:"platform"`
	AuthType      string            `json:"auth_type"`
	Auth          map[string]string `json:"auth"`
	ProxyURL      string            `json:"proxy_url,omitempty"`
	ChatIDLock    string            `json:"chat_id_lock,omitempty"` // Deprecated: retained for storage compatibility; not an authorization gate.
	BashAllowlist []string          `json:"bash_allowlist,omitempty"`
	DefaultCwd    string            `json:"default_cwd,omitempty"`   // Default working directory
	DefaultAgent  string            `json:"default_agent,omitempty"` // Default Agent UUID
	Enabled       bool              `json:"enabled"`
	// SmartGuide model configuration (required for @tb agent)
	SmartGuideProvider string `json:"smartguide_provider,omitempty"` // Provider UUID
	SmartGuideModel    string `json:"smartguide_model,omitempty"`    // Model identifier
	// RequirePairing enforces TOFU pairing for DMs. Nil = legacy/opt-in.
	RequirePairing *bool `json:"require_pairing,omitempty"`
	// Scenarios is the raw JSON-encoded list of hook scenarios this bot
	// serves. The notify module parses it into typed bindings; the
	// settings store keeps it opaque to avoid a cross-package dependency.
	Scenarios string    `json:"scenarios,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// ImBotSettingsStore persists ImBot settings in SQLite using GORM.
type ImBotSettingsStore struct {
	storeConn
	mu sync.Mutex
}

// NewImBotSettingsStore creates or loads an ImBot settings store over its
// own connection to the shared tingly.db.
func NewImBotSettingsStore(baseDir string) (*ImBotSettingsStore, error) {
	db, err := openTinglyDB(baseDir)
	if err != nil {
		return nil, fmt.Errorf("imbot settings store: %w", err)
	}
	return newImBotSettingsStore(ownedConn(db))
}

// newImBotSettingsStore finishes setting up an ImBotSettingsStore (migrate)
// over an already-open connection, shared by NewImBotSettingsStore and
// StoreManager.initImBotSettingsStore.
func newImBotSettingsStore(conn storeConn) (*ImBotSettingsStore, error) {
	if err := conn.db.AutoMigrate(&ImBotSettingsRecord{}); err != nil {
		return nil, fmt.Errorf("failed to migrate imbot settings database: %w", err)
	}
	return &ImBotSettingsStore{storeConn: conn}, nil
}

// ListSettings returns all ImBot configurations.
func (s *ImBotSettingsStore) ListSettings() ([]Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var records []ImBotSettingsRecord
	if err := s.db.Order("created_at DESC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to list settings: %w", err)
	}

	settings := make([]Settings, 0, len(records))
	for _, record := range records {
		setting, err := recordToSettings(record)
		if err != nil {
			return nil, err
		}
		settings = append(settings, setting)
	}

	return settings, nil
}

// ListEnabledSettings returns all enabled ImBot configurations.
func (s *ImBotSettingsStore) ListEnabledSettings() ([]Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var records []ImBotSettingsRecord
	if err := s.db.Where("enabled = ?", true).Order("created_at DESC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to list enabled settings: %w", err)
	}

	settings := make([]Settings, 0, len(records))
	for _, record := range records {
		setting, err := recordToSettings(record)
		if err != nil {
			return nil, err
		}
		settings = append(settings, setting)
	}

	return settings, nil
}

// GetSettingsByUUID returns a single ImBot configuration by UUID.
func (s *ImBotSettingsStore) GetSettingsByUUID(uuid string) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var record ImBotSettingsRecord
	if err := s.db.Where("bot_uuid = ?", uuid).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Settings{Auth: make(map[string]string)}, nil
		}
		return Settings{Auth: make(map[string]string)}, fmt.Errorf("failed to get settings by uuid: %w", err)
	}

	return recordToSettings(record)
}

// CreateSettings creates a new ImBot configuration.
func (s *ImBotSettingsStore) CreateSettings(settings Settings) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if settings.UUID == "" {
		settings.UUID = generateUUID()
	}

	now := time.Now()
	settings.CreatedAt = now
	settings.UpdatedAt = now

	// Convert auth map to JSON
	authConfigJSON := ""
	if len(settings.Auth) > 0 {
		if b, err := json.Marshal(settings.Auth); err == nil {
			authConfigJSON = string(b)
		}
	}

	// Convert bash allowlist to JSON
	allowlistJSON := ""
	if len(settings.BashAllowlist) > 0 {
		if b, err := json.Marshal(settings.BashAllowlist); err == nil {
			allowlistJSON = string(b)
		}
	}

	record := ImBotSettingsRecord{
		BotUUID:            settings.UUID,
		Name:               settings.Name,
		Platform:           settings.Platform,
		AuthType:           settings.AuthType,
		AuthConfig:         authConfigJSON,
		ProxyURL:           settings.ProxyURL,
		ChatIDLock:         settings.ChatIDLock,
		BashAllowlist:      allowlistJSON,
		DefaultCwd:         settings.DefaultCwd,
		DefaultAgent:       settings.DefaultAgent,
		Enabled:            settings.Enabled,
		SmartGuideProvider: settings.SmartGuideProvider,
		SmartGuideModel:    settings.SmartGuideModel,
		RequirePairing:     settings.RequirePairing,
		Scenarios:          settings.Scenarios,
		CreatedAt:          settings.CreatedAt,
		UpdatedAt:          settings.UpdatedAt,
	}

	if err := s.db.Create(&record).Error; err != nil {
		return Settings{Auth: make(map[string]string)}, fmt.Errorf("failed to create settings: %w", err)
	}

	return settings, nil
}

// UpdateSettings updates an existing ImBot configuration.
// Only updates fields that are non-zero/empty in the settings struct.
func (s *ImBotSettingsStore) UpdateSettings(uuid string, settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	settings.UpdatedAt = now

	// Build a map of only the fields to update
	// This allows partial updates - empty/zero values won't overwrite existing data
	updates := make(map[string]interface{})

	if settings.Name != "" {
		updates["name"] = settings.Name
	}
	if settings.Platform != "" {
		updates["platform"] = settings.Platform
	}
	if settings.AuthType != "" {
		updates["auth_type"] = settings.AuthType
	}
	if settings.ProxyURL != "" {
		updates["proxy_url"] = settings.ProxyURL
	}
	if settings.ChatIDLock != "" {
		updates["chat_id_lock"] = settings.ChatIDLock
	}

	// Handle Auth config - only update if non-empty
	if len(settings.Auth) > 0 {
		if b, err := json.Marshal(settings.Auth); err == nil {
			updates["auth_config"] = string(b)
		}
	}

	// Handle BashAllowlist - only update if non-empty
	if len(settings.BashAllowlist) > 0 {
		if b, err := json.Marshal(settings.BashAllowlist); err == nil {
			updates["bash_allowlist"] = string(b)
		}
	}

	if settings.DefaultCwd != "" {
		updates["default_cwd"] = settings.DefaultCwd
	}
	if settings.DefaultAgent != "" {
		updates["default_agent"] = settings.DefaultAgent
	}
	if settings.SmartGuideProvider != "" {
		updates["smartguide_provider"] = settings.SmartGuideProvider
	}
	if settings.SmartGuideModel != "" {
		updates["smartguide_model"] = settings.SmartGuideModel
	}
	if settings.RequirePairing != nil {
		updates["require_pairing"] = settings.RequirePairing
	}
	// Scenarios is intentionally allowed to be cleared (empty string) so
	// callers can unbind a bot from all scenarios.
	updates["scenarios"] = settings.Scenarios

	// Always update enabled and updated_at if explicitly set
	updates["enabled"] = settings.Enabled
	updates["updated_at"] = settings.UpdatedAt

	if len(updates) == 0 {
		return nil // Nothing to update
	}

	result := s.db.Model(&ImBotSettingsRecord{}).
		Where("bot_uuid = ?", uuid).
		Updates(updates)

	if result.Error != nil {
		return fmt.Errorf("failed to update settings: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("imbot settings with uuid %s not found", uuid)
	}

	return nil
}

// DeleteSettings deletes an ImBot configuration.
func (s *ImBotSettingsStore) DeleteSettings(uuid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := s.db.Where("bot_uuid = ?", uuid).Delete(&ImBotSettingsRecord{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete settings: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("imbot settings with uuid %s not found", uuid)
	}

	return nil
}

// ToggleSettings toggles the enabled status of an ImBot configuration.
func (s *ImBotSettingsStore) ToggleSettings(uuid string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var record ImBotSettingsRecord
	if err := s.db.Where("bot_uuid = ?", uuid).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, fmt.Errorf("imbot settings with uuid %s not found", uuid)
		}
		return false, fmt.Errorf("failed to get settings for toggle: %w", err)
	}

	newEnabled := !record.Enabled
	result := s.db.Model(&record).Update("enabled", newEnabled)
	if result.Error != nil {
		return false, fmt.Errorf("failed to toggle settings: %w", result.Error)
	}

	return newEnabled, nil
}

// SetEnabled writes the Bot's explicit lifecycle gate without touching any
// other settings. Capability lifecycle reconciliation uses this narrow method
// so it cannot accidentally clear partial-update fields such as scenarios.
func (s *ImBotSettingsStore) SetEnabled(uuid string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := s.db.Model(&ImBotSettingsRecord{}).
		Where("bot_uuid = ?", uuid).
		Updates(map[string]interface{}{"enabled": enabled, "updated_at": time.Now()})
	if result.Error != nil {
		return fmt.Errorf("failed to set imbot enabled state: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("imbot settings with uuid %s not found", uuid)
	}
	return nil
}

// recordToSettings converts an ImBotSettingsRecord to a Settings struct.
func recordToSettings(record ImBotSettingsRecord) (Settings, error) {
	settings := Settings{
		UUID:               record.BotUUID,
		Name:               record.Name,
		Platform:           record.Platform,
		AuthType:           record.AuthType,
		ProxyURL:           record.ProxyURL,
		ChatIDLock:         record.ChatIDLock,
		DefaultCwd:         record.DefaultCwd,
		DefaultAgent:       record.DefaultAgent,
		Enabled:            record.Enabled,
		SmartGuideProvider: record.SmartGuideProvider,
		SmartGuideModel:    record.SmartGuideModel,
		RequirePairing:     record.RequirePairing,
		Scenarios:          record.Scenarios,
		CreatedAt:          record.CreatedAt,
		UpdatedAt:          record.UpdatedAt,
		Auth:               make(map[string]string),
	}

	// Parse auth config JSON
	if record.AuthConfig != "" {
		if err := json.Unmarshal([]byte(record.AuthConfig), &settings.Auth); err != nil {
			return Settings{}, fmt.Errorf("failed to unmarshal auth config: %w", err)
		}
	}

	// Set Token field for backward compatibility (from auth["token"])
	if token, ok := settings.Auth["token"]; ok {
		settings.Token = token
	}

	// Parse bash allowlist JSON
	if record.BashAllowlist != "" {
		if err := json.Unmarshal([]byte(record.BashAllowlist), &settings.BashAllowlist); err != nil {
			return Settings{}, fmt.Errorf("failed to unmarshal bash allowlist: %w", err)
		}
	}

	return settings, nil
}

// generateUUID generates a unique ID for settings.
func generateUUID() string {
	// Simple UUID generation using timestamp and random
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), "imbot")
}
