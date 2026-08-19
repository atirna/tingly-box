package stream

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/openai/openai-go/v3"
	openaistream "github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tingly-dev/tingly-box/internal/protocol"
)

// truncatedResponsesEvents is a Responses API stream that delivers real
// partial text and then ends without any terminal event — the #1384 shape
// ("upstream stream ended before completion" on the Anthropic-target path).
func truncatedResponsesEvents() []string {
	return eventsToStrings([]map[string]any{
		{"type": "response.created", "response": map[string]any{"id": "resp_truncated"}},
		{"type": "response.output_text.delta", "item_id": "item_1", "output_index": 0, "delta": "partial"},
	})
}

// errFakeResponsesDecoder replays events, then reports a transport read error
// (the upstream tore the connection down mid-stream).
type errFakeResponsesDecoder struct {
	*fakeResponsesDecoder
	err error
}

func (f *errFakeResponsesDecoder) Err() error { return f.err }

// errFakeChatDecoder is the Chat Completions counterpart.
type errFakeChatDecoder struct {
	*fakeChatDecoder
	err error
}

func (f *errFakeChatDecoder) Err() error { return f.err }

func newAnthropicTestContext(t *testing.T) (*gin.Context, *closeNotifyRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	return c, w
}

// TestHandleResponsesToAnthropicStream_TruncatedSalvaged: with the
// salvage_truncated_stream flag on, a Responses upstream that dies after
// partial content must reach the Anthropic client as a clean message_stop
// (end_turn) keeping the partial text — not as a stream_error event.
func TestHandleResponsesToAnthropicStream_TruncatedSalvaged(t *testing.T) {
	c, w := newAnthropicTestContext(t)

	stream := openaistream.NewStream[responses.ResponseStreamEventUnion](
		newFakeResponsesDecoder(truncatedResponsesEvents()), nil)

	hc := protocol.NewHandleContext(c, "proxy-model")
	hc.SalvageTruncatedStream = true
	_, err := HandleResponsesToAnthropicV1Stream(hc, stream, "proxy-model")
	require.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "partial", "partial content must be preserved")
	events := parseSSEEvents(body)
	msgDelta, ok := events[eventTypeMessageDelta]
	require.True(t, ok, "salvage must emit message_delta")
	assert.Equal(t, "end_turn", msgDelta["delta"].(map[string]interface{})["stop_reason"])
	_, hasStop := events[eventTypeMessageStop]
	assert.True(t, hasStop, "salvage must emit message_stop")
	assert.NotContains(t, body, `"type":"error"`)
}

// TestHandleResponsesToAnthropicStream_TruncatedDefaultErrors: without the
// flag, the honest default survives — an explicit error event, no fabricated
// message_stop.
func TestHandleResponsesToAnthropicStream_TruncatedDefaultErrors(t *testing.T) {
	c, w := newAnthropicTestContext(t)

	stream := openaistream.NewStream[responses.ResponseStreamEventUnion](
		newFakeResponsesDecoder(truncatedResponsesEvents()), nil)

	_, err := HandleResponsesToAnthropicV1Stream(protocol.NewHandleContext(c, "proxy-model"), stream, "proxy-model")
	require.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "upstream stream ended before completion")
	assert.NotContains(t, body, `"type":"message_stop"`)
}

// TestHandleResponsesToAnthropicStream_ReadErrorSalvaged: an upstream that
// tears the connection down (transport read error) after partial content is
// the same truncation from the client's point of view — the flag must salvage
// it too, and the handler must not re-surface the read error afterwards.
func TestHandleResponsesToAnthropicStream_ReadErrorSalvaged(t *testing.T) {
	c, w := newAnthropicTestContext(t)

	dec := &errFakeResponsesDecoder{
		fakeResponsesDecoder: newFakeResponsesDecoder(truncatedResponsesEvents()),
		err:                  io.ErrUnexpectedEOF,
	}
	stream := openaistream.NewStream[responses.ResponseStreamEventUnion](dec, nil)

	hc := protocol.NewHandleContext(c, "proxy-model")
	hc.SalvageTruncatedStream = true
	_, err := HandleResponsesToAnthropicV1Stream(hc, stream, "proxy-model")
	require.NoError(t, err, "salvaged read error must not surface as a handler error")

	body := w.Body.String()
	events := parseSSEEvents(body)
	_, hasStop := events[eventTypeMessageStop]
	assert.True(t, hasStop, "salvage must emit message_stop")
	assert.NotContains(t, body, "stream_failed", "read error must not be re-surfaced after salvage")
	assert.NotContains(t, body, `"type":"error"`)
}

// TestHandleResponsesToAnthropicStream_ReadErrorDefaultStillErrors: without
// the flag, the read error keeps its existing surfacing (truncation event from
// the converter plus the handler's stream_failed error path).
func TestHandleResponsesToAnthropicStream_ReadErrorDefaultStillErrors(t *testing.T) {
	c, w := newAnthropicTestContext(t)

	dec := &errFakeResponsesDecoder{
		fakeResponsesDecoder: newFakeResponsesDecoder(truncatedResponsesEvents()),
		err:                  io.ErrUnexpectedEOF,
	}
	stream := openaistream.NewStream[responses.ResponseStreamEventUnion](dec, nil)

	_, err := HandleResponsesToAnthropicV1Stream(protocol.NewHandleContext(c, "proxy-model"), stream, "proxy-model")
	require.Error(t, err)
	assert.NotContains(t, w.Body.String(), `"type":"message_stop"`)
}

// TestHandleOpenAIToAnthropicStream_TruncatedSalvaged: the Chat→Anthropic
// conversion path honors the flag — a chat stream cut after content and
// before finish_reason closes with the normal terminal sequence.
func TestHandleOpenAIToAnthropicStream_TruncatedSalvaged(t *testing.T) {
	c, w := newAnthropicTestContext(t)

	dec := &fakeChatDecoder{events: []string{
		buildChatRoleOnlyChunkJSON(t),
		buildChatContentChunkJSON(t, "partial"),
	}, current: -1}
	stream := openaistream.NewStream[openai.ChatCompletionChunk](dec, nil)

	hc := protocol.NewHandleContext(c, "gpt-4o")
	hc.SalvageTruncatedStream = true
	_, err := HandleOpenAIToAnthropicStreamResponse(hc, nil, stream, "gpt-4o")
	require.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "partial")
	events := parseSSEEvents(body)
	msgDelta, ok := events[eventTypeMessageDelta]
	require.True(t, ok, "salvage must emit message_delta")
	assert.Equal(t, "end_turn", msgDelta["delta"].(map[string]interface{})["stop_reason"])
	_, hasStop := events[eventTypeMessageStop]
	assert.True(t, hasStop, "salvage must emit message_stop")
	assert.NotContains(t, body, `"type":"error"`)
}

// TestHandleOpenAIToAnthropicStream_TruncatedDefaultErrors: without the flag,
// the Chat→Anthropic truncation keeps surfacing the honest error event.
func TestHandleOpenAIToAnthropicStream_TruncatedDefaultErrors(t *testing.T) {
	c, w := newAnthropicTestContext(t)

	dec := &fakeChatDecoder{events: []string{
		buildChatRoleOnlyChunkJSON(t),
		buildChatContentChunkJSON(t, "partial"),
	}, current: -1}
	stream := openaistream.NewStream[openai.ChatCompletionChunk](dec, nil)

	_, err := HandleOpenAIToAnthropicStreamResponse(protocol.NewHandleContext(c, "gpt-4o"), nil, stream, "gpt-4o")
	require.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "upstream stream ended before completion")
	assert.NotContains(t, body, `"type":"message_stop"`)
}

// TestHandleOpenAIToAnthropicStream_ReadErrorSalvaged: a torn-down chat
// upstream connection after content salvages the same way, and the read error
// is not re-surfaced by the handler's post-run check.
func TestHandleOpenAIToAnthropicStream_ReadErrorSalvaged(t *testing.T) {
	c, w := newAnthropicTestContext(t)

	dec := &errFakeChatDecoder{
		fakeChatDecoder: &fakeChatDecoder{events: []string{
			buildChatRoleOnlyChunkJSON(t),
			buildChatContentChunkJSON(t, "partial"),
		}, current: -1},
		err: io.ErrUnexpectedEOF,
	}
	stream := openaistream.NewStream[openai.ChatCompletionChunk](dec, nil)

	hc := protocol.NewHandleContext(c, "gpt-4o")
	hc.SalvageTruncatedStream = true
	_, err := HandleOpenAIToAnthropicStreamResponse(hc, nil, stream, "gpt-4o")
	require.NoError(t, err)

	body := w.Body.String()
	events := parseSSEEvents(body)
	_, hasStop := events[eventTypeMessageStop]
	assert.True(t, hasStop, "salvage must emit message_stop")
	assert.NotContains(t, body, "stream_failed")
}

// TestHandleOpenAIChatToResponsesStream_ReadErrorSalvaged: the Chat→Responses
// path (Codex-shaped client) salvages a torn-down chat upstream connection
// after partial content the same way it salvages a clean EOF.
func TestHandleOpenAIChatToResponsesStream_ReadErrorSalvaged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	dec := &errFakeChatDecoder{
		fakeChatDecoder: &fakeChatDecoder{events: []string{
			buildChatRoleOnlyChunkJSON(t),
			buildChatContentChunkJSON(t, "partial"),
		}, current: -1},
		err: io.ErrUnexpectedEOF,
	}
	stream := openaistream.NewStream[openai.ChatCompletionChunk](dec, nil)

	hc := protocol.NewHandleContext(c, "gpt-4o")
	hc.SalvageTruncatedStream = true
	_, err := HandleOpenAIChatToResponsesStream(hc, stream, "gpt-4o")
	require.NoError(t, err)

	body := w.Body.String()
	assert.Contains(t, body, "response.completed")
	assert.NotContains(t, body, `"type":"error"`)
}
