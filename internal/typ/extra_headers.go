package typ

import (
	"fmt"
	"net/textproto"

	"golang.org/x/net/http/httpguts"
)

// Extra headers are deliberately user-driven: tingly-box is an orchestrator,
// and "custom" implies needs we cannot predict, so there is no denylist and
// no size/count cap — the user can configure any header, including ones the
// gateway also manages (Authorization, User-Agent, …), and owns the outcome.
// What still wins over a user header is decided by transport ordering, not
// by filtering: vendor-pinned headers and the User-Agent chain write later
// (closer to the wire) and therefore take precedence on generic conflicts
// (see .design/provider-flags.md §5).
//
// ValidateExtraHeaders therefore checks structural validity only — things
// that would make the HTTP request itself malformed or the config ambiguous:
//   - header names must be RFC 7230 tokens, values must be valid field values
//     (net/http would otherwise fail the request at send time with a less
//     helpful error);
//   - case-insensitively duplicate names are rejected, because HTTP header
//     names are case-insensitive and a map carrying both spellings has no
//     defined winner.
func ValidateExtraHeaders(headers map[string]string) error {
	seen := make(map[string]bool, len(headers))
	for name, value := range headers {
		if name == "" {
			return fmt.Errorf("extra header with empty name")
		}
		if !httpguts.ValidHeaderFieldName(name) {
			return fmt.Errorf("invalid extra header name %q", name)
		}
		if !httpguts.ValidHeaderFieldValue(value) {
			return fmt.Errorf("extra header %q has an invalid value", name)
		}
		canonical := textproto.CanonicalMIMEHeaderKey(name)
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
