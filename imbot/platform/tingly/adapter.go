package tingly

import (
	"time"

	"github.com/tingly-dev/tingly-box/imbot/core"
)

// decodeActions extracts the outbound keyboard for the test harness.
func decodeActions(opts *core.SendMessageOptions) *Keyboard {
	return convertActionSet(opts.Actions)
}

func convertActionSet(set *core.ActionSet) *Keyboard {
	if set.IsEmpty() {
		return nil
	}
	out := &Keyboard{Rows: make([][]Button, 0, len(set.Rows))}
	for _, row := range set.Rows {
		buttons := make([]Button, 0, len(row))
		for _, a := range row {
			buttons = append(buttons, Button{
				Label: a.Label,
				// The harness mirrors Telegram's flat encoding so tests read
				// the same strings production does. Unlike Telegram it has no
				// size limit, so no token fallback is needed here.
				CallbackData: a.EffectivePayload().FlatCallbackData(),
				URL:          a.URL,
			})
		}
		out.Rows = append(out.Rows, buttons)
	}
	return out
}

// NewIncomingTextMessage constructs an inbound text message. messageID is
// optional — tests usually pass "" and let the harness mint one.
func NewIncomingTextMessage(messageID, chatID string, sender core.Sender, text string, chatType core.ChatType) core.Message {
	return core.Message{
		ID:        messageID,
		Platform:  core.PlatformTingly,
		Timestamp: time.Now().Unix(),
		Sender:    sender,
		Recipient: core.Recipient{
			ID:   chatID,
			Type: recipientTypeFromChat(chatType),
		},
		Content:  core.NewTextContent(text),
		ChatType: chatType,
	}
}

// NewIncomingCallback constructs an inbound callback-query message. The
// shape mirrors imbot/platform/telegram/adapter.go: metadata carries
// is_callback / callback_data / callback_query_id, and Content is empty
// text. Production handlers (handler_message.go:38, telegram_callback.go,
// the generic interaction handler) all key off Metadata["is_callback"].
func NewIncomingCallback(messageID, chatID string, sender core.Sender, callbackData string, chatType core.ChatType) core.Message {
	return core.Message{
		ID:        messageID,
		Platform:  core.PlatformTingly,
		Timestamp: time.Now().Unix(),
		Sender:    sender,
		Recipient: core.Recipient{
			ID:   chatID,
			Type: recipientTypeFromChat(chatType),
		},
		Content:  core.NewTextContent(""),
		ChatType: chatType,
		Payload:  core.PayloadFromCallbackData(callbackData),
		Metadata: map[string]interface{}{
			"is_callback":       true,
			"callback_data":     callbackData,
			"callback_query_id": messageID,
		},
	}
}

func recipientTypeFromChat(c core.ChatType) string {
	switch c {
	case core.ChatTypeGroup:
		return "group"
	case core.ChatTypeChannel:
		return "channel"
	case core.ChatTypeThread:
		return "thread"
	default:
		return "user"
	}
}
