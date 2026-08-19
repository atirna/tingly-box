package protocol

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newUpstream400 drives a real request through the openai-go SDK against a
// stub upstream, so the returned *openai.Error is built by the SDK's own
// error path (requestconfig) — the exact object production code sees.
func newUpstream400(t *testing.T, body string, contentType string) *openai.Error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	client := openai.NewClient(
		option.WithBaseURL(srv.URL),
		option.WithAPIKey("test-key"),
		option.WithMaxRetries(0),
	)
	_, err := client.Chat.Completions.New(t.Context(), openai.ChatCompletionNewParams{
		Model:    "gpt-test",
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	})
	require.Error(t, err)
	var oaiErr *openai.Error
	require.ErrorAs(t, err, &oaiErr)
	return oaiErr
}

// TestUpstreamErrorMessage_RecoversNonStandardBody: a 400 whose body has no
// top-level "error" key prints as a bare status line from the SDK; the helper
// must append the raw body so the actual diagnostic reaches the client.
func TestUpstreamErrorMessage_RecoversNonStandardBody(t *testing.T) {
	oaiErr := newUpstream400(t, `{"message":"unsupported parameter: max_tokens"}`, "application/json")

	// Precondition: the SDK's own message lost the body.
	assert.NotContains(t, oaiErr.Error(), "unsupported parameter")

	msg := UpstreamErrorMessage(oaiErr)
	assert.Contains(t, msg, "400")
	assert.Contains(t, msg, "unsupported parameter: max_tokens",
		"the upstream body must be recovered into the message")

	// The body must be re-populated for later readers (DumpResponse etc).
	again := UpstreamErrorMessage(oaiErr)
	assert.Contains(t, again, "unsupported parameter: max_tokens")
}

// TestUpstreamErrorMessage_StandardBodyUnchanged: when the body is
// {"error": ...}-shaped the SDK message already carries it — no duplication.
func TestUpstreamErrorMessage_StandardBodyUnchanged(t *testing.T) {
	oaiErr := newUpstream400(t, `{"error":{"message":"bad param","type":"invalid_request_error"}}`, "application/json")

	require.Contains(t, oaiErr.Error(), "bad param", "SDK message should already carry the error body")
	msg := UpstreamErrorMessage(oaiErr)
	assert.Equal(t, oaiErr.Error(), msg)
	assert.NotContains(t, msg, "upstream body:")
}

// TestUpstreamErrorMessage_TruncatesHugeBody bounds the appended snippet.
func TestUpstreamErrorMessage_TruncatesHugeBody(t *testing.T) {
	oaiErr := newUpstream400(t, strings.Repeat("x", 4096), "text/plain")

	msg := UpstreamErrorMessage(oaiErr)
	assert.Contains(t, msg, "upstream body:")
	assert.Contains(t, msg, "…(truncated)")
	assert.Less(t, len(msg), 1024)
}

// TestUpstreamErrorMessage_NonAPIError passes plain errors through untouched.
func TestUpstreamErrorMessage_NonAPIError(t *testing.T) {
	err := io.ErrUnexpectedEOF
	assert.Equal(t, err.Error(), UpstreamErrorMessage(err))
	assert.Equal(t, "", UpstreamErrorMessage(nil))
}
