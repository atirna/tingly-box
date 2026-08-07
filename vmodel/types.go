package vmodel

// VirtualModelType represents the type/category of a virtual model.
type VirtualModelType string

const (
	// VirtualModelTypeStatic represents static mock models that return fixed responses.
	VirtualModelTypeStatic VirtualModelType = "static"

	// VirtualModelTypeProxy represents proxy/transform models that modify requests before forwarding.
	VirtualModelTypeProxy VirtualModelType = "proxy"

	// VirtualModelTypeTool represents tool models that return tool_use blocks.
	VirtualModelTypeTool VirtualModelType = "tool"

	// VirtualModelTypeSequence represents sequence models that walk a configured
	// program of per-request outcomes (e.g. 200, 200, 429) to simulate a flaky
	// upstream provider.
	VirtualModelTypeSequence VirtualModelType = "sequence"
)

// Model represents a virtual model in the models list (OpenAI-compatible format).
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ToolCallConfig defines a tool call to be returned by the virtual model.
type ToolCallConfig struct {
	Name      string                 `json:"name" yaml:"name"`
	Arguments map[string]interface{} `json:"arguments" yaml:"arguments"`

	// Once makes the tool call a one-shot: the model emits it only while the
	// conversation carries no tool result yet, and answers with Content once
	// one arrives.
	//
	// Without this a tool mock is stateless and re-requests the same call
	// every round, so any server-side tool loop spins until its round budget
	// runs out and the client gets an empty response. Once is what lets a
	// virtual model stand in for "model asks for a tool, reads the result,
	// then answers" — the shape every tool-loop feature (MCP, web proxy) has
	// to be exercised against.
	Once bool `json:"once,omitempty" yaml:"once,omitempty"`
}

// ToolCallDisplayContent extracts display text from tool call arguments.
// It checks for "message" and "question" keys, returning the first non-empty value found.
func ToolCallDisplayContent(args map[string]interface{}) string {
	if msg, ok := args["message"].(string); ok {
		return msg
	}
	if question, ok := args["question"].(string); ok {
		return question
	}
	return ""
}
