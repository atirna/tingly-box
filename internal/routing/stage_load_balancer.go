package routing

import (
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/tingly-dev/tingly-box/internal/loadbalance"
)

// LoadBalancerStage performs standard load balancing across all rule services.
// This stage always returns a service (or error), acting as the final fallback.
type LoadBalancerStage struct {
	loadBalancer LoadBalancer
}

// NewLoadBalancerStage creates a new load balancer stage
func NewLoadBalancerStage(lb LoadBalancer) *LoadBalancerStage {
	return &LoadBalancerStage{
		loadBalancer: lb,
	}
}

// Name returns the stage identifier
func (s *LoadBalancerStage) Name() string {
	return SourceLoadBalancer
}

// Evaluate selects a service using load balancing. This is the terminal
// stage: a failure here is a real error (there is no next stage to fall
// through to), so it is reported via err rather than a silent pass-through.
func (s *LoadBalancerStage) Evaluate(ctx *SelectionContext, candidates []*loadbalance.Service) ([]*loadbalance.Service, *SelectionResult, error) {
	// Degrade, don't disappear: an upstream stage must never leave the
	// terminal stage with nothing to pick while the rule itself still has
	// active services configured. Whatever emptied the set (health filtering,
	// smart-routing narrowing, a future stage), fall back to the rule's own
	// active pool so the request reaches an upstream and the client sees the
	// real upstream error instead of a "no service available" routing error.
	if len(candidates) == 0 {
		if fallback := activeBaseFallback(ctx, ctx.Rule, "routing_lb_candidates_degrade"); fallback != nil {
			candidates = fallback
		}
	}

	tempRule := *ctx.Rule
	tempRule.Services = candidates
	logOpenBreakerSkips(ctx, &tempRule)

	service, err := s.loadBalancer.SelectService(&tempRule)
	if err != nil {
		return candidates, nil, fmt.Errorf("selection failed: %w", err)
	}

	if service == nil {
		return candidates, nil, fmt.Errorf("no service returned")
	}

	// When a smart partition matched, the candidate set IS that partition, so
	// label the pick smart_routing — preserving the pre-reorder observability
	// contract (source=smart_routing ⇒ picked from a smart-matched subset).
	source := SourceLoadBalancer
	if ctx.MatchedSmartRuleIndex >= 0 {
		source = SourceSmartRouting
	}
	result := NewResult(service, source)
	result.MatchedSmartRuleIndex = ctx.MatchedSmartRuleIndex
	return candidates, result, nil
}

func logOpenBreakerSkips(ctx *SelectionContext, rule interface {
	GetTacticType() loadbalance.TacticType
	GetActiveServices() []*loadbalance.Service
}) {
	if ctx == nil || ctx.Rule == nil || rule == nil || rule.GetTacticType() != loadbalance.TacticTier {
		return
	}
	store := loadbalance.DefaultBreakerStore()
	for _, svc := range rule.GetActiveServices() {
		if svc == nil {
			continue
		}
		state := store.Get(ctx.Rule.UUID, svc.ServiceID()).State()
		if state != loadbalance.BreakerOpen {
			continue
		}
		logrus.WithContext(selectionLogContext(ctx)).WithFields(logrus.Fields{
			"stage":         "routing_breaker_skipped",
			"rule_uuid":     ctx.Rule.UUID,
			"scenario":      string(ctx.Scenario),
			"request_model": ctx.Rule.RequestModel,
			"lb_tactic":     ctx.Rule.GetTacticType().String(),
			"service":       svc.ServiceID(),
			"provider_uuid": svc.Provider,
			"attempt_model": svc.Model,
			"tier":          svc.Tier,
			"breaker_state": state.String(),
		}).Warnf("[routing] skipped %s because breaker is %s", svc.ServiceID(), state.String())
	}
}
