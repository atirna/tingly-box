package client

// contextKey is the client package's private context-key type.
type contextKey string

// ScenarioContextKey carries the request's scenario name into the outbound
// request context (set by the protocol server's routing middleware). Consumed
// by servertool hooks today; the wire-level recorder planned in
// .design/recording.md §4 will read it too.
const ScenarioContextKey contextKey = "scenario"
