package client

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestHoistCodexSystemMessagesJSON_MovesSystemToInstructions(t *testing.T) {
	body := `{
		"model": "gpt-5-codex",
		"instructions": "You are a helpful AI assistant.",
		"input": [
			{"type": "message", "role": "system", "content": [{"type": "input_text", "text": "You are Claude Code.", "prompt_cache_breakpoint": {"type": "persistent"}}]},
			{"type": "message", "role": "user", "content": "hello"}
		]
	}`

	out := hoistCodexSystemMessagesJSON(body)

	assert.Equal(t, "You are Claude Code.", gjson.Get(out, "instructions").String(),
		"system text must replace the default instructions placeholder")
	input := gjson.Get(out, "input").Array()
	require.Len(t, input, 1)
	assert.Equal(t, "user", input[0].Get("role").String())
}

func TestHoistCodexSystemMessagesJSON_AppendsToRealInstructions(t *testing.T) {
	body := `{
		"instructions": "Base instructions.",
		"input": [
			{"type": "message", "role": "system", "content": "First."},
			{"type": "message", "role": "developer", "content": "Second."},
			{"type": "message", "role": "user", "content": "hi"}
		]
	}`

	out := hoistCodexSystemMessagesJSON(body)

	assert.Equal(t, "Base instructions.\n\nFirst.\n\nSecond.", gjson.Get(out, "instructions").String(),
		"hoisted text must preserve original message order")
	input := gjson.Get(out, "input").Array()
	require.Len(t, input, 1)
	assert.Equal(t, "user", input[0].Get("role").String())
}

func TestHoistCodexSystemMessagesJSON_NoOpWithoutSystemMessages(t *testing.T) {
	body := `{"instructions":"Base.","input":[{"type":"message","role":"user","content":"hi"}]}`

	out := hoistCodexSystemMessagesJSON(body)

	assert.Equal(t, body, out)
}

func TestHoistCodexSystemMessagesJSON_NonArrayInput(t *testing.T) {
	body := `{"model":"gpt-5-codex","input":"hello"}`

	out := hoistCodexSystemMessagesJSON(body)

	assert.Equal(t, body, out)
}

func TestStripCodexPromptCacheFieldsJSON(t *testing.T) {
	body := `{
		"prompt_cache_options": {"mode": "explicit"},
		"input": [
			{"type": "message", "role": "user", "content": [
				{"type": "input_text", "text": "hi", "prompt_cache_breakpoint": {"type": "persistent"}}
			]},
			{"type": "function_call_output", "call_id": "c1", "output": [
				{"type": "input_text", "text": "result", "prompt_cache_breakpoint": {"type": "persistent"}}
			]}
		]
	}`

	out := stripCodexPromptCacheFieldsJSON(body)

	assert.False(t, gjson.Get(out, "prompt_cache_options").Exists())
	assert.NotContains(t, out, "prompt_cache_breakpoint")
	assert.Equal(t, "hi", gjson.Get(out, "input.0.content.0.text").String(), "content itself must be preserved")
	assert.Equal(t, "result", gjson.Get(out, "input.1.output.0.text").String())
}

func TestStripCodexPromptCacheFieldsJSON_NoOpWithoutCacheFields(t *testing.T) {
	body := `{"model":"gpt-5-codex","input":[{"type":"message","role":"user","content":"hi"}]}`

	out := stripCodexPromptCacheFieldsJSON(body)

	assert.Equal(t, body, out)
}

func TestSanitizeCodexInputIDsJSON_DropsRequiredEmptyID(t *testing.T) {
	body := `{
		"model": "gpt-5-codex",
		"input": [
			{"type": "message", "role": "user", "content": "hello"},
			{"type": "reasoning", "id": "", "summary": []},
			{"type": "reasoning", "id": "rs_abc123", "summary": []}
		]
	}`

	out := sanitizeCodexInputIDsJSON(body)

	input := gjson.Get(out, "input").Array()
	assert.Len(t, input, 2, "reasoning item with empty id must be dropped")
	assert.Equal(t, "message", input[0].Get("type").String())
	assert.Equal(t, "reasoning", input[1].Get("type").String())
	assert.Equal(t, "rs_abc123", input[1].Get("id").String())
}

func TestSanitizeCodexInputIDsJSON_ClearsOptionalEmptyID(t *testing.T) {
	body := `{
		"input": [
			{"type": "function_call", "id": "", "call_id": "c1", "name": "f", "arguments": "{}"},
			{"type": "shell_call", "id": "", "call_id": "c2", "action": {}},
			{"type": "apply_patch_call", "id": "", "call_id": "c3", "operation": {}}
		]
	}`

	out := sanitizeCodexInputIDsJSON(body)

	input := gjson.Get(out, "input").Array()
	assert.Len(t, input, 3, "optional-id items must not be dropped")
	for _, item := range input {
		assert.False(t, item.Get("id").Exists(), "empty id should be deleted from item type %s", item.Get("type").String())
	}
}

func TestSanitizeCodexInputIDsJSON_DropsInvalidChars(t *testing.T) {
	body := `{
		"input": [
			{"type": "reasoning", "id": "rs/bad chars!", "summary": []},
			{"type": "function_call", "id": "fc/bad", "call_id": "c1", "name": "f", "arguments": "{}"}
		]
	}`

	out := sanitizeCodexInputIDsJSON(body)

	input := gjson.Get(out, "input").Array()
	assert.Len(t, input, 1, "reasoning with invalid chars must be dropped")
	assert.Equal(t, "function_call", input[0].Get("type").String())
	assert.False(t, input[0].Get("id").Exists(), "function_call id with invalid chars must be cleared")
}

func TestSanitizeCodexInputIDsJSON_NoOpWhenAllValid(t *testing.T) {
	body := `{"input":[{"type":"reasoning","id":"rs_abc","summary":[]},{"type":"function_call","id":"fc_123","call_id":"c","name":"f","arguments":"{}"}]}`

	out := sanitizeCodexInputIDsJSON(body)

	assert.Equal(t, body, out, "valid ids must not be modified")
}

func TestSanitizeCodexInputIDsJSON_NoInputArray(t *testing.T) {
	body := `{"model":"gpt-5-codex","input":"hello"}`

	out := sanitizeCodexInputIDsJSON(body)

	assert.Equal(t, body, out, "non-array input must be passed through")
}

func TestSanitizeCodexInputIDsJSON_WhitespaceOnlyID(t *testing.T) {
	body := `{"input":[{"type":"function_call_output","id":"   ","call_id":"c1","output":"ok"}]}`

	out := sanitizeCodexInputIDsJSON(body)

	input := gjson.Get(out, "input").Array()
	assert.Len(t, input, 1)
	assert.False(t, input[0].Get("id").Exists(), "whitespace-only id should be cleared")
}

func TestSanitizeCodexInputIDsJSON_HighIndex(t *testing.T) {
	// Mirrors the production failure: input[186].id == ""
	var items []string
	for i := 0; i < 186; i++ {
		items = append(items, `{"type":"message","role":"user","content":"x"}`)
	}
	items = append(items, `{"type":"shell_call","id":"","call_id":"c","action":{}}`)
	body := `{"input":[` + strings.Join(items, ",") + `]}`

	out := sanitizeCodexInputIDsJSON(body)

	input := gjson.Get(out, "input").Array()
	assert.Len(t, input, 187)
	assert.False(t, input[186].Get("id").Exists())
}

func TestSanitizeCodexEmptyContentJSON_DropsEmptyStringContent(t *testing.T) {
	body := `{
		"input": [
			{"type": "message", "role": "user", "content": "hello"},
			{"type": "message", "role": "assistant", "content": ""},
			{"type": "message", "role": "user", "content": ""},
			{"type": "function_call_output", "call_id": "c1", "output": ""}
		]
	}`

	out := sanitizeCodexEmptyContentJSON(body)

	input := gjson.Get(out, "input").Array()
	assert.Len(t, input, 2, "message items with empty string content must be dropped")
	assert.Equal(t, "hello", input[0].Get("content").String())
	assert.Equal(t, "function_call_output", input[1].Get("type").String(), "non-message items with empty content must be kept")
}

func TestSanitizeCodexEmptyContentJSON_KeepsNonEmptyContent(t *testing.T) {
	body := `{"input":[{"type":"message","role":"user","content":"hi"},{"type":"message","role":"assistant","content":"hello"}]}`

	out := sanitizeCodexEmptyContentJSON(body)

	assert.Equal(t, body, out, "non-empty content must not be modified")
}

func TestSanitizeCodexEmptyContentJSON_NoInputArray(t *testing.T) {
	body := `{"model":"gpt-5-codex","input":"hello"}`

	out := sanitizeCodexEmptyContentJSON(body)

	assert.Equal(t, body, out)
}

func TestSanitizeCodexEmptyContentJSON_HighIndex(t *testing.T) {
	// Mirrors the production failure: input[1012].content == ""
	var items []string
	for i := 0; i < 1012; i++ {
		items = append(items, `{"type":"message","role":"user","content":"x"}`)
	}
	items = append(items, `{"type":"message","role":"assistant","content":""}`)
	body := `{"input":[` + strings.Join(items, ",") + `]}`

	out := sanitizeCodexEmptyContentJSON(body)

	input := gjson.Get(out, "input").Array()
	assert.Len(t, input, 1012, "empty-content message at high index must be dropped")
}

func TestCodexInputItemIDRequired(t *testing.T) {
	required := []string{
		"reasoning", "code_interpreter_call", "computer_call", "file_search_call",
		"web_search_call", "image_generation_call", "local_shell_call",
		"local_shell_call_output", "mcp_list_tools", "mcp_approval_request",
		"mcp_call", "item_reference",
	}
	optional := []string{
		"function_call", "function_call_output", "shell_call", "shell_call_output",
		"apply_patch_call", "apply_patch_call_output", "computer_call_output",
		"custom_tool_call", "custom_tool_call_output", "mcp_approval_response",
		"compaction", "message", "output_message",
	}
	for _, typ := range required {
		assert.True(t, codexInputItemIDRequired(typ), "type %q should require id", typ)
	}
	for _, typ := range optional {
		assert.False(t, codexInputItemIDRequired(typ), "type %q should not require id", typ)
	}
}

func TestValidateCodexStreamResponse_AllowsSSE(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}},
		Body:   io.NopCloser(strings.NewReader("data: {}\n\n")),
	}

	require.NoError(t, validateCodexStreamResponse(resp))
}

func TestValidateCodexStreamResponse_AllowsMissingContentType(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{},
		Body:   io.NopCloser(strings.NewReader("data: {}\n\n")),
	}

	require.NoError(t, validateCodexStreamResponse(resp))
}

func TestValidateCodexStreamResponse_RejectsJSON200(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(`{"error":"maintenance"}`)),
	}

	err := validateCodexStreamResponse(resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-SSE 200 response")
	assert.Contains(t, err.Error(), "maintenance")
}

func TestValidateCodexStreamResponse_AllowsAmbiguousNonSSEContentType(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/plain"}},
		Body:   io.NopCloser(strings.NewReader("data: {}\n\n")),
	}

	require.NoError(t, validateCodexStreamResponse(resp))
}
