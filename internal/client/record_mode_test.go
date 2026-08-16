package client

import (
	"context"
	"net/http"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/tingly-dev/tingly-box/ai"
	"github.com/tingly-dev/tingly-box/internal/obs"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// enabledSink returns a record sink that reports IsEnabled — the condition
// ClientPool uses before calling SetRecordSink on every freshly built client.
func enabledSink(t *testing.T) *obs.Sink {
	t.Helper()
	sink := obs.NewSink(t.TempDir(), obs.RecordModeStagedRequestResponse)
	require.NotNil(t, sink)
	require.True(t, sink.IsEnabled())
	t.Cleanup(func() { sink.Close() })
	return sink
}

// TestClaudeClient_SetRecordSink pins the fix for a crash on the hottest path:
// ClientPool calls SetRecordSink on every Anthropic client it builds, and
// ClaudeClient used to carry no *http.Client at all, so enabling recording on a
// Claude Code provider dereferenced nil inside applyRecordMode.
func TestClaudeClient_SetRecordSink(t *testing.T) {
	c, err := NewClaudeClient(context.Background(), newOAuthProvider(), "claude-sonnet-4-6", typ.SessionID{Value: "s"})
	require.NoError(t, err)

	require.NotPanics(t, func() { c.SetRecordSink(enabledSink(t)) })

	// Recording must reach the live chain, not merely avoid crashing: the
	// recorder sits outermost, over the session-bound transport.
	require.NotNil(t, c.HttpClient(), "the SDK must send through a client we hold")
	rec, ok := c.HttpClient().Transport.(*RecordRoundTripper)
	require.True(t, ok, "record mode must wrap the client's transport, got %T", c.HttpClient().Transport)
	assert.IsType(t, &SessionBoundTransport{}, rec.transport)
}

// TestClaudeClient_RecordModeSurvivesGuard covers the delivery path: every
// request rebuilds an SDK client in Guard/GuardBeta from the copied options, so
// the recorder installed above only records if that rebuild keeps addressing the
// same *http.Client.
func TestClaudeClient_RecordModeSurvivesGuard(t *testing.T) {
	c, err := NewClaudeClient(context.Background(), newOAuthProvider(), "claude-sonnet-4-6", typ.SessionID{Value: "s"})
	require.NoError(t, err)
	c.SetRecordSink(enabledSink(t))

	req := &anthropic.MessageNewParams{
		Model:     anthropic.Model("claude-sonnet-4-6"),
		MaxTokens: 512,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	}
	req.Metadata.UserID = param.NewOpt(`{"device_id":"dev1","account_uuid":"acc1","session_id":"550e8400-e29b-41d4-a716-446655440000"}`)

	base, _ := c.Guard(context.Background(), req)
	require.NotNil(t, base)
	assert.Same(t, c.HttpClient(), base.HttpClient(), "the guard client must share the recorded HTTP client")
	assert.IsType(t, &RecordRoundTripper{}, base.HttpClient().Transport)
}

// TestNewClaudeClient_UsesSessionBoundTransport pins the wiring the transport
// policy already assumed: ai.IssuerClaudeCode is registered TransportPerSession,
// which only takes effect if the client goes through the pooled session-bound
// transport instead of the SDK's http.DefaultClient. Riding the pool is also
// what makes provider.ProxyURL apply and stops env proxies being inherited.
func TestNewClaudeClient_UsesSessionBoundTransport(t *testing.T) {
	provider := newOAuthProvider()
	provider.UUID = "provider-uuid"
	provider.ProxyURL = "http://127.0.0.1:1"
	provider.OAuthDetail.Issuer = ai.IssuerClaudeCode

	c, err := NewClaudeClient(context.Background(), provider, "claude-sonnet-4-6", typ.SessionID{Value: "sess-1"})
	require.NoError(t, err)

	sbt, ok := c.HttpClient().Transport.(*SessionBoundTransport)
	require.True(t, ok, "expected a session-bound transport, got %T", c.HttpClient().Transport)
	assert.Equal(t, "provider-uuid", sbt.providerUUID)
	assert.Equal(t, "http://127.0.0.1:1", sbt.proxyURL)
	assert.Equal(t, ai.IssuerClaudeCode, sbt.issuer)
	assert.Equal(t, "sess-1", sbt.sessionID.Value)
}

// TestApplyRecordMode_NilHTTPClient covers the backstop: a constructor that
// forgets to store its client must degrade to a warning, never a panic.
func TestApplyRecordMode_NilHTTPClient(t *testing.T) {
	c := &AnthropicClient{provider: newOAuthProvider()}
	require.NotPanics(t, func() { c.SetRecordSink(enabledSink(t)) })
	assert.Nil(t, c.httpClient)
}

// TestVertexAnthropicClient_RecordsTheAuthedClient guards the same invariant on
// the Vertex path, where it failed silently instead of loudly: the adapter
// options replace the SDK's HTTP client with the SA-authed one, so the field
// must follow — otherwise record mode wrapped a client no request ever used.
func TestVertexAnthropicClient_RecordsTheAuthedClient(t *testing.T) {
	provider := cloudProvider("vertex", typ.AuthTypeGCPVertex, map[string]string{
		ai.CredFieldGCPServiceAccountJSON: fakeSAJSON,
		ai.CredFieldGCPProjectID:          "proj",
		ai.CredFieldGCPLocation:           "us-east5",
	})

	c, err := NewVertexAnthropicClient(provider, "claude-sonnet-4-6", typ.SessionID{})
	require.NoError(t, err)
	require.IsType(t, &oauth2.Transport{}, c.HttpClient().Transport, "the field must hold the SA-authed client")

	c.SetRecordSink(enabledSink(t))
	rec, ok := c.HttpClient().Transport.(*RecordRoundTripper)
	require.True(t, ok, "record mode must wrap the authed transport, got %T", c.HttpClient().Transport)
	assert.IsType(t, &oauth2.Transport{}, rec.transport)
}

var _ http.RoundTripper = (*RecordRoundTripper)(nil)
