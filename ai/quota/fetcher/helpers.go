package fetcher

import (
	"cmp"
	"strings"
	"time"

	"github.com/tingly-dev/tingly-box/ai"
	"github.com/tingly-dev/tingly-box/ai/quota"
)

// calcPercent returns used/limit * 100, capped at 100.
func calcPercent(used, limit float64) float64 {
	if limit <= 0 {
		return 0
	}
	p := (used / limit) * 100
	if p > 100 {
		return 100
	}
	return p
}

// unreadableUsage describes a provider whose quota cannot be read.
func unreadableUsage(provider *ai.Provider, providerType quota.ProviderType, reason string) *quota.ProviderUsage {
	return quota.Unreadable(provider.UUID, provider.Name, providerType, reason, time.Now(), 1*time.Hour)
}

// windowTypeForMinutes names a period from its length. Upstream types are not
// dependable — Codex reports 604800s for what it calls the primary window on a
// free plan, and MiniMax's "daily" bucket is whatever interval it reports — and
// the name reaches users through the status line.
func windowTypeForMinutes(minutes int) quota.WindowType {
	switch {
	case minutes <= 0:
		return quota.WindowTypeCustom
	case minutes < 24*60:
		return quota.WindowTypeSession
	case minutes < 2*24*60:
		return quota.WindowTypeDaily
	case minutes < 28*24*60:
		return quota.WindowTypeWeekly
	default:
		return quota.WindowTypeMonthly
	}
}

// endpoint resolves a request URL, preferring the test override over the
// production host.
func endpoint(override, production, path string) string {
	return strings.TrimRight(cmp.Or(override, production), "/") + path
}

// apiRoot reduces a provider's configured APIBase to the host root a fetcher
// can append a full path to. A provider's APIBase is the inference endpoint
// and normally already carries the vendor's API prefix ("…/api/v1"), while a
// usage endpoint is addressed from the root — appending the full path to an
// unstripped base doubles the prefix and the request 404s. Longest suffixes
// first, since "/api/v1" also ends with "/v1".
func apiRoot(apiBase, production string, suffixes ...string) string {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	for _, suffix := range suffixes {
		if trimmed, ok := strings.CutSuffix(base, suffix); ok {
			base = strings.TrimRight(trimmed, "/")
			break
		}
	}
	return cmp.Or(base, production)
}

// errorDetail renders a non-2xx upstream response body as a ": <message>"
// suffix for an error, so "unexpected status code: N" says what N actually
// meant instead of leaving the caller to guess between an expired token, a
// rejected client, or a lapsed plan. Whitespace (including the newlines an
// HTML error page carries) is collapsed to keep it one line, and long bodies
// are clipped on a rune boundary — 512 runes reaches past a typical vendor
// error envelope's outer keys into the nested message that explains it.
func errorDetail(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	if text == "" {
		return ""
	}
	if runes := []rune(text); len(runes) > 512 {
		text = string(runes[:512]) + "…"
	}
	return ": " + text
}
