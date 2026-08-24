package client

import (
	"context"
	"testing"
)

// The advisor loopback layer must stamp the depth header exactly when the
// context carries the advisor mark — that header is what lets the inbound
// side detect a loopback and skip MCP tool injection (recursion guard).

func TestAdvisorLoopbackTransport_MarkedContextStampsHeader(t *testing.T) {
	capture := &captureTransport{}
	tr := wrapWithAdvisorLoopback(capture)

	ctx := WithAdvisorLoopback(context.Background())
	if _, err := tr.RoundTrip(newReq(t, ctx, "")); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if got := capture.lastReq.Header.Get(AdvisorDepthHeader); got != "1" {
		t.Errorf("expected %s=1 on marked context, got %q", AdvisorDepthHeader, got)
	}
}

func TestAdvisorLoopbackTransport_UnmarkedContextUntouched(t *testing.T) {
	capture := &captureTransport{}
	tr := wrapWithAdvisorLoopback(capture)

	req := newReq(t, context.Background(), "")
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if got := capture.lastReq.Header.Get(AdvisorDepthHeader); got != "" {
		t.Errorf("expected no %s header on unmarked context, got %q", AdvisorDepthHeader, got)
	}
	if capture.lastReq != req {
		t.Error("unmarked request should pass through without cloning")
	}
}
