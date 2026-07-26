package telegram

import (
	"context"
	"strings"

	"github.com/go-telegram/bot/models"
	"github.com/tingly-dev/tingly-box/imbot/core"
	itx "github.com/tingly-dev/tingly-box/imbot/interaction"
)

// InteractionAdapter implements itx.Adapter for Telegram
type InteractionAdapter struct {
	*itx.BaseAdapter
}

// NewInteractionAdapter creates a new Telegram interaction adapter
func NewInteractionAdapter() *InteractionAdapter {
	return &InteractionAdapter{
		BaseAdapter: itx.NewBaseAdapter(true, true), // Supports interactions and editing
	}
}

// BuildFallbackText creates numbered text options for text mode
func (a *InteractionAdapter) BuildFallbackText(message string, interactions []itx.Interaction) string {
	return itx.BuildFallbackText(message, interactions, "Reply with number:", "Cancel")
}

// UpdateMessage edits a Telegram message
func (a *InteractionAdapter) UpdateMessage(ctx context.Context, bot core.Bot, chatID, messageID, text string, interactions []itx.Interaction) error {
	// Need to use platform-specific bot interface
	// This is a placeholder - actual implementation would use the platform adapter
	return itx.ErrNotSupported
}

// keyboardBuilder helps build Telegram inline keyboards
type keyboardBuilder struct {
	rows [][]models.InlineKeyboardButton
}

// AddRow adds a new row with buttons
func (b *keyboardBuilder) AddRow(buttons ...models.InlineKeyboardButton) {
	b.rows = append(b.rows, buttons)
}

// AddButton adds a button to the last row
func (b *keyboardBuilder) AddButton(button models.InlineKeyboardButton) {
	if len(b.rows) == 0 {
		b.rows = append(b.rows, []models.InlineKeyboardButton{})
	}
	b.rows[len(b.rows)-1] = append(b.rows[len(b.rows)-1], button)
}

// Callback data helpers

// formatCallbackData formats callback data parts with colon separator
func formatCallbackData(parts ...string) string {
	return strings.Join(parts, ":")
}

// parseCallbackData parses callback data into parts
func parseCallbackData(data string) []string {
	return strings.Split(data, ":")
}
