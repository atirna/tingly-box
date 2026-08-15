package wire

// AnthropicMsgWire is the intermediate JSON representation used to build
// anthropic.Message / anthropic.BetaMessage via marshal+unmarshal, which is
// necessary because the SDK content union types have no public constructors.
type AnthropicMsgWire struct {
	ID            string             `json:"id"`
	Type          string             `json:"type"`
	Role          string             `json:"role"`
	Content       interface{}        `json:"content"`
	Model         string             `json:"model"`
	StopReason    string             `json:"stop_reason"`
	StopSequence  string             `json:"stop_sequence"`
	Usage         AnthropicUsageWire `json:"usage"`
	ServerToolUse interface{}        `json:"server_tool_use,omitempty"`
}

// AnthropicUsageWire represents the Anthropic usage wire format.
// input_tokens = uncached only; cache_read and cache_creation are separate.
type AnthropicUsageWire struct {
	InputTokens              int64                             `json:"input_tokens"`
	OutputTokens             int64                             `json:"output_tokens"`
	CacheReadInputTokens     int64                             `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64                             `json:"cache_creation_input_tokens,omitempty"`
	OutputTokensDetails      *AnthropicOutputTokensDetailsWire `json:"output_tokens_details,omitempty"`
}

// AnthropicOutputTokensDetailsWire mirrors Anthropic's own
// usage.output_tokens_details (added alongside claude-opus-4-8):
// thinking_tokens is a subset of output_tokens, same relationship as
// OpenAI's completion_tokens_details.reasoning_tokens.
type AnthropicOutputTokensDetailsWire struct {
	ThinkingTokens int64 `json:"thinking_tokens"`
}
