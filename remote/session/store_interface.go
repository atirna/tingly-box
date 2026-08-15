package session

import "context"

// SessionStore defines the interface for session persistence, keeping this
// package independent of where sessions are actually stored
type SessionStore interface {
	// Get retrieves a session by ID
	Get(ctx context.Context, sessionID string) (*Session, error)

	// Set stores a session
	Set(ctx context.Context, sessionID string, sess *Session) error

	// Delete removes a session
	Delete(ctx context.Context, sessionID string) error

	// List returns all sessions
	List(ctx context.Context) []*Session

	// FindByChatAgentProject finds a session by (chatID, agent, project) tuple
	FindByChatAgentProject(ctx context.Context, chatID, agent, project string) (*Session, error)

	// ListByChat lists all sessions for a given chat ID
	ListByChat(ctx context.Context, chatID string) ([]*Session, error)

	// AppendMessage adds one message to a session's transcript. Separate from
	// Set because the transcript is append-only and lives outside the session
	// index — see Transcript for why history is not stored as rows. Not
	// ctx-aware: the backing implementation is plain file I/O, not gorm.
	AppendMessage(sessionID string, msg Message) error

	// Messages returns a session's full history, read on demand. Not
	// ctx-aware, for the same reason as AppendMessage.
	Messages(sessionID string) ([]Message, error)
}
