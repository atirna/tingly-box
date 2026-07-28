package server

import (
	"time"

	"github.com/sirupsen/logrus"

	"github.com/tingly-dev/tingly-box/internal/remote_control/bot"
	notifymodule "github.com/tingly-dev/tingly-box/internal/server/module/notify"
	"github.com/tingly-dev/tingly-box/remote/channel"
)

// buildBotChatLister wires the GET /bots/:bot/chats endpoint to the shared
// chat store. The store is global across bots, so a chat is attributed to a
// bot when its platform matches the bot's channel platform; when the bot has
// a chat-id lock set, only that single chat is returned (a locked bot can
// reach no other). This is what makes the chat_id required by /notify and
// /interact discoverable — see ux-principles #5 (show the concrete value)
// and #11 (hand over the artifact for the next action).
func buildBotChatLister(reg *channel.Registry, provider botChatProvider) notifymodule.ChatLister {
	return func(botUUID string, includeDisabled bool) ([]notifymodule.ChatSummary, error) {
		// Resolve the bot's platform from its registered channel. If the bot
		// isn't running, the route layer already returned 404 before calling
		// the lister — but defend in depth anyway.
		ch, ok := reg.Get(botUUID)
		if !ok {
			return nil, nil
		}
		platform := ch.Platform()

		// A chat-id lock collapses the reachable set to one chat id.
		lock := provider.ChatIDLock(botUUID)

		// Shared store, owned by the StoreManager — not ours to close.
		store, err := provider.ChatStore()
		if err != nil {
			return nil, err
		}

		// ListChats scopes at the source: only records whose Platform field
		// equals this bot's channel platform are returned, so unattributed or
		// cross-platform chats never reach the API surface.
		all, err := store.ListChats(platform, includeDisabled)
		if err != nil {
			return nil, err
		}

		out := make([]notifymodule.ChatSummary, 0, len(all))
		for _, c := range all {
			if lock != "" && c.ChatID != lock {
				// A chat-id lock collapses the reachable set to one chat id.
				continue
			}
			if c.IsPaired && c.PairedBotUUID != "" && c.PairedBotUUID != botUUID {
				// Paired to another bot — it belongs to that bot's list, not
				// this one. Unpaired same-platform chats stay visible to every
				// bot of the platform: there is nothing else to attribute them
				// by, and hiding them would make fresh chats undiscoverable.
				continue
			}
			summary := notifymodule.ChatSummary{
				ChatID:        c.ChatID,
				Platform:      c.Platform,
				IsPaired:      c.IsPaired,
				IsWhitelisted: c.IsWhitelisted,
				ProjectPath:   c.ProjectPath,
				Disabled:      c.Disabled,
				DisabledAt:    formatChatTime(c.DisabledAt),
				UpdatedAt:     formatChatTime(c.UpdatedAt),
			}
			out = append(out, summary)
		}
		logrus.Debugf("bot chats list: bot=%s platform=%s lock=%q count=%d", botUUID, platform, lock, len(out))
		return out, nil
	}
}

// formatChatTime renders a chat's UpdatedAt as RFC3339; empty when zero.
func formatChatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// resolveReachableChat centralizes the "does this chat belong to this bot"
// check the delete/disable mutations must pass: the chat exists, its platform
// matches the bot's channel platform, and it is not paired to a different
// bot. Returns notifymodule.ErrChatNotFound on any miss — one body for all
// causes, so a caller cannot probe which chats exist on other platforms.
func resolveReachableChat(reg *channel.Registry, provider botChatProvider, botUUID, chatID string) (bot.ChatStoreInterface, error) {
	ch, ok := reg.Get(botUUID)
	if !ok {
		return nil, notifymodule.ErrChatNotFound
	}
	store, err := provider.ChatStore()
	if err != nil {
		return nil, err
	}
	c, err := store.GetChat(chatID)
	if err != nil {
		return nil, err
	}
	if c == nil || c.Platform != ch.Platform() {
		return nil, notifymodule.ErrChatNotFound
	}
	if c.IsPaired && c.PairedBotUUID != "" && c.PairedBotUUID != botUUID {
		return nil, notifymodule.ErrChatNotFound
	}
	return store, nil
}

// buildBotChatDeleter wires DELETE /bots/:bot/chats/:chat_id. Refuses to
// delete the bot's chat-id lock — a locked bot's only reachable chat.
func buildBotChatDeleter(reg *channel.Registry, provider botChatProvider) notifymodule.ChatDeleter {
	return func(botUUID, chatID string) error {
		if lock := provider.ChatIDLock(botUUID); lock != "" && chatID == lock {
			return notifymodule.ErrChatLocked
		}
		store, err := resolveReachableChat(reg, provider, botUUID, chatID)
		if err != nil {
			return err
		}
		return store.DeleteChat(chatID)
	}
}

// buildBotChatDisabler wires PUT /bots/:bot/chats/:chat_id/disabled.
func buildBotChatDisabler(reg *channel.Registry, provider botChatProvider) notifymodule.ChatDisabler {
	return func(botUUID, chatID string, disabled bool) error {
		store, err := resolveReachableChat(reg, provider, botUUID, chatID)
		if err != nil {
			return err
		}
		return store.SetChatDisabled(chatID, disabled)
	}
}

// buildBotChatDisabledChecker wires the outbound blocklist check used by
// POST /bots/:bot/{notify,interact}. Unknown chats report false so pushes to
// fresh chat ids keep working.
func buildBotChatDisabledChecker(provider botChatProvider) notifymodule.ChatDisabledChecker {
	return func(chatID string) bool {
		store, err := provider.ChatStore()
		if err != nil {
			return false
		}
		return store.IsChatDisabled(chatID)
	}
}

// botChatProvider is the narrow surface buildBotChatLister needs from the
// imbot handler: reach the shared chat store and look up a bot's chat-id
// lock. An interface so the helper is testable without the full imbot.Handler.
type botChatProvider interface {
	ChatStore() (bot.ChatStoreInterface, error)
	ChatIDLock(botUUID string) string
}
