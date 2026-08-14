package ops

import (
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/tingly-dev/tingly-box/internal/protocol"
)

// TestProviderDispatchDoesNotMatchHostnameMentionedElsewhereInURL proves the
// fix for the old strings.Contains(url, "...") dispatch: a base URL whose
// path or query merely *mentions* a vendor's hostname as text (e.g. a proxy
// relaying to a target named in a query parameter) must not be mistaken for
// that vendor. Only the parsed host is matched.
func TestProviderDispatchDoesNotMatchHostnameMentionedElsewhereInURL(t *testing.T) {
	msg := assistantToolCallMessage(t)
	msg.OfAssistant.SetExtraFields(map[string]any{"x_thinking": "should not be converted"})

	req := &openai.ChatCompletionNewParams{
		Model:    openai.ChatModel("gpt-4o"),
		Messages: []openai.ChatCompletionMessageParamUnion{msg},
	}

	ApplyProviderTransforms(req, "https://gateway.example.com/relay?target=api.deepseek.com", string(req.Model), &protocol.OpenAIConfig{})

	raw := marshalMessage(t, req.Messages[0])
	assert.Equal(t, "should not be converted", raw["x_thinking"],
		"a URL that merely mentions a vendor hostname in its path/query must not trigger that vendor's transform")
	assert.NotContains(t, raw, "reasoning_content")
}

// TestProviderDispatchMatchesBareHostnameWithoutScheme proves dispatch still
// works when Provider.APIBase is stored without a scheme (e.g.
// "api.deepseek.com" rather than "https://api.deepseek.com"), which
// net/url.Parse alone would treat as a relative path, not a host.
func TestProviderDispatchMatchesBareHostnameWithoutScheme(t *testing.T) {
	req := &openai.ChatCompletionNewParams{
		Model: openai.ChatModel("deepseek-v4-flash"),
	}

	ApplyProviderTransforms(req, "api.deepseek.com", string(req.Model), &protocol.OpenAIConfig{
		HasThinking:     true,
		ReasoningEffort: "high",
	})

	assert.Equal(t, "high", string(req.ReasoningEffort))
}
