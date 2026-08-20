package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestOpenAIChatToolMessageImagePartRoundTrip is the ingress-level regression
// for issue #1606: an OpenAI Chat Completions request whose role:"tool"
// message carries a multimodal content array must survive the gateway's
// parse → re-marshal round trip byte-equivalent in meaning. Before the fix the
// SDK union for tool message content only admitted text parts, so the
// image_url part was re-serialized as {"type":"image_url","text":""} — the
// payload was dropped and upstreams rejected the request with 400.
func TestOpenAIChatToolMessageImagePartRoundTrip(t *testing.T) {
	const imageURL = "data:image/png;base64,iVBORw0KGgo="
	raw := []byte(`{
		"model": "test-model",
		"stream": false,
		"messages": [
			{"role": "user", "content": "analyze the image"},
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "call_1", "type": "function",
				 "function": {"name": "vision_analyze", "arguments": "{}"}}
			]},
			{"role": "tool", "tool_call_id": "call_1", "content": [
				{"type": "text", "text": "Image loaded. What color is it?"},
				{"type": "image_url", "image_url": {"url": "` + imageURL + `"}}
			]}
		]
	}`)

	var req OpenAIChatCompletionRequest
	require.NoError(t, json.Unmarshal(raw, &req))

	out, err := json.Marshal(&req)
	require.NoError(t, err)

	parts := gjson.GetBytes(out, "messages.2.content")
	require.True(t, parts.IsArray(), "tool message content should stay an array, got: %s", parts.Raw)
	require.Len(t, parts.Array(), 2, "both content parts must survive")

	textPart := parts.Array()[0]
	assert.Equal(t, "text", textPart.Get("type").Str)
	assert.Equal(t, "Image loaded. What color is it?", textPart.Get("text").Str)

	imagePart := parts.Array()[1]
	assert.Equal(t, "image_url", imagePart.Get("type").Str)
	assert.Equal(t, imageURL, imagePart.Get("image_url.url").Str,
		"image_url.url payload must survive the round trip")
	assert.False(t, imagePart.Get("text").Exists(),
		"image part must not grow a bogus text field")
}

// TestOpenAIChatToolMessageImageOnlyRoundTrip covers the image-only list
// content row of the issue's control matrix.
func TestOpenAIChatToolMessageImageOnlyRoundTrip(t *testing.T) {
	const imageURL = "https://example.com/shot.png"
	raw := []byte(`{
		"model": "test-model",
		"messages": [
			{"role": "tool", "tool_call_id": "call_1", "content": [
				{"type": "image_url", "image_url": {"url": "` + imageURL + `"}}
			]}
		]
	}`)

	var req OpenAIChatCompletionRequest
	require.NoError(t, json.Unmarshal(raw, &req))
	out, err := json.Marshal(&req)
	require.NoError(t, err)

	parts := gjson.GetBytes(out, "messages.0.content")
	require.True(t, parts.IsArray())
	require.Len(t, parts.Array(), 1)
	assert.Equal(t, "image_url", parts.Array()[0].Get("type").Str)
	assert.Equal(t, imageURL, parts.Array()[0].Get("image_url.url").Str)
}
