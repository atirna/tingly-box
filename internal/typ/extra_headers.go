package typ

import (
	"fmt"
	"net/textproto"

	"golang.org/x/net/http/httpguts"
)

// Extra-header limits enforced by ValidateExtraHeaders, per level.
const (
	MaxExtraHeadersPerLevel = 16
	MaxExtraHeaderNameLen   = 128
	MaxExtraHeaderValueLen  = 4096
)

// deniedExtraHeaders lists header names (canonical form) that extra_headers
// may never set, at any level:
//   - transport-breaking headers the HTTP stack owns
//   - credential-carrying headers — credentials must go through the
//     Token/Credential fields so masking, export scrubbing, and refresh keep
//     working (never through header config)
//   - User-Agent, which has its own dedicated mechanism (rule
//     custom_user_agent + vendor pins; see .design/user-agent.md)
var deniedExtraHeaders = map[string]bool{
	"Host":                true,
	"Content-Length":      true,
	"Transfer-Encoding":   true,
	"Connection":          true,
	"Upgrade":             true,
	"Trailer":             true,
	"Te":                  true,
	"Keep-Alive":          true,
	"Authorization":       true,
	"Proxy-Authorization": true,
	"X-Api-Key":           true,
	"User-Agent":          true,
}

// IsDeniedExtraHeader reports whether the header name (any case) is on the
// denylist. The outbound transport uses this as a second line of defense
// against pre-validation data (imports, old rows).
func IsDeniedExtraHeader(name string) bool {
	return deniedExtraHeaders[textproto.CanonicalMIMEHeaderKey(name)]
}

// ValidateExtraHeaders checks one level's extra-header map at config-save
// time. All write entry points share this function so a header rejected in
// one place is rejected everywhere. Fail-loudly-on-save: the request path
// never filters silently except through the denylist defense above.
func ValidateExtraHeaders(headers map[string]string) error {
	if len(headers) == 0 {
		return nil
	}
	if len(headers) > MaxExtraHeadersPerLevel {
		return fmt.Errorf("too many extra headers: %d (max %d)", len(headers), MaxExtraHeadersPerLevel)
	}
	seen := make(map[string]bool, len(headers))
	for name, value := range headers {
		if name == "" {
			return fmt.Errorf("extra header with empty name")
		}
		if len(name) > MaxExtraHeaderNameLen {
			return fmt.Errorf("extra header name %q too long: %d bytes (max %d)", name, len(name), MaxExtraHeaderNameLen)
		}
		if !httpguts.ValidHeaderFieldName(name) {
			return fmt.Errorf("invalid extra header name %q", name)
		}
		if len(value) > MaxExtraHeaderValueLen {
			return fmt.Errorf("extra header %q value too long: %d bytes (max %d)", name, len(value), MaxExtraHeaderValueLen)
		}
		if !httpguts.ValidHeaderFieldValue(value) {
			return fmt.Errorf("extra header %q has an invalid value", name)
		}
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		if deniedExtraHeaders[canonical] {
			return fmt.Errorf("extra header %q is not allowed: it is managed by the gateway", canonical)
		}
		if seen[canonical] {
			return fmt.Errorf("duplicate extra header %q (names are case-insensitive)", canonical)
		}
		seen[canonical] = true
	}
	return nil
}

// CanonicalizeExtraHeaders returns a copy of the map with every name in
// canonical form. Called on the save path after validation so stored config
// always shows the concrete on-wire spelling.
func CanonicalizeExtraHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for name, value := range headers {
		out[textproto.CanonicalMIMEHeaderKey(name)] = value
	}
	return out
}
