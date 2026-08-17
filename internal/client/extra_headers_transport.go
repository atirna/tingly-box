package client

import (
	"net/http"

	"github.com/sirupsen/logrus"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// extraHeadersTransport applies user-configured extra headers
// (.design/provider-flags.md) to outbound requests. It is mounted by
// wrapWithLogging — the one transport wrapper every client constructor goes
// through — so it covers all API styles from a single place, sitting OUTSIDE
// the vendor round-trippers: headers written here are set before the inner
// (vendor) chain runs, so vendor-pinned headers always win on a name
// conflict. Never mount this inside a vendor chain.
//
// Headers arrive via request context (typ.WithExtraHeaders), resolved at
// dispatch time from the rule's extra_headers flag. Provider- and
// model-level headers layer onto the same mechanism in a follow-up.
//
// Release gate: extra headers apply to api_key providers only. The gate
// lives in wrapWithExtraHeaders (non-api_key providers get no transport at
// all), mirroring the API validation gate.
type extraHeadersTransport struct {
	inner http.RoundTripper
}

// wrapWithExtraHeaders mounts the extra-headers layer for api_key providers.
// Other auth types return inner unchanged — the release gate: ctx-carried
// rule headers can never reach a vendor/OAuth/multi-field-credential chain.
func wrapWithExtraHeaders(inner http.RoundTripper, provider *typ.Provider) http.RoundTripper {
	if provider == nil || !provider.IsAPIKey() {
		return inner
	}
	return &extraHeadersTransport{inner: inner}
}

func (t *extraHeadersTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	headers := typ.GetExtraHeaders(req.Context())
	if len(headers) == 0 {
		return t.inner.RoundTrip(req)
	}

	// Clone before mutating: the request may be shared/retried by the SDK.
	req = req.Clone(req.Context())
	for name, value := range headers {
		// Second line of defense behind ValidateExtraHeaders — imports or
		// pre-validation rows might carry denied names. Skip loudly, never
		// silently alter gateway-managed headers.
		if typ.IsDeniedExtraHeader(name) {
			logrus.WithContext(req.Context()).Warnf("extra header %q is gateway-managed, skipping", name)
			continue
		}
		req.Header.Set(name, value)
	}
	return t.inner.RoundTrip(req)
}
