package client

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTitleCaseToolName(t *testing.T) {
	cases := map[string]string{
		"read_file":     "ReadFile",
		"ha_get_state":  "HaGetState",
		"terminal":      "Terminal",
		"browser_exec":  "BrowserExec",
		"Bash":          "Bash", // already TitleCase
		"WebFetch":      "WebFetch",
		"":              "",
		"mcp_foo_bar":   "mcp_Foo_bar", // prefix kept, only first char folded
		"mcp_":          "mcp_",
		"tool_2_thing":  "Tool2Thing",
		"__weird__name": "WeirdName",
	}
	for in, want := range cases {
		assert.Equal(t, want, titleCaseToolName(in), "titleCaseToolName(%q)", in)
	}
}

func TestClaudeCodeToolName_MapWinsOverMechanical(t *testing.T) {
	// The official spelling must win: mechanical folding would give "Ls".
	assert.Equal(t, "LS", claudeCodeToolName("ls"))
	assert.Equal(t, "TodoWrite", claudeCodeToolName("todowrite"))
	assert.Equal(t, "NotebookEdit", claudeCodeToolName("notebookedit"))
	// Not in the map — mechanical fold.
	assert.Equal(t, "SearchFiles", claudeCodeToolName("search_files"))
}

func TestPlanToolRenames_SkipsCollisions(t *testing.T) {
	t.Run("target already taken by another tool", func(t *testing.T) {
		plan := planToolRenames([]string{"my_tool", "MyTool"})
		assert.Empty(t, plan)
	})

	t.Run("two sources folding to the same target", func(t *testing.T) {
		// "a_b" and "a__b" both fold to "AB"; only the first may rename.
		plan := planToolRenames([]string{"a_b", "a__b"})
		assert.Len(t, plan, 1)
		assert.Equal(t, "AB", plan["a_b"])
	})

	t.Run("independent names all rename", func(t *testing.T) {
		plan := planToolRenames([]string{"read_file", "write_file"})
		assert.Equal(t, map[string]string{
			"read_file":  "ReadFile",
			"write_file": "WriteFile",
		}, plan)
	})
}

// sseLine builds one SSE event frame.
func sseLine(event string, payload any) string {
	b, _ := json.Marshal(payload)
	return "event: " + event + "\ndata: " + string(b) + "\n\n"
}

func TestSSEToolNameRewriter(t *testing.T) {
	reverse := map[string]string{"ReadFile": "read_file"}

	t.Run("rewrites tool_use name in content_block_start", func(t *testing.T) {
		body := sseLine("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type":  "tool_use",
				"id":    "toolu_1",
				"name":  "ReadFile",
				"input": map[string]any{},
			},
		})
		out := readAll(t, body, reverse)
		assert.Contains(t, out, `"name":"read_file"`)
		assert.NotContains(t, out, "ReadFile")
		// Framing preserved.
		assert.True(t, strings.HasPrefix(out, "event: content_block_start\ndata: "))
		assert.True(t, strings.HasSuffix(out, "\n\n"))
	})

	t.Run("passes through unrelated events byte-for-byte", func(t *testing.T) {
		body := sseLine("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"delta": map[string]any{"type": "input_json_delta", "partial_json": `{"path":`},
		}) + sseLine("message_stop", map[string]any{"type": "message_stop"})
		assert.Equal(t, body, readAll(t, body, reverse))
	})

	t.Run("leaves names absent from the reverse map alone", func(t *testing.T) {
		body := sseLine("content_block_start", map[string]any{
			"type": "content_block_start",
			"content_block": map[string]any{
				"type": "tool_use", "id": "toolu_2", "name": "SomethingElse",
			},
		})
		assert.Equal(t, body, readAll(t, body, reverse))
	})

	t.Run("text content_block_start untouched", func(t *testing.T) {
		body := sseLine("content_block_start", map[string]any{
			"type":          "content_block_start",
			"content_block": map[string]any{"type": "text", "text": ""},
		})
		assert.Equal(t, body, readAll(t, body, reverse))
	})

	t.Run("handles a multi-event stream and tiny reads", func(t *testing.T) {
		body := sseLine("message_start", map[string]any{"type": "message_start"}) +
			sseLine("content_block_start", map[string]any{
				"type": "content_block_start",
				"content_block": map[string]any{
					"type": "tool_use", "id": "toolu_3", "name": "ReadFile",
				},
			}) +
			sseLine("message_stop", map[string]any{"type": "message_stop"})

		r := newSSEToolNameRewriter(io.NopCloser(strings.NewReader(body)), reverse)
		var sb strings.Builder
		buf := make([]byte, 3) // force many partial reads
		for {
			n, err := r.Read(buf)
			sb.Write(buf[:n])
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
		}
		out := sb.String()
		assert.Contains(t, out, `"name":"read_file"`)
		assert.Contains(t, out, "message_start")
		assert.Contains(t, out, "message_stop")
	})

	t.Run("malformed json passes through", func(t *testing.T) {
		body := "data: {\"type\":\"content_block_start\",\"tool_use\" BROKEN\n\n"
		assert.Equal(t, body, readAll(t, body, reverse))
	})

	t.Run("empty reverse map is inert", func(t *testing.T) {
		body := sseLine("content_block_start", map[string]any{
			"type": "content_block_start",
			"content_block": map[string]any{
				"type": "tool_use", "id": "toolu_4", "name": "ReadFile",
			},
		})
		assert.Equal(t, body, readAll(t, body, map[string]string{}))
	})
}

func readAll(t *testing.T, body string, reverse map[string]string) string {
	t.Helper()
	r := newSSEToolNameRewriter(io.NopCloser(strings.NewReader(body)), reverse)
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	return string(out)
}
