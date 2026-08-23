package routing

import (
	"github.com/tingly-dev/tingly-box/internal/loadbalance"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// Selection sources: which stage produced a SelectionResult. Also used as the
// stage names in EvaluatedStages and observability output.
const (
	SourceHealth       = "health"
	SourceAffinity     = "affinity"
	SourceSmartRouting = "smart_routing"
	SourceLoadBalancer = "load_balancer"
	// SourceProbePin marks the X-Tingly-Probe-Service bypass, which pins a
	// specific service without running the pipeline.
	SourceProbePin = "probe_pin"
)

// SelectionResult represents the output of service selection pipeline.
// It includes the selected service, provider, and metadata about the selection.
// A SelectionResult only ever represents a TERMINAL pick (a stage's `final`
// return value) — non-terminal narrowing is carried by the `narrowed` slice
// SelectionStage.Evaluate returns alongside it, not by this struct.
type SelectionResult struct {
	// Service is the selected load-balanced service
	Service *loadbalance.Service

	// Provider is the resolved provider for the service
	Provider *typ.Provider

	// Source indicates which stage selected this service (one of the
	// Source* constants above).
	Source string

	// EvaluatedStages tracks which stages were evaluated (for observability)
	EvaluatedStages []string

	// MatchedSmartRuleIndex is the index of the matched smart routing rule
	// -1 if no smart routing rule matched
	MatchedSmartRuleIndex int
}

// NewResult creates a new selection result with the given service and source
func NewResult(service *loadbalance.Service, source string) *SelectionResult {
	return &SelectionResult{
		Service:               service,
		Source:                source,
		EvaluatedStages:       []string{},
		MatchedSmartRuleIndex: -1,
	}
}
