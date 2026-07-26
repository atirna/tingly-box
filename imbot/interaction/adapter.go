package interaction

import (
	"context"
	"fmt"
	"strings"

	"github.com/tingly-dev/tingly-box/imbot/core"
)

// Adapter converts platform-agnostic interactions to platform-specific format
type Adapter interface {
	// SupportsInteractions returns true if the platform supports native interactions
	SupportsInteractions() bool

	// BuildFallbackText creates text-based numbered options
	BuildFallbackText(message string, interactions []Interaction) string

	// ParseResponse parses user response into InteractionResponse
	ParseResponse(msg core.Message) (*InteractionResponse, error)

	// UpdateMessage updates a message (optional, for platforms that support it)
	UpdateMessage(ctx context.Context, bot core.Bot, chatID, messageID, text string, interactions []Interaction) error

	// CanEditMessages returns true if platform supports message editing
	CanEditMessages() bool
}

// BaseAdapter provides common functionality for adapters
type BaseAdapter struct {
	supportsInteractions bool
	canEditMessages      bool
}

// NewBaseAdapter creates a new base adapter with the given capabilities
func NewBaseAdapter(supportsInteractions, canEditMessages bool) *BaseAdapter {
	return &BaseAdapter{
		supportsInteractions: supportsInteractions,
		canEditMessages:      canEditMessages,
	}
}

// SupportsInteractions returns true if the platform supports native interactions
func (a *BaseAdapter) SupportsInteractions() bool {
	return a.supportsInteractions
}

// CanEditMessages returns true if platform supports message editing
func (a *BaseAdapter) CanEditMessages() bool {
	return a.canEditMessages
}

// BuildActions renders the interaction options as neutral actions. It is
// shared rather than overridable: there is nothing platform-specific left in
// the job, and the five hand-written versions this replaces had already
// drifted into disagreeing with their own parsers.
func (a *BaseAdapter) BuildActions(interactions []Interaction) *core.ActionSet {
	return BuildActions(interactions)
}

// ParseResponse reads an interaction reply out of an inbound message. Shared
// for the same reason as BuildActions — the two are the ends of one loop and
// must agree.
func (a *BaseAdapter) ParseResponse(msg core.Message) (*InteractionResponse, error) {
	return ParseActionResponse(msg)
}

// UpdateMessage default implementation returns ErrNotSupported
func (a *BaseAdapter) UpdateMessage(ctx context.Context, bot core.Bot, chatID, messageID, text string, interactions []Interaction) error {
	return ErrNotSupported
}

// BuildFallbackText creates numbered text options for text mode
// prompt is the text asking user to reply with number (e.g., "Reply with number:" or "请回复数字：")
// cancelText is the cancel option text (e.g., "Cancel" or "取消")
func BuildFallbackText(message string, interactions []Interaction, prompt, cancelText string) string {
	var sb strings.Builder
	sb.WriteString(message)
	sb.WriteString("\n\n")
	sb.WriteString(prompt)
	sb.WriteString("\n")

	for i, item := range interactions {
		if item.Type == ActionSelect || item.Type == ActionConfirm {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, item.Label))
		}
	}
	sb.WriteString("0. ")
	sb.WriteString(cancelText)

	return sb.String()
}
