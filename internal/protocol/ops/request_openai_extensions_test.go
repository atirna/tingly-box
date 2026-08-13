package ops

import (
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/tingly-dev/tingly-box/internal/protocol"
)

func TestSplitProviderHostPath(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantHost string
		wantPath string
	}{
		{"scheme + path", "https://api.deepseek.com/v1", "api.deepseek.com", "/v1"},
		{"bare host, no scheme", "api.deepseek.com", "api.deepseek.com", ""},
		{"bare host + path, no scheme", "opencode.ai/zen/go", "opencode.ai", "/zen/go"},
		{"port stripped", "https://api.deepseek.com:8443/v1", "api.deepseek.com", "/v1"},
		{"userinfo stripped", "https://user:pass@api.deepseek.com/v1", "api.deepseek.com", "/v1"},
		{"uppercase normalized", "HTTPS://API.DeepSeek.COM/V1", "api.deepseek.com", "/v1"},
		{"query string kept in path", "https://gateway.example.com/relay?target=api.deepseek.com", "gateway.example.com", "/relay?target=api.deepseek.com"},
		{"bracketed IPv6 host + port", "https://[::1]:8080/v1", "[::1]", "/v1"},
		{"bracketed IPv6 host, no port", "https://[2001:db8::1]/v1", "[2001:db8::1]", "/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, path := splitProviderHostPath(tt.url)
			assert.Equal(t, tt.wantHost, host)
			assert.Equal(t, tt.wantPath, path)
		})
	}
}

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
