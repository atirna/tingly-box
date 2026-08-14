package ops

import (
	"net/url"
	"strings"
)

// SplitProviderHostPath splits a provider base URL into its lowercased host
// and lowercased path via net/url, which already handles userinfo, port, and
// bracketed IPv6 literals correctly — no need to hand-roll that parsing.
//
// The one thing net/url won't do is a bare, scheme-less host: Provider.APIBase
// is sometimes stored without one (e.g. "api.deepseek.com" rather than
// "https://api.deepseek.com"), and net/url.Parse treats that as a relative
// path rather than a host. A default scheme is prepended when one is
// missing so parsing lands on the host as intended.
//
// Every vendor-dispatch site in this package (request-side transforms,
// response-side transforms) and in transform.VendorTransform's Anthropic-shape
// dispatch shares this helper rather than re-parsing providerURL itself.
// Match on the parsed host (and, for vendors scoped to one path on a shared
// host, a path prefix) instead of strings.Contains-ing the raw URL text — the
// old pattern let a base URL that merely *mentioned* a vendor's hostname in
// its path or query (e.g. a proxy at
// "https://gateway.example.com/relay?target=api.deepseek.com") get mistaken
// for that vendor.
func SplitProviderHostPath(providerURL string) (host, path string) {
	raw := strings.TrimSpace(providerURL)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", ""
	}
	return strings.ToLower(u.Hostname()), strings.ToLower(u.Path)
}
