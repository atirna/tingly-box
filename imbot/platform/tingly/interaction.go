package tingly

import (
	"context"
	"strings"

	"github.com/tingly-dev/tingly-box/imbot/core"
	itx "github.com/tingly-dev/tingly-box/imbot/interaction"
)

// InteractionAdapter implements interaction.Adapter for the tingly platform.
//
// It mirrors the Telegram adapter: callbacks travel through metadata as
// is_callback / callback_data, and the callback data uses the conventional
// "ia:<interactionID>:<value>" or "ia:<interactionID>:<requestID>:<value>"
// shape.
type InteractionAdapter struct {
	*itx.BaseAdapter
}

// NewInteractionAdapter creates an adapter that supports both interactions
// and message editing.
func NewInteractionAdapter() *InteractionAdapter {
	return &InteractionAdapter{
		BaseAdapter: itx.NewBaseAdapter(true, true),
	}
}

// BuildFallbackText delegates to the package-default numbered-list helper.
func (a *InteractionAdapter) BuildFallbackText(message string, interactions []itx.Interaction) string {
	return itx.BuildFallbackText(message, interactions, "Reply with number:", "Cancel")
}

// UpdateMessage edits an existing message via the bot. Tingly supports
// editing, so we delegate to bot.EditMessage.
func (a *InteractionAdapter) UpdateMessage(ctx context.Context, bot core.Bot, chatID, messageID, text string, interactions []itx.Interaction) error {
	return bot.EditMessage(ctx, messageID, text)
}

func formatCallbackData(parts ...string) string {
	return strings.Join(parts, ":")
}
