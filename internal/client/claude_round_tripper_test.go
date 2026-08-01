package client

import (
	"net/http"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
)

// mockTransport is a minimal http.RoundTripper for testing.
type mockTransport struct {
	roundTrip func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTrip(req)
}

// Test that the SDK middleware approach still applies the correct headers
func TestClaudeSDKHeaders(t *testing.T) {
	assert.Equal(t, "claude-cli/2.1.86 (external, cli)", claudeCLIUserAgent)
	assert.Contains(t, claudeCLIUserAgent, "2.1.86")
	assert.Equal(t, "v24.3.0", stainlessRuntimeVersion)
	assert.Equal(t, "cli", claudeXApp)
	assert.Equal(t, "600", stainlessTimeout)
}

func TestAnthropicBetaFlags(t *testing.T) {
	for _, flag := range []string{
		"claude-code-20250219",
		"oauth-2025-04-20",
		"interleaved-thinking-2025-05-14",
		"structured-outputs-2025-12-15",
		"fast-mode-2026-02-01",
		"redact-thinking-2026-02-12",
		"token-efficient-tools-2026-03-28",
	} {
		assert.Contains(t, anthropicBeta, flag, "anthropicBeta should contain %s", flag)
	}
}

func TestRemapToolNames(t *testing.T) {
	t.Run("renames bash to Bash in OfTool", func(t *testing.T) {
		tools := []anthropic.ToolUnionParam{
			{OfTool: &anthropic.ToolParam{Name: "bash"}},
		}
		rev := remapToolNames(tools)
		assert.Equal(t, "Bash", tools[0].OfTool.Name)
		assert.Equal(t, map[string]string{"Bash": "bash"}, rev)
	})

	t.Run("skips built-in tools (OfTool is nil)", func(t *testing.T) {
		tools := []anthropic.ToolUnionParam{
			{OfBashTool20250124: &anthropic.ToolBash20250124Param{}},
		}
		rev := remapToolNames(tools)
		assert.Empty(t, rev)
	})

	t.Run("already TitleCase — no rename", func(t *testing.T) {
		tools := []anthropic.ToolUnionParam{
			{OfTool: &anthropic.ToolParam{Name: "Bash"}},
		}
		rev := remapToolNames(tools)
		assert.Equal(t, "Bash", tools[0].OfTool.Name)
		assert.Empty(t, rev)
	})

	t.Run("unknown tool — passed through unchanged", func(t *testing.T) {
		tools := []anthropic.ToolUnionParam{
			{OfTool: &anthropic.ToolParam{Name: "my_custom_tool"}},
		}
		rev := remapToolNames(tools)
		assert.Equal(t, "my_custom_tool", tools[0].OfTool.Name)
		assert.Empty(t, rev)
	})
}

func TestRestoreToolNamesInMessage(t *testing.T) {
	t.Run("restores tool_use name", func(t *testing.T) {
		msg := &anthropic.Message{
			Content: []anthropic.ContentBlockUnion{
				{Type: "tool_use", Name: "Bash"},
			},
		}
		restoreToolNamesInMessage(msg, map[string]string{"Bash": "bash"})
		assert.Equal(t, "bash", msg.Content[0].Name)
	})

	t.Run("noop for nil reverseMap", func(t *testing.T) {
		msg := &anthropic.Message{
			Content: []anthropic.ContentBlockUnion{
				{Type: "tool_use", Name: "Bash"},
			},
		}
		restoreToolNamesInMessage(msg, nil)
		assert.Equal(t, "Bash", msg.Content[0].Name)
	})

	t.Run("does not touch non-tool_use blocks", func(t *testing.T) {
		msg := &anthropic.Message{
			Content: []anthropic.ContentBlockUnion{
				{Type: "text", Name: ""},
			},
		}
		restoreToolNamesInMessage(msg, map[string]string{"Bash": "bash"})
		assert.Equal(t, "", msg.Content[0].Name)
	})
}
