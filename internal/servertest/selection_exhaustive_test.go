package servertest

import (
	"fmt"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tingly-dev/tingly-box/internal/config"
	"github.com/tingly-dev/tingly-box/internal/loadbalance"
	server "github.com/tingly-dev/tingly-box/internal/protocolserver"
	"github.com/tingly-dev/tingly-box/internal/routing"
	"github.com/tingly-dev/tingly-box/internal/routing/smartrouting"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// svcState is one service's full runtime state: config (Active), health
// monitor (429/auth channel), and circuit breaker (5xx channel).
type svcState struct {
	active    bool
	unhealthy bool
	tripped   bool
}

func (s svcState) String() string {
	code := func(b bool, y, n byte) byte {
		if b {
			return y
		}
		return n
	}
	return string([]byte{code(s.active, 'A', 'a'), code(s.unhealthy, 'U', 'u'), code(s.tripped, 'T', 't')})
}

func allSvcStates() []svcState {
	out := make([]svcState, 0, 8)
	for _, active := range []bool{true, false} {
		for _, unhealthy := range []bool{true, false} {
			for _, tripped := range []bool{true, false} {
				out = append(out, svcState{active, unhealthy, tripped})
			}
		}
	}
	return out
}

// TestExhaustive_SelectionDegradeInvariant enumerates every combination of
// service runtime state across the selection pipeline's dimensions and asserts
// the degrade invariant on the REAL stack (ServiceSelector + LoadBalancer +
// HealthFilter + breaker store):
//
//	If the scope a request resolves to (base pool for non-matching requests,
//	base ∪ partition for partition-matching requests) contains at least one
//	ACTIVE service, selection must return an active service from the rule —
//	no combination of health-monitor or breaker state may fail the rule.
//	Only a scope with zero active services may error (a genuine config
//	problem).
//
// Dimensions:
//   - 1 or 2 base services × each service in 8 states (Active × unhealthy ×
//     breaker-tripped)
//   - no smart partition | 1 partition service in 8 states
//   - request: does not match any partition | matches the partition
//   - tactic: random | tier
func TestExhaustive_SelectionDegradeInvariant(t *testing.T) {
	appConfig, err := config.NewAppConfig(config.WithConfigDir(t.TempDir()))
	require.NoError(t, err)
	cfg := appConfig.GetGlobalConfig()

	providers := map[string]string{
		"base1": uuid.New().String(),
		"base2": uuid.New().String(),
		"part":  uuid.New().String(),
	}
	for name, id := range providers {
		require.NoError(t, cfg.AddProvider(&typ.Provider{
			UUID: id, Name: name, APIBase: "https://example.invalid", Token: "sk", Enabled: true,
		}))
	}

	// An op that matches every request (token count >= 0).
	alwaysMatchOp := smartrouting.SmartOp{
		Position:  smartrouting.PositionToken,
		Operation: smartrouting.OpTokenGe,
		Value:     "0",
	}
	matchingReq := &anthropic.MessageNewParams{
		Model:    "req-model",
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hello"))},
	}

	states := allSvcStates()
	tactics := map[string]typ.Tactic{
		"random": {Type: loadbalance.TacticRandom, Params: typ.NewRandomParams()},
		"tier":   {Type: loadbalance.TacticTier, Params: typ.DefaultTierParams()},
	}

	// partitionStates[0] == nil means "no partition".
	partitionStates := make([]*svcState, 0, len(states)+1)
	partitionStates = append(partitionStates, nil)
	for i := range states {
		partitionStates = append(partitionStates, &states[i])
	}

	caseCount := 0
	runCase := func(t *testing.T, tacticName string, tactic typ.Tactic, baseStates []svcState, partState *svcState, requestMatches bool) {
		caseCount++
		ruleUUID := fmt.Sprintf("ex-%d", caseCount)

		newSvc := func(provider, model string, st svcState) *loadbalance.Service {
			return &loadbalance.Service{
				Provider: providers[provider], Model: model,
				Weight: 1, Active: st.active, TimeWindow: 300,
			}
		}
		baseSvcs := make([]*loadbalance.Service, len(baseStates))
		for i, st := range baseStates {
			baseSvcs[i] = newSvc(fmt.Sprintf("base%d", i+1), fmt.Sprintf("base-model-%d", i+1), st)
		}
		rule := &typ.Rule{
			Scenario:     typ.ScenarioClaudeCode,
			RequestModel: "req-model",
			UUID:         ruleUUID,
			LBTactic:     tactic,
			Services:     baseSvcs,
			Active:       true,
		}
		var partSvc *loadbalance.Service
		if partState != nil {
			partSvc = newSvc("part", "part-model", *partState)
			rule.SmartEnabled = true
			rule.SmartRouting = []smartrouting.SmartRouting{{
				Description: "always matches",
				Ops:         []smartrouting.SmartOp{alwaysMatchOp},
				Services:    []*loadbalance.Service{partSvc},
			}}
		}

		healthMonitor := loadbalance.NewHealthMonitor(loadbalance.DefaultHealthMonitorConfig())
		selector := routing.NewServiceSelector(cfg, server.NewAffinityStore(0),
			server.NewLoadBalancer(cfg, routing.NewHealthFilter(healthMonitor)))

		applyState := func(svc *loadbalance.Service, st svcState) {
			if st.unhealthy {
				healthMonitor.ReportRateLimit(svc.ServiceID())
			}
			if st.tripped {
				for i := 0; i < loadbalance.DefaultBreakerFailureThreshold; i++ {
					loadbalance.RecordServiceFailure(ruleUUID, svc.ServiceID())
				}
			}
		}
		for i, st := range baseStates {
			applyState(baseSvcs[i], st)
		}
		if partState != nil {
			applyState(partSvc, *partState)
		}

		ctx := &routing.SelectionContext{Rule: rule, MatchedSmartRuleIndex: -1}
		if requestMatches {
			ctx.Request = matchingReq
		}

		// The scope this request resolves to. Non-matching requests must be
		// served from the base pool; matching requests may be served from the
		// partition or degrade back to the base pool.
		scope := baseSvcs
		if requestMatches && partState != nil {
			scope = append(append([]*loadbalance.Service{}, baseSvcs...), partSvc)
		}
		activeInScope := 0
		for _, svc := range scope {
			if svc.Active {
				activeInScope++
			}
		}

		res, err := selector.Select(ctx)
		if activeInScope == 0 {
			require.Error(t, err, "no active service in scope is a config error and must be reported")
			return
		}
		require.NoErrorf(t, err,
			"selection must not fail while %d active services are in scope", activeInScope)
		require.NotNil(t, res)
		require.True(t, res.Service.Active, "an inactive service must never be selected")
		found := false
		for _, svc := range scope {
			if svc.ServiceID() == res.Service.ServiceID() {
				found = true
			}
		}
		require.Truef(t, found, "selected %s outside the request's scope", res.Service.ServiceID())
	}

	for tacticName, tactic := range tactics {
		// One base service.
		for _, b1 := range states {
			for _, part := range partitionStates {
				matchModes := []bool{false}
				if part != nil {
					matchModes = []bool{false, true}
				}
				for _, matches := range matchModes {
					name := fmt.Sprintf("%s/base=%s/part=%v/match=%v", tacticName, b1, part, matches)
					t.Run(name, func(t *testing.T) {
						runCase(t, tacticName, tactic, []svcState{b1}, part, matches)
					})
				}
			}
		}
		// Two base services.
		for _, b1 := range states {
			for _, b2 := range states {
				for _, part := range partitionStates {
					matchModes := []bool{false}
					if part != nil {
						matchModes = []bool{false, true}
					}
					for _, matches := range matchModes {
						name := fmt.Sprintf("%s/base=%s,%s/part=%v/match=%v", tacticName, b1, b2, part, matches)
						t.Run(name, func(t *testing.T) {
							runCase(t, tacticName, tactic, []svcState{b1, b2}, part, matches)
						})
					}
				}
			}
		}
	}
	t.Logf("exhaustive cases: %d", caseCount)
}
