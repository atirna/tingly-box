package protocolserver

import (
	"testing"

	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/server/config"
	"github.com/tingly-dev/tingly-box/internal/typ"
	"github.com/tingly-dev/tingly-box/internal/webproxy"
)

func webProxyScenarioConfig(provider, model string) *typ.ScenarioConfig {
	return &typ.ScenarioConfig{
		Extensions: map[string]interface{}{
			config.ExtensionWebProxyService: map[string]interface{}{
				"provider": provider,
				"model":    model,
			},
		},
	}
}

// The scenario-level service must fold into the same field the rule uses, so
// everything downstream reads one source — and the rule must win when both
// scopes are configured.
func TestResolveRuleFlagsWithScenario_WebProxyScopes(t *testing.T) {
	tests := []struct {
		name           string
		rule           *typ.Rule
		scenarioConfig *typ.ScenarioConfig
		wantProvider   string
	}{
		{
			name:           "scenario only",
			rule:           &typ.Rule{},
			scenarioConfig: webProxyScenarioConfig("scenario-provider", "m"),
			wantProvider:   "scenario-provider",
		},
		{
			name: "rule wins over scenario",
			rule: &typ.Rule{Flags: typ.RuleFlags{
				WebProxyService: &typ.WebProxyService{Provider: "rule-provider", Model: "m"},
			}},
			scenarioConfig: webProxyScenarioConfig("scenario-provider", "m"),
			wantProvider:   "rule-provider",
		},
		{
			name:           "neither",
			rule:           &typ.Rule{},
			scenarioConfig: &typ.ScenarioConfig{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newGinContext(t)
			flags := ResolveRuleFlagsWithScenario(c, tc.rule, "openai", tc.scenarioConfig,
				protocol.TypeOpenAIChat, protocol.TypeOpenAIChat, &typ.Provider{})

			if tc.wantProvider == "" {
				if flags.WebProxyService.IsActive() {
					t.Fatalf("expected no web proxy service, got %+v", flags.WebProxyService)
				}
				if webproxy.ActiveInContext(c.Request.Context()) {
					t.Fatal("inactive web proxy must not be attached to the request context")
				}
				return
			}

			if !flags.WebProxyService.IsActive() || flags.WebProxyService.Provider != tc.wantProvider {
				t.Fatalf("flags.WebProxyService = %+v, want provider %s", flags.WebProxyService, tc.wantProvider)
			}
			// The tool loop reads the service back from the context, deep in
			// dispatch where the rule is gone — so the stash matters as much
			// as the merged flag.
			got := webproxy.ServiceFromContext(c.Request.Context())
			if got == nil || got.Provider != tc.wantProvider {
				t.Fatalf("ServiceFromContext = %+v, want provider %s", got, tc.wantProvider)
			}
		})
	}
}

// A configured web proxy must contribute its tool transform to the preVendor
// slot; an unconfigured one must contribute nothing (zero-cost no-op).
func TestRulePreVendorTransforms_WebProxy(t *testing.T) {
	if got := RulePreVendorTransforms(typ.RuleFlags{}); len(got) != 0 {
		t.Fatalf("unconfigured flags produced %d preVendor transforms", len(got))
	}

	got := RulePreVendorTransforms(typ.RuleFlags{
		WebProxyService: &typ.WebProxyService{Provider: "p", Model: "m"},
	})
	var found bool
	for _, tr := range got {
		if tr.Name() == "web_proxy_tools" {
			found = true
		}
	}
	if !found {
		t.Fatalf("web_proxy_tools transform missing from preVendor slot: %+v", got)
	}

	// A half-filled reference is not a configuration.
	if got := RulePreVendorTransforms(typ.RuleFlags{
		WebProxyService: &typ.WebProxyService{Provider: "p"},
	}); len(got) != 0 {
		t.Fatalf("half-filled service produced %d preVendor transforms", len(got))
	}
}
