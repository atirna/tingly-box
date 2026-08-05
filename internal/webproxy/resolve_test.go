package webproxy

import (
	"context"
	"testing"

	"github.com/tingly-dev/tingly-box/internal/server/config"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

func testConfig(scenario typ.RuleScenario, ext map[string]interface{}) *config.Config {
	return &config.Config{
		Scenarios: []typ.ScenarioConfig{{Scenario: scenario, Extensions: ext}},
	}
}

func scenarioExt(provider, model string) map[string]interface{} {
	return map[string]interface{}{
		config.ExtensionWebProxyService: map[string]interface{}{
			"provider": provider,
			"model":    model,
		},
	}
}

func ruleWith(provider, model string) *typ.Rule {
	return &typ.Rule{Flags: typ.RuleFlags{
		WebProxyService: &typ.WebProxyService{Provider: provider, Model: model},
	}}
}

func TestResolve_Priority(t *testing.T) {
	const scenario = typ.RuleScenario("claude_code")

	tests := []struct {
		name         string
		rule         *typ.Rule
		ext          map[string]interface{}
		wantProvider string
		wantModel    string
	}{
		{
			name:         "rule wins over scenario",
			rule:         ruleWith("rule-provider", "rule-model"),
			ext:          scenarioExt("scenario-provider", "scenario-model"),
			wantProvider: "rule-provider",
			wantModel:    "rule-model",
		},
		{
			name:         "rule only",
			rule:         ruleWith("rule-provider", "rule-model"),
			wantProvider: "rule-provider",
			wantModel:    "rule-model",
		},
		{
			name:         "scenario only",
			rule:         &typ.Rule{},
			ext:          scenarioExt("scenario-provider", "scenario-model"),
			wantProvider: "scenario-provider",
			wantModel:    "scenario-model",
		},
		{
			name: "neither",
			rule: &typ.Rule{},
		},
		{
			// A half-filled rule value is not a configuration, so it must not
			// shadow a complete scenario value.
			name:         "rule missing model falls back to scenario",
			rule:         ruleWith("rule-provider", ""),
			ext:          scenarioExt("scenario-provider", "scenario-model"),
			wantProvider: "scenario-provider",
			wantModel:    "scenario-model",
		},
		{
			name:         "nil rule with scenario",
			rule:         nil,
			ext:          scenarioExt("scenario-provider", "scenario-model"),
			wantProvider: "scenario-provider",
			wantModel:    "scenario-model",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(testConfig(scenario, tc.ext), scenario, tc.rule)
			if tc.wantProvider == "" {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected a resolved service, got nil")
			}
			if got.Provider != tc.wantProvider || got.Model != tc.wantModel {
				t.Fatalf("got %s/%s, want %s/%s", got.Provider, got.Model, tc.wantProvider, tc.wantModel)
			}
		})
	}
}

func TestParseScenarioService(t *testing.T) {
	tests := []struct {
		name string
		ext  map[string]interface{}
		want bool
	}{
		{name: "nil extensions"},
		{name: "missing key", ext: map[string]interface{}{"other": 1}},
		{name: "wrong shape", ext: map[string]interface{}{config.ExtensionWebProxyService: "not-an-object"}},
		{name: "missing provider", ext: scenarioExt("", "m")},
		{name: "missing model", ext: scenarioExt("p", "")},
		{name: "complete", ext: scenarioExt("p", "m"), want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseScenarioService(tc.ext)
			if tc.want != (got != nil) {
				t.Fatalf("ParseScenarioService(%v) = %+v, want non-nil=%v", tc.ext, got, tc.want)
			}
		})
	}
}

func TestContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if ActiveInContext(ctx) {
		t.Fatal("bare context must not report the web proxy as active")
	}

	// A half-filled reference is not a configuration and must not activate.
	ctx = WithService(ctx, &typ.WebProxyService{Provider: "p"})
	if ActiveInContext(ctx) {
		t.Fatal("half-filled service must not activate the web proxy")
	}

	ctx = WithService(ctx, &typ.WebProxyService{Provider: "p", Model: "m"})
	if !ActiveInContext(ctx) {
		t.Fatal("expected the web proxy to be active")
	}
	if got := ServiceFromContext(ctx); got == nil || got.Model != "m" {
		t.Fatalf("ServiceFromContext = %+v, want model m", got)
	}
}

func TestToLoadBalanceService(t *testing.T) {
	if svc := toLoadBalanceService(nil); svc != nil {
		t.Fatalf("nil reference must not produce a service, got %+v", svc)
	}
	if svc := toLoadBalanceService(&typ.WebProxyService{Provider: "p"}); svc != nil {
		t.Fatalf("half-filled reference must not produce a service, got %+v", svc)
	}
	svc := toLoadBalanceService(&typ.WebProxyService{Provider: "p", Model: "m"})
	if svc == nil || !svc.Active || svc.Provider != "p" || svc.Model != "m" {
		t.Fatalf("toLoadBalanceService = %+v, want active p/m", svc)
	}
}
