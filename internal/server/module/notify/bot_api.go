// Package notify — bot interaction API.
//
// bot_api.go is the *general, caller-facing* bot interaction surface: any
// authenticated integration can drive a running bot's channel to deliver a
// one-way notification (Notify) or start an interactive prompt (Interact) and
// long-poll for the reply (Wait). It is distinct from handler.go, which is the
// Claude-Code-hook-specific shim under /tingly/:scenario/{notify,wait} whose
// value is *plugin classification* (hook_event_name → push vs interactive).
//
// This handler is bot-scoped (the bot UUID is in the path) and bypasses the
// scenario plugin entirely: the caller has already decided whether the request
// is one-way or interactive by choosing the endpoint. Auth is inherited from
// the control-plane route group (getUserAuthMiddleware) it is registered on;
// see server_control.go and .design/bot-interaction-api.md.
//
// The two interface kinds map directly onto the existing channel.Channel
// contract: Notify → Channel.Send (fire-and-forget); Interact → Channel.Prompt
// (blocking), with the reply delivered through the shared interaction.Registry
// the Wait handler reads from. No new domain types or runtime — this file is a
// thin HTTP adapter over remote/channel + remote/interaction.
package notify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/tingly-dev/tingly-box/remote/channel"
	"github.com/tingly-dev/tingly-box/remote/interaction"
)

// DefaultInteractTimeout bounds a single interactive prompt when the caller
// omits timeout_seconds. It must stay below interaction.Registry's entry TTL
// (30s today) plus a safety margin is NOT required — the registry TTL is a
// fallback eviction, not the prompt budget; the prompt's own context deadline
// governs how long Channel.Prompt blocks.
const DefaultInteractTimeout = 5 * time.Minute

// MaxInteractTimeout caps an interactive prompt's budget so a caller cannot
// pin a registry entry and an IM prompt open indefinitely.
const MaxInteractTimeout = 30 * time.Minute

// BotAPIHandler is the HTTP front end for the general bot interaction API.
// It resolves a bot's channel from the registry and drives it directly.
type BotAPIHandler struct {
	channels     *channel.Registry
	results      *interaction.Registry[interaction.Result]
	chatLister   ChatLister
	chatDeleter  ChatDeleter
	chatDisabler ChatDisabler
	chatDisabled ChatDisabledChecker
}

// ChatSummary is the projection of a bot's chat record exposed by
// GET /bots/:bot/chats. ChatID is the channel-native conversation identifier
// the caller must pass as chat_id to /notify and /interact — surfacing it
// here is what makes those endpoints usable (see ux-principles #5/#11).
type ChatSummary struct {
	ChatID        string `json:"chat_id"`
	Platform      string `json:"platform,omitempty"`
	IsPaired      bool   `json:"is_paired,omitempty"`
	IsWhitelisted bool   `json:"is_whitelisted,omitempty"`
	ProjectPath   string `json:"project_path,omitempty"`
	Disabled      bool   `json:"disabled,omitempty"`
	DisabledAt    string `json:"disabled_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

// ChatLister returns the chats a bot can reach, scoped to that bot's
// platform (and to its chat-id lock when one is set). includeDisabled adds
// blocklisted chats to the result. Defined here so the notify package does
// not import remote_control/bot — the server wires the concrete
// implementation. Returns (nil, nil) when no lister is configured.
type ChatLister func(botUUID string, includeDisabled bool) ([]ChatSummary, error)

// ChatDeleter hard-deletes a chat record reachable by the bot. Wired by the
// server for the same import-direction reason as ChatLister.
type ChatDeleter func(botUUID, chatID string) error

// ChatDisabler toggles a reachable chat's inbound blocklist flag.
type ChatDisabler func(botUUID, chatID string, disabled bool) error

// ChatDisabledChecker reports whether a chat is blocklisted. Used by Notify
// and Interact so disable cuts both directions — a disabled chat neither
// reaches the bot nor is reachable from it. Unknown chats report false so
// pushes to fresh chat ids keep working.
type ChatDisabledChecker func(chatID string) bool

// ErrChatNotFound is returned by ChatDeleter/ChatDisabler when the chat is
// not in the bot's reachable set (unknown, wrong platform, or paired to a
// different bot). Mapped to HTTP 404.
var ErrChatNotFound = errors.New("chat not found")

// ErrChatLocked is returned by ChatDeleter when the chat is the bot's
// chat-id lock — its only reachable chat; deleting it is almost certainly a
// mistake. Mapped to HTTP 409.
var ErrChatLocked = errors.New("chat is the bot's chat-id lock")

// NewBotAPIHandler builds the handler. channels and results are the same
// registries the Claude Code scenario path uses.
// chatLister/chatDeleter/chatDisabler may be nil — the corresponding
// endpoints then report unavailable.
func NewBotAPIHandler(channels *channel.Registry, results *interaction.Registry[interaction.Result], chatLister ChatLister, chatDeleter ChatDeleter, chatDisabler ChatDisabler, chatDisabled ChatDisabledChecker) *BotAPIHandler {
	return &BotAPIHandler{channels: channels, results: results, chatLister: chatLister, chatDeleter: chatDeleter, chatDisabler: chatDisabler, chatDisabled: chatDisabled}
}

// isChatDisabled reports the blocklist flag through the wired checker; a nil
// checker (stock setups without chat management) never blocks.
func (h *BotAPIHandler) isChatDisabled(chatID string) bool {
	return h.chatDisabled != nil && h.chatDisabled(chatID)
}

// notifyRequest is the body of POST /bots/:bot/notify — a one-way push.
type notifyRequest struct {
	ChatID string `json:"chat_id" binding:"required"`
	Title  string `json:"title,omitempty"`
	Body   string `json:"body" binding:"required"`
	// Level is an optional render hint: info | warn | error. Passed through
	// to the channel via Notification.Meta; channels that don't understand it
	// ignore it.
	Level string `json:"level,omitempty"`
}

// interactRequest is the body of POST /bots/:bot/interact — starts an
// interactive prompt and returns a request_id the caller long-polls.
type interactRequest struct {
	ChatID string `json:"chat_id" binding:"required"`
	// Kind is one of interaction.Kind: confirm | choose | ask.
	Kind    string               `json:"kind" binding:"required"`
	Title   string               `json:"title" binding:"required"`
	Body    string               `json:"body,omitempty"`
	Options []interaction.Option `json:"options,omitempty"`
	// TimeoutSeconds bounds how long the prompt waits for a reply. Defaults
	// to DefaultInteractTimeout; capped at MaxInteractTimeout.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// Notify handles POST /api/v1/bots/:bot/notify.
//
//	200  delivered (one-way push, no reply expected)
//	400  malformed body
//	404  bot not running (unknown or stopped — same body shape)
//	500  delivery failed
func (h *BotAPIHandler) Notify(c *gin.Context) {
	botUUID := c.Param("bot")

	var req notifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	ch, ok := h.resolveChannel(botUUID)
	if !ok {
		// Uniform 404 for unknown and stopped bots — see spec §3.5 (defend in
		// depth: an authenticated caller must not probe which bots exist).
		c.JSON(http.StatusNotFound, gin.H{"error": "bot not running"})
		return
	}
	if h.isChatDisabled(req.ChatID) {
		// Disable cuts both directions: same body as an unknown chat so the
		// caller cannot distinguish blocked from nonexistent.
		c.JSON(http.StatusNotFound, gin.H{"error": "chat not reachable"})
		return
	}

	notification := interaction.Notification{
		Title: req.Title,
		Body:  req.Body,
	}
	if req.Level != "" {
		notification.Meta = map[string]any{"level": req.Level}
	}

	target := channel.Target{ChatID: req.ChatID}
	// Send is fire-and-forget at the protocol level but synchronous here so we
	// can report delivery failure. A short bounded context guards against a
	// wedged channel blocking the request thread.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	if err := ch.Send(ctx, target, notification); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"bot":     botUUID,
			"chat_id": req.ChatID,
		}).Warn("bot notify push failed")
		h.auditLog(botUUID, "bot.notify.error", map[string]any{
			"chat_id": req.ChatID,
			"err":     err.Error(),
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delivery failed"})
		return
	}

	h.auditLog(botUUID, "bot.notify", map[string]any{"chat_id": req.ChatID})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Interact handles POST /api/v1/bots/:bot/interact.
//
//	202  interactive flow started; client polls wait_url
//	400  malformed body or invalid kind
//	404  bot not running
//	503  interaction registry unavailable (no bot middle layer wired)
func (h *BotAPIHandler) Interact(c *gin.Context) {
	botUUID := c.Param("bot")

	if h.results == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "interaction registry unavailable"})
		return
	}

	var req interactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	kind := interaction.Kind(req.Kind)
	if !validKind(kind) {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid kind %q (want confirm|choose|ask)", req.Kind)})
		return
	}
	if (kind == interaction.KindConfirm || kind == interaction.KindChoose) && len(req.Options) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("kind %q requires at least one option", req.Kind)})
		return
	}

	ch, ok := h.resolveChannel(botUUID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "bot not running"})
		return
	}
	if h.isChatDisabled(req.ChatID) {
		// Disable cuts both directions — see Notify.
		c.JSON(http.StatusNotFound, gin.H{"error": "chat not reachable"})
		return
	}

	budget := interactBudget(req.TimeoutSeconds)
	id := newRequestID()
	if !h.results.Begin(id) {
		// Practically unreachable (id is freshly random) — keep the contract
		// honest rather than panicking.
		c.JSON(http.StatusConflict, gin.H{"error": "request id collision, retry"})
		return
	}

	ix := interaction.Interaction{
		ID:      id,
		Kind:    kind,
		Title:   req.Title,
		Body:    req.Body,
		Options: req.Options,
		Timeout: budget,
	}
	target := channel.Target{ChatID: req.ChatID}

	// Drive the prompt in the background and resolve the registry entry with
	// the final reply (or a timeout/error). Mirrors the Claude Code plugin's
	// run() — see remote/scenario/builtin/claudecode/plugin.go.
	go h.runPrompt(botUUID, ch, target, ix, budget)

	h.auditLog(botUUID, "bot.interact.start", map[string]any{
		"interaction_id": id,
		"chat_id":        req.ChatID,
		"kind":           string(kind),
	})

	c.JSON(http.StatusAccepted, gin.H{
		"request_id": id,
		"wait_url":   fmt.Sprintf("/api/v1/bots/%s/interact/%s", botUUID, id),
		"expires_at": time.Now().Add(budget).UTC().Format(time.RFC3339),
	})
}

// Wait handles GET /api/v1/bots/:bot/interact/:request_id?timeout=45s.
//
// Status mapping is identical to the Claude Code /wait endpoint
// (handler.go): 200 answered/cancelled, 410 timeout, 504 pending, 404 expired.
// The :bot param is accepted for path symmetry but not re-resolved — the
// interaction id already encodes its owning bot implicitly via the channel it
// was started against, and the registry is shared.
func (h *BotAPIHandler) Wait(c *gin.Context) {
	if h.results == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
		return
	}
	requestID := c.Param("request_id")
	if requestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing request_id"})
		return
	}

	timeout := parseTimeout(c.Query("timeout"))

	ch, ok := h.results.Await(requestID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"status": "expired"})
		return
	}

	select {
	case res := <-ch:
		respondResult(c, res)
	case <-time.After(timeout):
		c.JSON(http.StatusGatewayTimeout, gin.H{"status": "pending"})
	case <-c.Request.Context().Done():
		c.JSON(http.StatusGatewayTimeout, gin.H{"status": "pending"})
	}
}

// ListChats handles GET /api/v1/bots/:bot/chats.
//
// Returns the chats a bot can reach, so a caller of /notify and /interact
// can discover the channel-native chat_id those endpoints require. Without
// this, the chat_id the request body demands is undiscoverable (it is not in
// /help, not on the bot table, and not otherwise exposed).
//
// A bot that isn't running simply has no reachable chats — that's an empty
// state, not an error — so this endpoint never returns 404. The response
// carries a `running` flag so the UI can tailor the empty message ("start the
// bot" vs "send it a message"). See ux-principles #11.
//
//	200  chat list (possibly empty) with running flag
//	503  chat listing unavailable (no store wired)
func (h *BotAPIHandler) ListChats(c *gin.Context) {
	botUUID := c.Param("bot")
	includeDisabled := c.Query("include_disabled") == "true"

	_, running := h.resolveChannel(botUUID)
	if !running {
		// A stopped/unknown bot has zero reachable chats; surface that as a
		// normal empty result rather than a 404 so the UI shows the empty
		// state instead of "failed to list chats".
		c.JSON(http.StatusOK, gin.H{"chats": []ChatSummary{}, "running": false})
		return
	}
	if h.chatLister == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "chat listing unavailable"})
		return
	}

	chats, err := h.chatLister(botUUID, includeDisabled)
	if err != nil {
		logrus.WithError(err).WithField("bot", botUUID).Warn("bot chats list failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "chat listing failed"})
		return
	}
	if chats == nil {
		chats = []ChatSummary{}
	}
	c.JSON(http.StatusOK, gin.H{"chats": chats, "running": true})
}

// DeleteChat handles DELETE /api/v1/bots/:bot/chats/:chat_id.
//
// Hard-deletes the chat record: pairing, whitelist, and project binding are
// gone. If the chat messages the bot again, the normal auto-create path
// rebuilds it as a brand-new chat (re-pair required when pairing is
// enforced). Session history is untouched.
//
//	200  deleted
//	404  chat not in this bot's reachable set
//	409  chat_id is the bot's chat-id lock (its only reachable chat)
//	503  chat management unavailable (no deleter wired)
func (h *BotAPIHandler) DeleteChat(c *gin.Context) {
	botUUID := c.Param("bot")
	chatID := c.Param("chat_id")

	if h.chatDeleter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "chat management unavailable"})
		return
	}

	if err := h.chatDeleter(botUUID, chatID); err != nil {
		if errors.Is(err, ErrChatNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
			return
		}
		if errors.Is(err, ErrChatLocked) {
			c.JSON(http.StatusConflict, gin.H{"error": "chat is this bot's chat-id lock; unlock the bot first"})
			return
		}
		logrus.WithError(err).WithFields(logrus.Fields{"bot": botUUID, "chat_id": chatID}).Warn("bot chat delete failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "chat delete failed"})
		return
	}

	h.auditLog(botUUID, "bot.chat.delete", map[string]any{"chat_id": chatID})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// SetChatDisabled handles PUT /api/v1/bots/:bot/chats/:chat_id/disabled.
//
// Toggles the chat's inbound blocklist flag. A disabled chat's messages are
// dropped before any handler runs (including /bind — it cannot re-enable
// itself) and it disappears from the reachable list, notify, and interact.
//
//	200  updated
//	400  malformed body
//	404  chat not in this bot's reachable set
//	503  chat management unavailable (no disabler wired)
func (h *BotAPIHandler) SetChatDisabled(c *gin.Context) {
	botUUID := c.Param("bot")
	chatID := c.Param("chat_id")

	if h.chatDisabler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "chat management unavailable"})
		return
	}

	var req setChatDisabledRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.Disabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "disabled field is required"})
		return
	}

	if err := h.chatDisabler(botUUID, chatID, *req.Disabled); err != nil {
		if errors.Is(err, ErrChatNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
			return
		}
		logrus.WithError(err).WithFields(logrus.Fields{"bot": botUUID, "chat_id": chatID}).Warn("bot chat disable failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "chat update failed"})
		return
	}

	h.auditLog(botUUID, "bot.chat.disabled", map[string]any{"chat_id": chatID, "disabled": *req.Disabled})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// setChatDisabledRequest is the body of PUT /bots/:bot/chats/:chat_id/disabled.
// Disabled is a pointer so an omitted field is a 400, not a silent enable.
type setChatDisabledRequest struct {
	Disabled *bool `json:"disabled"`
}

// resolveChannel looks up the bot's channel. Centralized so both Notify and
// Interact return the identical 404 body for unknown and stopped bots.
func (h *BotAPIHandler) resolveChannel(botUUID string) (channel.Channel, bool) {
	if h.channels == nil {
		return nil, false
	}
	return h.channels.Get(botUUID)
}

// runPrompt drives Channel.Prompt to completion and resolves the registry
// entry. Runs in its own goroutine started by Interact.
func (h *BotAPIHandler) runPrompt(botUUID string, ch channel.Channel, target channel.Target, ix interaction.Interaction, budget time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	reply, err := ch.Prompt(ctx, target, ix)
	if err != nil {
		status := interaction.StatusError
		if errors.Is(err, context.DeadlineExceeded) {
			status = interaction.StatusTimeout
		}
		h.results.Resolve(ix.ID, interaction.Result{
			Status: status,
			Reason: err.Error(),
		})
		h.auditLog(botUUID, "bot.interact.error", map[string]any{
			"interaction_id": ix.ID,
			"err":            err.Error(),
		})
		return
	}

	status := interaction.StatusAnswered
	if reply.Status == interaction.StatusCancelled {
		status = interaction.StatusCancelled
	}
	h.results.Resolve(ix.ID, interaction.Result{
		Status:   status,
		Decision: replyDecision(reply),
	})
	h.auditLog(botUUID, "bot.interact.done", map[string]any{
		"interaction_id": ix.ID,
		"status":         string(status),
	})
}

// replyDecision packages the human's reply into the Result.Decision map the
// Wait caller consumes. Selected is the chosen option value; FreeText carries
// ask-style input.
func replyDecision(reply interaction.Reply) map[string]any {
	decision := map[string]any{}
	if reply.Selected != "" {
		decision["selected"] = reply.Selected
	}
	if reply.FreeText != "" {
		decision["free_text"] = reply.FreeText
	}
	return decision
}

// auditLog records a bot-interaction-API event through the regular
// application log.
func (h *BotAPIHandler) auditLog(botUUID, action string, details map[string]any) {
	logrus.WithFields(logrus.Fields(appendBot(details, botUUID))).WithField("action", action).Info(action)
}

func appendBot(details map[string]any, botUUID string) map[string]any {
	if details == nil {
		details = map[string]any{}
	}
	details["bot"] = botUUID
	return details
}

func validKind(k interaction.Kind) bool {
	switch k {
	case interaction.KindConfirm, interaction.KindChoose, interaction.KindAsk:
		return true
	}
	return false
}

func interactBudget(seconds int) time.Duration {
	if seconds <= 0 {
		return DefaultInteractTimeout
	}
	d := time.Duration(seconds) * time.Second
	if d > MaxInteractTimeout {
		return MaxInteractTimeout
	}
	return d
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is a broken host; fall back to a timestamp-ish
		// id so the request still proceeds rather than 500-ing.
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
