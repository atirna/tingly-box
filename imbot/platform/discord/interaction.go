package discord

import (
	"context"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/tingly-dev/tingly-box/imbot/core"
	itx "github.com/tingly-dev/tingly-box/imbot/interaction"
)

// InteractionAdapter implements itx.Adapter for Discord
type InteractionAdapter struct {
	*itx.BaseAdapter
}

// NewInteractionAdapter creates a new Discord interaction adapter
func NewInteractionAdapter() *InteractionAdapter {
	return &InteractionAdapter{
		BaseAdapter: itx.NewBaseAdapter(true, true), // Supports interactions and editing
	}
}

// toMessageComponents converts []discordgo.Button to []discordgo.MessageComponent
func toMessageComponents(buttons []discordgo.Button) []discordgo.MessageComponent {
	result := make([]discordgo.MessageComponent, len(buttons))
	for i, btn := range buttons {
		result[i] = btn
	}
	return result
}

// BuildFallbackText creates numbered text options for text mode
func (a *InteractionAdapter) BuildFallbackText(message string, interactions []itx.Interaction) string {
	return itx.BuildFallbackText(message, interactions, "Reply with number:", "Cancel")
}

// UpdateMessage edits a Discord message
func (a *InteractionAdapter) UpdateMessage(ctx context.Context, bot core.Bot, chatID, messageID, text string, interactions []itx.Interaction) error {
	// Discord message editing requires the Discord-specific bot interface
	// For now, return not supported as we need to add this to the imbot interface
	return itx.ErrNotSupported
}

// Custom ID helpers

// formatCustomID formats Discord custom ID with colon separator
func formatCustomID(parts ...string) string {
	return strings.Join(parts, ":")
}

// parseCustomID parses Discord custom ID into parts
func parseCustomID(id string) []string {
	return strings.Split(id, ":")
}
