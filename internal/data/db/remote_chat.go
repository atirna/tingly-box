package db

import (
	"time"
)

// Chat is all state associated with an IM chat (direct or group): which
// project it is bound to, whether it is paired or whitelisted, and which
// agent is currently driving it.
//
// The type lives here rather than in internal/remote_control/bot because that
// package already imports this one; keeping the domain type on this side lets
// the GORM store implement bot.ChatStoreInterface without an import cycle. The
// bot package aliases it (type Chat = db.Chat), so callers are unaffected.
type Chat struct {
	ChatID         string   `json:"chat_id"`
	Platform       string   `json:"platform"`
	ProjectPath    string   `json:"project_path,omitempty"`
	ProjectHistory []string `json:"project_history,omitempty"` // MRU list of paths this chat has bound to
	OwnerID        string   `json:"owner_id,omitempty"`

	// Pairing (TOFU) — applies to direct messages only. Group chats continue
	// to use the IsWhitelisted gate, but the operator who whitelisted the
	// group must themselves be paired in DM with the same bot.
	IsPaired       bool      `json:"is_paired,omitempty"`
	PairedBotUUID  string    `json:"paired_bot_uuid,omitempty"`
	PairedSenderID string    `json:"paired_sender_id,omitempty"`
	PairedAt       time.Time `json:"paired_at,omitempty"`

	// Group-specific
	IsWhitelisted bool   `json:"is_whitelisted"`
	WhitelistedBy string `json:"whitelisted_by,omitempty"`

	// Bash state
	BashCwd string `json:"bash_cwd,omitempty"`

	// CurrentAgent is which agent is driving the chat ("tingly-box" or "claude").
	CurrentAgent string `json:"current_agent,omitempty"`

	// Chat-level settings
	Verbose *bool `json:"verbose,omitempty"` // Verbose mode: nil=use bot default, true=verbose, false=quiet

	// Disabled is the inbound blocklist flag: a disabled chat's messages are
	// dropped before any handler runs and the chat is excluded from the
	// reachable list. Unlike deletion, the row survives auto-create paths —
	// only an explicit enable clears it.
	Disabled   bool      `json:"disabled,omitempty"`
	DisabledAt time.Time `json:"disabled_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RemoteChatRecord is the GORM model behind Chat.
//
// ProjectHistory stays a JSON column for now — splitting it into its own
// table is the next step (see .design/remote-storage.md P2); everything the
// product actually queries on (owner, whitelist, pairing) is a real column
// with an index.
type RemoteChatRecord struct {
	ChatID      string `gorm:"primaryKey;column:chat_id"`
	Platform    string `gorm:"column:platform"`
	ProjectPath string `gorm:"column:project_path"`
	OwnerID     string `gorm:"column:owner_id;index:idx_remote_chats_owner,priority:1"`

	// ProjectHistory is the MRU path list, JSON-encoded.
	ProjectHistory string `gorm:"column:project_history;type:text"`

	IsPaired       bool      `gorm:"column:is_paired"`
	PairedBotUUID  string    `gorm:"column:paired_bot_uuid;index:idx_remote_chats_paired_bot"`
	PairedSenderID string    `gorm:"column:paired_sender_id"`
	PairedAt       time.Time `gorm:"column:paired_at"`

	IsWhitelisted bool   `gorm:"column:is_whitelisted;index:idx_remote_chats_whitelisted"`
	WhitelistedBy string `gorm:"column:whitelisted_by"`

	BashCwd string `gorm:"column:bash_cwd"`

	CurrentAgent string `gorm:"column:current_agent"`

	Verbose *bool `gorm:"column:verbose"`

	Disabled   bool      `gorm:"column:disabled;index:idx_remote_chats_disabled"`
	DisabledAt time.Time `gorm:"column:disabled_at"`

	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName specifies the table name for GORM.
func (RemoteChatRecord) TableName() string {
	return "remote_chats"
}
