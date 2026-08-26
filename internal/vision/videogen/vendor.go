package videogen

import (
	"net/url"
	"strings"

	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// Vendor identifies the video generation surface a provider exposes. It is
// derived from the provider's API base host, not from a user-chosen provider
// name, so it stays stable regardless of how the user labelled the provider.
type Vendor string

const (
	// VendorOpenAICompat covers providers assumed to speak the OpenAI
	// /videos (Sora) job contract. Today that is essentially api.openai.com,
	// but — mirroring imagegen's optimistic default — any other OpenAI-style
	// provider is also given the benefit of the doubt: if the upstream lacks
	// /videos the request fails with the upstream's own 404, which is more
	// honest than the gateway guessing capabilities.
	VendorOpenAICompat Vendor = "openai_compat"
	// VendorDashScope is Alibaba Model Studio / DashScope (Wan text-to-video):
	// native async task-submit-then-poll API.
	VendorDashScope Vendor = "dashscope"
	// VendorMinimax is MiniMax (Hailuo): native async task API with a
	// separate file-retrieve step for the finished asset.
	VendorMinimax Vendor = "minimax"
	// VendorArk is Volcengine Ark / BytePlus (ByteDance Doubao Seedance):
	// native async "content generation tasks" API. Ark's chat surface is
	// OpenAI-compatible on the same base, but its video surface is not.
	VendorArk Vendor = "ark"
	// VendorUnknown is a provider with no known video surface.
	VendorUnknown Vendor = "unknown"
)

// DetectVendor inspects a provider and returns the video generation vendor
// family it belongs to. Detection is host-based so it works for both the
// canonical providers in internal/data and user-defined clones that point at
// the same hosts.
func DetectVendor(provider *typ.Provider) Vendor {
	if provider == nil {
		return VendorUnknown
	}

	host := apiHost(provider.APIBase)
	switch {
	case strings.Contains(host, "dashscope") && strings.Contains(host, "aliyuncs.com"):
		// Matches both dashscope.aliyuncs.com (Beijing) and
		// dashscope-intl.aliyuncs.com (Singapore).
		return VendorDashScope
	case strings.Contains(host, "api.minimax.io"), strings.Contains(host, "api.minimaxi.com"):
		return VendorMinimax
	case strings.Contains(host, "volces.com"), strings.Contains(host, "bytepluses.com"):
		// ark.cn-beijing.volces.com (Volcengine) and
		// ark.ap-southeast.bytepluses.com (BytePlus).
		return VendorArk
	}

	if provider.APIStyle == protocol.APIStyleOpenAI || provider.APIStyle == "" {
		return VendorOpenAICompat
	}
	return VendorUnknown
}

// apiHost extracts the lowercased host from an API base URL. Falls back to the
// raw string when parsing fails so substring matching still has something to
// work with.
func apiHost(apiBase string) string {
	apiBase = strings.TrimSpace(apiBase)
	if apiBase == "" {
		return ""
	}
	if u, err := url.Parse(apiBase); err == nil && u.Host != "" {
		return strings.ToLower(u.Host)
	}
	return strings.ToLower(apiBase)
}

// apiScheme returns the scheme of the API base, defaulting to https.
func apiScheme(apiBase string) string {
	if u, err := url.Parse(strings.TrimSpace(apiBase)); err == nil && u.Scheme != "" {
		return u.Scheme
	}
	return "https"
}
