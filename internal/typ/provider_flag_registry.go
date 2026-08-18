package typ

// ProviderFlagRegistry returns the catalog of supported provider/model-level
// flags — the supply-side counterpart of RuleFlagRegistry, with the same
// contract: keys must match the JSON tag names on ProviderFlags, and the UI
// renders each spec from its Type. Every spec is configurable at both the
// provider and the model level (the model level wins per key), so there is
// no per-spec scope or merge axis to declare. See .design/provider-flags.md.
func ProviderFlagRegistry() []FlagSpec {
	return []FlagSpec{
		{
			Key:         "extra_headers",
			Label:       "Custom Headers",
			Description: "Append custom HTTP headers to outbound requests to this provider. Model-level headers apply only to requests for that model and win over provider-level headers on a name conflict; a rule-level Custom Headers flag wins over both (provider < model < rule). API-key providers only. Headers are sent as configured, including ones the gateway also sets (Authorization, User-Agent, …) — overriding those is your call and your responsibility.",
			Type:        FlagTypeHeaders,
			Category:    FlagCategoryRequest,
		},
	}
}
