package typ

import "net/textproto"

// SupplyExtraHeaders merges the two supply-side header levels for one
// (provider, model) pair — Provider.Flags ∪ Provider.ModelFlags[model], the
// model level winning on name conflicts. These are a property of the
// upstream, not of the request, so the client layer resolves them once per
// client (which is already keyed by provider + model) instead of per request.
//
// The third level, the rule's extra_headers, is a request-side flag: it
// rides the request context and the outbound transport applies it *after*
// this map, which is what makes the full precedence
//
//	provider  <  model  <  rule
//
// hold without anyone merging all three (see .design/provider-flags.md §5.1).
//
// Returns nil when neither supply level configures anything, so callers can
// cheaply skip injection.
func SupplyExtraHeaders(p *Provider, model string) map[string]string {
	if p == nil {
		return nil
	}
	provider := p.Flags.ExtraHeaders
	modelLevel := p.ModelFlags[model].ExtraHeaders
	if len(provider) == 0 && len(modelLevel) == 0 {
		return nil
	}
	merged := make(map[string]string, len(provider)+len(modelLevel))
	for _, level := range []map[string]string{provider, modelLevel} {
		for name, value := range level {
			merged[textproto.CanonicalMIMEHeaderKey(name)] = value
		}
	}
	return merged
}

// PruneModelFlags drops entries carrying no configuration (and the empty
// model key) so a cleared model does not linger as an empty object in
// storage and in API responses. Returns nil when nothing is left.
func PruneModelFlags(modelFlags map[string]ProviderFlags) map[string]ProviderFlags {
	pruned := make(map[string]ProviderFlags, len(modelFlags))
	for model, flags := range modelFlags {
		if model == "" || flags.IsZero() {
			continue
		}
		pruned[model] = flags
	}
	if len(pruned) == 0 {
		return nil
	}
	return pruned
}
