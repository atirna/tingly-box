package transform

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAlignToolMessages_OrphanKeepsImageParts guards the orphan-tool-message
// rewrite (issue #1606 family): when a tool message with multimodal content
// has no matching tool_call and is downgraded to a user message, its
// image_url parts must be carried over, not flattened to empty text.
func TestAlignToolMessages_OrphanKeepsImageParts(t *testing.T) {
	const imageURL = "data:image/png;base64,iVBORw0KGgo="
	raw := []byte(`{
		"model": "test-model",
		"messages": [
			{"role": "tool", "tool_call_id": "call_missing", "content": [
				{"type": "text", "text": "Image loaded."},
				{"type": "image_url", "image_url": {"url": "` + imageURL + `"}}
			]}
		]
	}`)
	req := &openai.ChatCompletionNewParams{}
	require.NoError(t, json.Unmarshal(raw, req))

	AlignToolMessagesForOpenAI(req)

	require.Len(t, req.Messages, 1)
	user := req.Messages[0].OfUser
	require.NotNil(t, user, "orphan tool message should become a user message")

	parts := user.Content.OfArrayOfContentParts
	require.Len(t, parts, 2, "text + image parts must both survive the downgrade")
	require.NotNil(t, parts[0].OfText)
	assert.Equal(t, "Image loaded.", parts[0].OfText.Text)
	require.NotNil(t, parts[1].OfImageURL, "image part must survive the downgrade")
	assert.Equal(t, imageURL, parts[1].OfImageURL.ImageURL.URL)
}

// TestAlignToolMessages_MatchedToolMessageUntouched confirms the aligner does
// not rewrite tool messages that do have a matching tool_call — their
// multimodal content must pass through bit-identical.
func TestAlignToolMessages_MatchedToolMessageUntouched(t *testing.T) {
	const imageURL = "data:image/png;base64,iVBORw0KGgo="
	raw := []byte(`{
		"model": "test-model",
		"messages": [
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "call_1", "type": "function",
				 "function": {"name": "vision_analyze", "arguments": "{}"}}
			]},
			{"role": "tool", "tool_call_id": "call_1", "content": [
				{"type": "image_url", "image_url": {"url": "` + imageURL + `"}}
			]}
		]
	}`)
	req := &openai.ChatCompletionNewParams{}
	require.NoError(t, json.Unmarshal(raw, req))

	AlignToolMessagesForOpenAI(req)

	require.Len(t, req.Messages, 2)
	tool := req.Messages[1].OfTool
	require.NotNil(t, tool, "matched tool message must stay a tool message")

	out, err := json.Marshal(req.Messages[1])
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(out, &m))
	parts, ok := m["content"].([]any)
	require.True(t, ok, "tool content should stay an array, got %v", m["content"])
	require.Len(t, parts, 1)
	img := parts[0].(map[string]any)
	assert.Equal(t, "image_url", img["type"])
	imgURL, ok := img["image_url"].(map[string]any)
	require.True(t, ok, "image_url payload must survive, got %v", img)
	assert.Equal(t, imageURL, imgURL["url"])
}
