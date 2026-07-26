package feishu

import (
	"context"

	"github.com/tingly-dev/tingly-box/imbot/core"
	itx "github.com/tingly-dev/tingly-box/imbot/interaction"
)

// InteractionAdapter implements itx.Adapter for Feishu
type InteractionAdapter struct {
	*itx.BaseAdapter
}

// NewInteractionAdapter creates a new Feishu interaction adapter
func NewInteractionAdapter() *InteractionAdapter {
	return &InteractionAdapter{
		BaseAdapter: itx.NewBaseAdapter(true, false), // Supports cards but no editing via stream mode
	}
}

// SupportsInteractions returns true - Feishu supports interactive cards
func (a *InteractionAdapter) SupportsInteractions() bool {
	return true
}

// BuildFallbackText creates numbered text options
// This is used when Mode=Text or when cards are not appropriate
func (a *InteractionAdapter) BuildFallbackText(message string, interactions []itx.Interaction) string {
	return itx.BuildFallbackText(message, interactions, "请回复数字：", "取消")
}

// UpdateMessage updates a Feishu message
// Note: Feishu message editing is limited in stream mode
func (a *InteractionAdapter) UpdateMessage(ctx context.Context, bot core.Bot, chatID, messageID, text string, interactions []itx.Interaction) error {
	// Feishu doesn't support message editing via the same API
	// Would need to use the message update API separately
	return itx.ErrNotSupported
}

// CanEditMessages returns false - Feishu stream mode doesn't support editing
func (a *InteractionAdapter) CanEditMessages() bool {
	return false
}
