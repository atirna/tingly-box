package client

import (
	"net/http"

	"github.com/tingly-dev/tingly-box/internal/typ"
)

// ruleFlagTransport is the single outbound consumer of the request's resolved
// rule flags (typ.GetRuleFlags): every flag that materializes as a wire header
// is applied here, in one place, instead of one transport per flag.
//
// Which flags apply on which chain is a plain judgment call inside RoundTrip
// and at the mount sites — no capability framework:
//
//   - extra_headers: api_key providers only (release gate, mirroring the API
//     validation gate). Vendor/OAuth/multi-field-credential chains never see
//     rule-configured headers.
//   - custom_user_agent + inbound client UA forwarding: only chains mounted
//     with resolveUA=true (the generic OpenAI and non-OAuth Anthropic
//     pass-through chains). Vendor-specialized chains (Claude Code OAuth,
//     Codex, Kimi, Gemini, Antigravity) never mount this transport at all, so
//     their pinned handshake UA stays decisive (see .design/user-agent.md).
//
// Flags that cannot be expressed as a late header rewrite stay at the SDK
// layer by design, reading the same typ.GetRuleFlags: context_1m rides the
// SDK Betas field / per-call header option (anthropic.go) so it also reaches
// the Claude OAuth chain, and claude_org_id is resolved at client
// construction (claude_client.go).
type ruleFlagTransport struct {
	inner    http.RoundTripper
	provider *typ.Provider
	// resolveUA marks a generic pass-through chain: resolve the UA precedence
	// (rule/scenario custom_user_agent > inbound client UA > SDK default) at
	// this layer. Chains whose UA is owned elsewhere mount with false.
	resolveUA bool
}

// wrapWithRuleFlags mounts the rule-flag layer on a client transport chain.
// Mount it on pass-through chains only; vendor round-tripper chains stay
// unwrapped so no rule flag can reach into a vendor handshake.
func wrapWithRuleFlags(inner http.RoundTripper, provider *typ.Provider, resolveUA bool) http.RoundTripper {
	return &ruleFlagTransport{inner: inner, provider: provider, resolveUA: resolveUA}
}

func (t *ruleFlagTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	inner := t.inner
	if inner == nil {
		inner = http.DefaultTransport
	}

	ctx := req.Context()
	flags := typ.GetRuleFlags(ctx)

	// extra_headers: api_key providers only. Applied verbatim — user-driven
	// config, no filtering.
	var extra map[string]string
	if t.provider != nil && t.provider.IsAPIKey() {
		extra = flags.ExtraHeaders
	}

	// UA: an explicit rule/scenario override wins; otherwise forward the
	// inbound client's own UA; otherwise leave whatever the SDK stamped (兜底).
	var ua string
	if t.resolveUA {
		ua = flags.CustomUserAgent
		if ua == "" {
			ua = typ.GetClientUserAgent(ctx)
		}
	}

	if len(extra) == 0 && ua == "" {
		return inner.RoundTrip(req)
	}

	// Clone before mutating so concurrent retries never race on shared headers.
	req = req.Clone(ctx)

	// Extra headers first, UA second: on a User-Agent name conflict the UA
	// resolution wins, same precedence the old two-transport stack produced
	// (extra headers outermost, UA innermost). Inner chains still write after
	// this layer and win any remaining conflict by ordering.
	for name, value := range extra {
		req.Header.Set(name, value)
	}
	if ua == typ.UserAgentNone {
		// Sentinel (rule/scenario only): strip the User-Agent entirely. net/http
		// omits the header when it is present-but-empty, but injects the default
		// Go-http-client/<ver> when it is absent — so "" is the only way to send
		// a request carrying no User-Agent at all.
		req.Header.Set("User-Agent", "")
	} else if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	return inner.RoundTrip(req)
}
