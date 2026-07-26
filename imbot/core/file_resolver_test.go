package core

import (
	"context"
	"errors"
	"testing"
)

// resolvingBot is a bot that can resolve its own media URLs.
type resolvingBot struct {
	Bot
	called string
	err    error
}

func (b *resolvingBot) ResolveFileURL(_ context.Context, mediaURL string) (string, error) {
	b.called = mediaURL
	if b.err != nil {
		return "", b.err
	}
	return "https://cdn.test/" + mediaURL, nil
}

// plainBot cannot; its attachments already carry usable URLs.
type plainBot struct{ Bot }

// TestResolveFileURLPassesThroughUnsupportedPlatforms: a platform with nothing
// to resolve must not turn a perfectly good URL into an error. Callers hand
// every attachment through this without inspecting schemes.
func TestResolveFileURLPassesThroughUnsupportedPlatforms(t *testing.T) {
	got, err := ResolveFileURL(context.Background(), &plainBot{}, "https://example.test/a.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://example.test/a.png" {
		t.Errorf("URL = %q, want it unchanged", got)
	}
}

func TestResolveFileURLDelegatesToTheBot(t *testing.T) {
	bot := &resolvingBot{}
	got, err := ResolveFileURL(context.Background(), bot, "tgfile://ABC123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bot.called != "tgfile://ABC123" {
		t.Errorf("bot saw %q", bot.called)
	}
	if got != "https://cdn.test/tgfile://ABC123" {
		t.Errorf("URL = %q", got)
	}
}

// TestResolveFileURLPropagatesFailure: a resolution that fails must not fall
// back to the unresolved URL, which would be fetched as a literal
// "tgfile://..." and fail confusingly downstream.
func TestResolveFileURLPropagatesFailure(t *testing.T) {
	bot := &resolvingBot{err: errors.New("file expired")}
	if _, err := ResolveFileURL(context.Background(), bot, "tgfile://ABC123"); err == nil {
		t.Fatal("expected the failure to propagate")
	}
}

// TestAsFileResolverUsesAnInterfaceAssertion guards the mistake this seam
// replaces: asserting on a concrete platform type, which no other platform
// could ever satisfy however capable it was.
func TestAsFileResolverUsesAnInterfaceAssertion(t *testing.T) {
	if _, ok := AsFileResolver(&resolvingBot{}); !ok {
		t.Error("a bot implementing the interface must be recognised")
	}
	if _, ok := AsFileResolver(&plainBot{}); ok {
		t.Error("a bot that cannot resolve must not be recognised")
	}
}
