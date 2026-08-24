package client

import (
	"context"
	"net/http"
)

// AdvisorDepthHeader marks advisor loopback requests. When the advisor
// provider points back at tingly-box itself, the inbound side reads this
// header to mark the request as an advisor loopback and skip MCP tool
// injection, instead of recursively re-injecting the advisor tool (see
// protocolserver/protocol_transform.go and transform_mcp_tool_injection.go).
const AdvisorDepthHeader = "X-Tingly-Advisor-Depth"

// advisorLoopbackKey marks a context whose outbound HTTP calls originate from
// the in-process advisor tool.
type advisorLoopbackKey struct{}

// WithAdvisorLoopback marks ctx so advisorLoopbackTransport stamps
// AdvisorDepthHeader on outbound requests. The advisor tool calls this right
// before its SDK calls (mcp/runtime/advisor_call.go).
func WithAdvisorLoopback(ctx context.Context) context.Context {
	return context.WithValue(ctx, advisorLoopbackKey{}, true)
}

func isAdvisorLoopback(ctx context.Context) bool {
	v, _ := ctx.Value(advisorLoopbackKey{}).(bool)
	return v
}

// advisorLoopbackTransport stamps AdvisorDepthHeader on requests whose context
// carries the WithAdvisorLoopback mark; all other requests pass through
// untouched. Mounted on the generic pass-through chains only — the only chains
// an advisor loopback can traverse, since vendor chains pin real vendor
// endpoints and cannot point back at tingly-box. Header-stamp only: it never
// reads or rewrites anything else, so it is safe on api_key and OAuth-less
// chains alike.
type advisorLoopbackTransport struct {
	inner http.RoundTripper
}

func wrapWithAdvisorLoopback(inner http.RoundTripper) http.RoundTripper {
	return &advisorLoopbackTransport{inner: inner}
}

func (t *advisorLoopbackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	inner := t.inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	ctx := req.Context()
	if !isAdvisorLoopback(ctx) {
		return inner.RoundTrip(req)
	}
	// Clone before mutating so retries never race on shared headers.
	req = req.Clone(ctx)
	req.Header.Set(AdvisorDepthHeader, "1")
	return inner.RoundTrip(req)
}
