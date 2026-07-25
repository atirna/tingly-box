package bot

// Outbound interactive controls now travel as a neutral imbot.ActionSet on
// SendMessageOptions.Actions, and each platform renders them itself.
//
// This file used to build three parallel renderings of the same buttons into
// the metadata bag — a Telegram markup under "replyMarkup", a neutral card
// under "card", and Feishu card JSON under "card_json" — and hope the right
// one got picked up. Two of the three were never read on any send path, and
// the one that was carried a go-telegram type that Feishu could not decode,
// so Feishu users got messages with no buttons at all.
//
// What is left here is the one thing that genuinely is out-of-band metadata:
// the flag asking the send path to remember this message's ID so the action
// menu can be removed later.

const trackActionMenuIDKey = "_trackActionMenuID"

// trackActionMenuMetadata marks an outbound message as the current action-menu
// message, so its ID is recorded and the menu can be taken down later.
func trackActionMenuMetadata() map[string]interface{} {
	return map[string]interface{}{
		trackActionMenuIDKey: true,
	}
}

// withContextToken copies the inbound reply-context token onto outbound
// metadata when the platform needs it (Weixin/WeCom).
//
// TODO(phase-4): this should not be the caller's job either — the bot knows
// which inbound message it is replying to. Seam 2 moves it into BaseBot.
func withContextToken(metadata map[string]interface{}, contextToken string) map[string]interface{} {
	if contextToken == "" {
		return metadata
	}
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["context_token"] = contextToken
	return metadata
}
