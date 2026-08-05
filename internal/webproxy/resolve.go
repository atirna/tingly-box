package webproxy

import (
	"context"

	"github.com/tingly-dev/tingly-box/internal/loadbalance"
	"github.com/tingly-dev/tingly-box/internal/server/config"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// Resolve picks the effective web service for this request. Rule level wins
// over scenario level when both are set — the more specific scope is taken to
// be the user's intent. Returns nil when neither scope configures a usable
// {provider, model}, in which case the proxy is skipped entirely.
//
// There is no separate on/off flag: "enabled" is exactly "a service is
// configured", the same single-source-of-truth rule the vision proxy uses.
func Resolve(cfg *config.Config, scenarioType typ.RuleScenario, rule *typ.Rule) *typ.WebProxyService {
	if rule != nil && rule.Flags.WebProxyService.IsActive() {
		return rule.Flags.WebProxyService
	}
	if cfg != nil {
		if scCfg := cfg.GetScenarioConfig(scenarioType); scCfg != nil {
			if svc := ParseScenarioService(scCfg.Extensions); svc != nil {
				return svc
			}
		}
	}
	return nil
}

// ParseScenarioService reads the scenario-level web service from a scenario's
// Extensions map (stored as a nested object under
// config.ExtensionWebProxyService; a map[string]interface{} after JSON/YAML
// unmarshal). Returns nil when absent, malformed, or half-filled.
func ParseScenarioService(extensions map[string]interface{}) *typ.WebProxyService {
	if extensions == nil {
		return nil
	}
	raw, ok := extensions[config.ExtensionWebProxyService]
	if !ok {
		return nil
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	provider, _ := m["provider"].(string)
	model, _ := m["model"].(string)
	svc := &typ.WebProxyService{Provider: provider, Model: model}
	if !svc.IsActive() {
		return nil
	}
	return svc
}

// toLoadBalanceService wraps a resolved reference into the loadbalance.Service
// shape the upstream clients expect, or nil when the reference is unusable.
func toLoadBalanceService(ref *typ.WebProxyService) *loadbalance.Service {
	if !ref.IsActive() {
		return nil
	}
	return &loadbalance.Service{
		Provider: ref.Provider,
		Model:    ref.Model,
		Active:   true,
		Weight:   1,
	}
}

// ctxKey is the private context key type for the resolved web service.
type ctxKey struct{}

// WithService attaches the resolved web service to ctx. The request path
// resolves the service once (at the single rule-flag merge point) and the
// server-side tool loop reads it back at tool-execution time, which happens
// deep inside the dispatch layer where the rule is no longer in hand.
//
// This mirrors how the resolved custom User-Agent and the 1M-context hint are
// carried: one merge point writes, the deep consumer reads.
func WithService(ctx context.Context, svc *typ.WebProxyService) context.Context {
	if !svc.IsActive() {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, svc)
}

// ServiceFromContext returns the web service attached by WithService, or nil
// when the web proxy is not active for this request.
func ServiceFromContext(ctx context.Context) *typ.WebProxyService {
	if ctx == nil {
		return nil
	}
	svc, _ := ctx.Value(ctxKey{}).(*typ.WebProxyService)
	return svc
}

// ActiveInContext reports whether the web proxy is active for this request.
// The dispatch layer uses it to open the server-side tool loop for requests
// that would otherwise skip it (i.e. when MCP is disabled).
func ActiveInContext(ctx context.Context) bool {
	return ServiceFromContext(ctx) != nil
}
