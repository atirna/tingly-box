package server

import (
	"testing"

	"github.com/tingly-dev/tingly-box/ai"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

func TestResolveOpenAIEndpoint(t *testing.T) {
	undeclared := &typ.Provider{UUID: "p-undeclared"} // empty declaration → chat default
	chatOnly := &typ.Provider{UUID: "p-chat", OpenAIEndpoints: []ai.OpenAIEndpoint{ai.OpenAIEndpointChat}}
	responsesOnly := &typ.Provider{UUID: "p-resp", OpenAIEndpoints: []ai.OpenAIEndpoint{ai.OpenAIEndpointResponses}}
	dual := &typ.Provider{UUID: "p-dual", OpenAIEndpoints: []ai.OpenAIEndpoint{ai.OpenAIEndpointChat, ai.OpenAIEndpointResponses}}

	tests := []struct {
		name     string
		provider *typ.Provider
		flags    typ.RuleFlags
		incoming IncomingAPIType
		want     protocol.APIType
	}{
		// Undeclared — conservative chat default
		{
			name:     "undeclared defaults to chat (chat incoming)",
			provider: undeclared,
			incoming: IncomingAPIChat,
			want:     protocol.TypeOpenAIChat,
		},
		{
			name:     "undeclared defaults to chat (responses incoming, downgrade)",
			provider: undeclared,
			incoming: IncomingAPIResponses,
			want:     protocol.TypeOpenAIChat,
		},

		// Explicit chat-only — responses incoming downgrades
		{
			name:     "chat-only serves chat natively",
			provider: chatOnly,
			incoming: IncomingAPIChat,
			want:     protocol.TypeOpenAIChat,
		},
		{
			name:     "chat-only converts responses incoming down to chat",
			provider: chatOnly,
			incoming: IncomingAPIResponses,
			want:     protocol.TypeOpenAIChat,
		},

		// Responses-only (Codex)
		{
			name:     "responses-only converts chat incoming up to responses",
			provider: responsesOnly,
			incoming: IncomingAPIChat,
			want:     protocol.TypeOpenAIResponses,
		},
		{
			name:     "responses-only serves responses natively",
			provider: responsesOnly,
			incoming: IncomingAPIResponses,
			want:     protocol.TypeOpenAIResponses,
		},

		// Dual declaration (OpenAI proper, DeepSeek) — native-first
		{
			name:     "dual declaration serves chat natively",
			provider: dual,
			incoming: IncomingAPIChat,
			want:     protocol.TypeOpenAIChat,
		},
		{
			name:     "dual declaration serves responses natively",
			provider: dual,
			incoming: IncomingAPIResponses,
			want:     protocol.TypeOpenAIResponses,
		},

		// Rule overrides (override takes priority over provider mode)
		{
			name:     "override=responses on chat-only provider forces responses",
			provider: chatOnly,
			flags:    typ.RuleFlags{OpenAIEndpointOverride: "responses"},
			incoming: IncomingAPIChat,
			want:     protocol.TypeOpenAIResponses,
		},
		{
			name:     "override=chat on responses-only provider forces chat",
			provider: responsesOnly,
			flags:    typ.RuleFlags{OpenAIEndpointOverride: "chat"},
			incoming: IncomingAPIChat,
			want:     protocol.TypeOpenAIChat,
		},
		{
			name:     "override=chat on dual provider forces chat",
			provider: dual,
			flags:    typ.RuleFlags{OpenAIEndpointOverride: "chat"},
			incoming: IncomingAPIResponses,
			want:     protocol.TypeOpenAIChat,
		},
		{
			name:     "override=responses on dual provider forces responses",
			provider: dual,
			flags:    typ.RuleFlags{OpenAIEndpointOverride: "responses"},
			incoming: IncomingAPIChat,
			want:     protocol.TypeOpenAIResponses,
		},

		// Auto / unknown override values
		{
			name:     "auto flag treated as no override",
			provider: chatOnly,
			flags:    typ.RuleFlags{OpenAIEndpointOverride: "auto"},
			incoming: IncomingAPIResponses,
			want:     protocol.TypeOpenAIChat,
		},
		{
			name:     "unknown flag value treated as no override",
			provider: dual,
			flags:    typ.RuleFlags{OpenAIEndpointOverride: "bogus"},
			incoming: IncomingAPIResponses,
			want:     protocol.TypeOpenAIResponses,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveOpenAIEndpoint(tt.provider, tt.flags, tt.incoming)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveOpenAIEndpointNilProviderErrors(t *testing.T) {
	if _, err := ResolveOpenAIEndpoint(nil, typ.RuleFlags{}, IncomingAPIChat); err == nil {
		t.Error("expected error for nil provider")
	}
}

// TestResolveOpenAIEndpointCodexOAuthSnapshot documents the design assumption
// that Codex providers declare OpenAIEndpoints=[responses] by the time they
// reach routing. The OAuth handler is responsible for setting this on
// instantiation; this test pins the resolver behavior against such providers.
func TestResolveOpenAIEndpointCodexOAuthSnapshot(t *testing.T) {
	codex := &typ.Provider{
		UUID:            "codex-1",
		OAuthDetail:     &typ.OAuthDetail{Issuer: ai.IssuerCodex},
		OpenAIEndpoints: ai.OpenAIEndpointsForIssuer(ai.IssuerCodex),
	}
	got, err := ResolveOpenAIEndpoint(codex, typ.RuleFlags{}, IncomingAPIChat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != protocol.TypeOpenAIResponses {
		t.Errorf("Codex declaring [responses] should route to Responses, got %v", got)
	}
}

func TestParseEndpointOverride(t *testing.T) {
	cases := []struct {
		in   string
		want EndpointOverride
	}{
		{"", OverrideAuto},
		{"auto", OverrideAuto},
		{"chat", OverrideChat},
		{"responses", OverrideResponses},
		{"unknown", OverrideAuto},
		{"CHAT", OverrideAuto}, // case-sensitive on purpose; the registry emits lowercase
	}
	for _, c := range cases {
		got := ParseEndpointOverride(c.in)
		if got != c.want {
			t.Errorf("ParseEndpointOverride(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsCodexProvider(t *testing.T) {
	if (&typ.Provider{UUID: "p-1"}).IsCodexProvider() {
		t.Error("provider without OAuthDetail should not be Codex")
	}
	codex := &typ.Provider{
		UUID:        "codex-1",
		AuthType:    typ.AuthTypeOAuth,
		OAuthDetail: &typ.OAuthDetail{Issuer: ai.IssuerCodex},
	}
	if !codex.IsCodexProvider() {
		t.Error("Codex-issuer OAuth provider should be Codex")
	}
}
