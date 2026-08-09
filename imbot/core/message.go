package core

import (
	"strings"
)

// Content represents the message content interface
type Content interface {
	ContentType() string
}

// Message represents a unified message from any platform
type Message struct {
	ID            string                 `json:"id"`
	Platform      Platform               `json:"platform"`
	Timestamp     int64                  `json:"timestamp"`
	Sender        Sender                 `json:"sender"`
	Recipient     Recipient              `json:"recipient"`
	Content       Content                `json:"content"`
	ChatType      ChatType               `json:"chatType"`
	ThreadContext *ThreadContext         `json:"threadContext,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	// Payload carries the segments of the action the user activated, for
	// messages that are button presses rather than typed input. Empty for
	// ordinary messages. Platforms fill it in their inbound adapter, which is
	// where the platform's own encoding stops being visible.
	Payload Payload `json:"payload,omitempty"`
}

// IsCallback reports whether this message is an action firing rather than
// user-typed content.
func (m *Message) IsCallback() bool {
	if !m.Payload.IsEmpty() {
		return true
	}
	isCallback, _ := m.Metadata["is_callback"].(bool)
	return isCallback
}

// TextContent represents text message content
type TextContent struct {
	Text     string    `json:"text"`
	Entities []Entity  `json:"entities,omitempty"`
	// Segments preserves an ordered multi-segment body (e.g. interleaved
	// body + thinking) on inbound messages. Nil for ordinary single-text
	// messages. See core.Segment.
	Segments []Segment `json:"segments,omitempty"`
}

func (c *TextContent) ContentType() string { return "text" }

// NewTextContent creates a new TextContent
func NewTextContent(text string, entities ...Entity) *TextContent {
	return &TextContent{
		Text:     text,
		Entities: entities,
	}
}

// GetReplyTarget returns the reply target ID for the message.
// Different platforms may use different IDs:
// - Telegram: Recipient.ID (chat ID)
// - DingTalk/Feishu: Recipient.ID (conversation ID)
// - Discord: Recipient.ID (channel ID)
func (m *Message) GetReplyTarget() string {
	return strings.TrimSpace(m.Recipient.ID)
}

// GetText returns the text from content if it's TextContent
func (m *Message) GetText() string {
	if tc, ok := m.Content.(*TextContent); ok {
		return tc.Text
	}
	return ""
}

// IsTextContent checks if the message content is text
func (m *Message) IsTextContent() bool {
	_, ok := m.Content.(*TextContent)
	return ok
}

// MediaContent represents media message content
type MediaContent struct {
	Media   []MediaAttachment `json:"media"`
	Caption string            `json:"caption,omitempty"`
}

func (c *MediaContent) ContentType() string { return "media" }

// NewMediaContent creates a new MediaContent
func NewMediaContent(media []MediaAttachment, caption string) *MediaContent {
	return &MediaContent{
		Media:   media,
		Caption: caption,
	}
}

// GetMedia returns the media from content if it's MediaContent
func (m *Message) GetMedia() []MediaAttachment {
	if mc, ok := m.Content.(*MediaContent); ok {
		return mc.Media
	}
	return nil
}

// IsMediaContent checks if the message content is media
func (m *Message) IsMediaContent() bool {
	_, ok := m.Content.(*MediaContent)
	return ok
}

// PollContent represents poll message content
type PollContent struct {
	Poll Poll `json:"poll"`
}

func (c *PollContent) ContentType() string { return "poll" }

// NewPollContent creates a new PollContent
func NewPollContent(poll Poll) *PollContent {
	return &PollContent{Poll: poll}
}

// GetPoll returns the poll from content if it's PollContent
func (m *Message) GetPoll() *Poll {
	if pc, ok := m.Content.(*PollContent); ok {
		return &pc.Poll
	}
	return nil
}

// IsPollContent checks if the message content is a poll
func (m *Message) IsPollContent() bool {
	_, ok := m.Content.(*PollContent)
	return ok
}

// ReactionContent represents reaction message content
type ReactionContent struct {
	Reaction Reaction `json:"reaction"`
}

func (c *ReactionContent) ContentType() string { return "reaction" }

// NewReactionContent creates a new ReactionContent
func NewReactionContent(reaction Reaction) *ReactionContent {
	return &ReactionContent{Reaction: reaction}
}

// GetReaction returns the reaction from content if it's ReactionContent
func (m *Message) GetReaction() *Reaction {
	if rc, ok := m.Content.(*ReactionContent); ok {
		return &rc.Reaction
	}
	return nil
}

// IsReactionContent checks if the message content is a reaction
func (m *Message) IsReactionContent() bool {
	_, ok := m.Content.(*ReactionContent)
	return ok
}

// SystemContent represents system message content
type SystemContent struct {
	EventType string                 `json:"eventType"`
	Data      map[string]interface{} `json:"data"`
}

func (c *SystemContent) ContentType() string { return "system" }

// NewSystemContent creates a new SystemContent
func NewSystemContent(eventType string, data map[string]interface{}) *SystemContent {
	return &SystemContent{
		EventType: eventType,
		Data:      data,
	}
}

// IsSystemContent checks if the message content is a system message
func (m *Message) IsSystemContent() bool {
	_, ok := m.Content.(*SystemContent)
	return ok
}

// MediaAttachment represents a media attachment
type MediaAttachment struct {
	Type      string                 `json:"type"` // "image", "video", "audio", "document", "sticker", "gif"
	URL       string                 `json:"url"`
	MimeType  string                 `json:"mimeType,omitempty"`
	Filename  string                 `json:"filename,omitempty"`
	Title     string                 `json:"title,omitempty"`
	Size      int64                  `json:"size,omitempty"`
	Thumbnail string                 `json:"thumbnail,omitempty"`
	Width     int                    `json:"width,omitempty"`
	Height    int                    `json:"height,omitempty"`
	Duration  int                    `json:"duration,omitempty"` // for audio/video in seconds
	Raw       map[string]interface{} `json:"raw,omitempty"`
}

// Poll represents a poll
type Poll struct {
	Question  string       `json:"question"`
	Options   []PollOption `json:"options"`
	Multiple  bool         `json:"multiple,omitempty"`
	Anonymous bool         `json:"anonymous,omitempty"`
	ExpiresAt int64        `json:"expiresAt,omitempty"`
}

// PollOption represents a poll option
type PollOption struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Votes int    `json:"votes,omitempty"`
}

// Reaction represents a reaction
type Reaction struct {
	MessageID string `json:"messageId"`
	Emoji     string `json:"emoji"`
	Action    string `json:"action"` // "add" or "remove"
	UserID    string `json:"userId,omitempty"`
}

// IsGroupMessage checks if the message is from a group chat
func (m *Message) IsGroupMessage() bool {
	return m.ChatType == ChatTypeGroup || m.ChatType == ChatTypeChannel
}

// IsDirectMessage checks if the message is from a direct chat
func (m *Message) IsDirectMessage() bool {
	return m.ChatType == ChatTypeDirect
}

// IsThreadMessage checks if the message is in a thread
func (m *Message) IsThreadMessage() bool {
	return m.ChatType == ChatTypeThread || m.ThreadContext != nil
}

// GetSenderDisplayName returns the sender's display name
func (m *Message) GetSenderDisplayName() string {
	if m.Sender.DisplayName != "" {
		return m.Sender.DisplayName
	}
	if m.Sender.Username != "" {
		return m.Sender.Username
	}
	return m.Sender.ID
}

// FormatMessage formats the message for display
func (m *Message) FormatMessage() string {
	sender := m.GetSenderDisplayName()

	switch m.Content.ContentType() {
	case "text":
		return sender + ": " + m.GetText()
	case "media":
		mediaTypes := ""
		if mc, ok := m.Content.(*MediaContent); ok {
			for i, media := range mc.Media {
				if i > 0 {
					mediaTypes += ", "
				}
				mediaTypes += media.Type
			}
		}
		return sender + ": [" + mediaTypes + "]"
	case "poll":
		if poll := m.GetPoll(); poll != nil {
			return sender + ": [Poll: " + poll.Question + "]"
		}
	case "reaction":
		if reaction := m.GetReaction(); reaction != nil {
			return sender + ": [Reacted " + reaction.Emoji + "]"
		}
	case "system":
		if sc, ok := m.Content.(*SystemContent); ok {
			return sender + ": [" + sc.EventType + "]"
		}
	}

	return sender + ": [Unknown message type]"
}

// Clone creates a deep copy of the message. Maps and content variants are
// duplicated so mutating the clone (its metadata, sender raw, or any content
// map/slice) does not touch the original.
func (m *Message) Clone() *Message {
	clone := *m

	// Deep copy maps.
	clone.Metadata = cloneStringAnyMap(m.Metadata)

	// Clone sender.
	clone.Sender = m.Sender
	clone.Sender.Raw = cloneStringAnyMap(m.Sender.Raw)

	clone.Content = cloneContent(m.Content)

	return &clone
}

// cloneContent returns an independent copy of c for every content variant, or
// nil when c is nil. Content types added in the future must get a case here or
// Clone will return them shared — see TestMessage_CloneIsDeep.
func cloneContent(c Content) Content {
	switch c := c.(type) {
	case nil:
		return nil
	case *TextContent:
		return &TextContent{
			Text:     c.Text,
			Entities: cloneEntities(c.Entities),
			Segments: cloneSegments(c.Segments),
		}
	case *MediaContent:
		media := make([]MediaAttachment, len(c.Media))
		for i := range c.Media {
			media[i] = c.Media[i]
			media[i].Raw = cloneStringAnyMap(c.Media[i].Raw)
		}
		return &MediaContent{
			Media:   media,
			Caption: c.Caption,
		}
	case *PollContent:
		opts := make([]PollOption, len(c.Poll.Options))
		copy(opts, c.Poll.Options)
		return &PollContent{
			Poll: Poll{
				Question:  c.Poll.Question,
				Options:   opts,
				Multiple:  c.Poll.Multiple,
				Anonymous: c.Poll.Anonymous,
				ExpiresAt: c.Poll.ExpiresAt,
			},
		}
	case *ReactionContent:
		return &ReactionContent{Reaction: c.Reaction}
	case *SystemContent:
		return &SystemContent{
			EventType: c.EventType,
			Data:      cloneStringAnyMap(c.Data),
		}
	default:
		// Unknown Content implementation: nothing safe to copy, return as-is.
		return c
	}
}

// cloneEntities deep-copies an Entity slice, duplicating each Entity.Data map.
func cloneEntities(in []Entity) []Entity {
	if len(in) == 0 {
		return nil
	}
	out := make([]Entity, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].Data = cloneStringAnyMap(in[i].Data)
	}
	return out
}

// cloneStringAnyMap returns an independent copy of m, or nil when m is nil.
func cloneStringAnyMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// WithMetadata adds metadata to the message
func (m *Message) WithMetadata(metadata map[string]interface{}) *Message {
	if m.Metadata == nil {
		m.Metadata = make(map[string]interface{})
	}
	for k, v := range metadata {
		m.Metadata[k] = v
	}
	return m
}
