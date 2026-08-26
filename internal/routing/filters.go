package routing

import (
	"github.com/sirupsen/logrus"

	"github.com/tingly-dev/tingly-box/internal/loadbalance"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// activeBaseFallback is the "degrade, don't disappear" fallback the pipeline
// driver (ServiceSelector.Select) applies whenever a stage's narrowing comes
// back empty: it returns the rule's active base services so the terminal
// stage still picks an upstream and the client sees the real upstream error
// instead of a "no service available" routing error. Returns nil when the
// rule has no active services at all — that is a genuine config problem the
// terminal stage should report. stage tags the warn log.
func activeBaseFallback(ctx *SelectionContext, rule *typ.Rule, stage string) []*loadbalance.Service {
	fallback := FilterActiveServices(rule.Services)
	if len(fallback) == 0 {
		return nil
	}
	logrus.WithContext(selectionLogContext(ctx)).WithFields(logrus.Fields{
		"stage":     stage,
		"rule_uuid": rule.UUID,
		"services":  len(fallback),
	}).Warnf("[routing] candidate set is empty; falling back to the rule's %d active services", len(fallback))
	return fallback
}

// Narrowing convention (SelectionStage.Evaluate implementations rely on
// this): every function below that returns a filtered []*loadbalance.Service
// allocates a fresh backing array via append-to-nil. None of them truncate
// or append onto an input slice's existing backing array, so a caller's
// slice — including one being threaded through the pipeline as another
// stage's `candidates` argument — is never mutated out from under it. Keep
// this property when adding new narrowing helpers here.

// FilterActiveServices returns only active services from the input list
func FilterActiveServices(services []*loadbalance.Service) []*loadbalance.Service {
	if len(services) == 0 {
		return services
	}

	var activeServices []*loadbalance.Service
	for _, service := range services {
		if service.Active {
			activeServices = append(activeServices, service)
		}
	}

	return activeServices
}

// ContainsService checks if target exists in services by service ID.
func ContainsService(services []*loadbalance.Service, target *loadbalance.Service) bool {
	if target == nil {
		return false
	}
	for _, service := range services {
		if service != nil && service.ServiceID() == target.ServiceID() {
			return true
		}
	}
	return false
}

// IntersectServices keeps services that are present in both lists.
func IntersectServices(left, right []*loadbalance.Service) []*loadbalance.Service {
	if len(left) == 0 || len(right) == 0 {
		return []*loadbalance.Service{}
	}

	allowed := make(map[string]struct{}, len(right))
	for _, svc := range right {
		if svc == nil {
			continue
		}
		allowed[svc.ServiceID()] = struct{}{}
	}

	var out []*loadbalance.Service
	for _, svc := range left {
		if svc == nil {
			continue
		}
		if _, ok := allowed[svc.ServiceID()]; ok {
			out = append(out, svc)
		}
	}
	return out
}
