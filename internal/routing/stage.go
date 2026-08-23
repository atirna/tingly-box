package routing

import (
	"github.com/tingly-dev/tingly-box/internal/loadbalance"
)

// SelectionStage is one step in the service-selection pipeline. Stages are
// pure with respect to the pipeline's control flow: each receives the
// current candidate set as an explicit argument and returns what it asserts
// the set now is. There is no shared mutable state between stages, and no
// "nil means unchanged" — a stage with no opinion returns its input slice
// unchanged.
//
// Stages may only READ fields on the *loadbalance.Service values in
// candidates; they must not write to them (Service.InitializeStats's
// idempotent lazy-init is the one sanctioned exception — see its doc
// comment). Narrowing must allocate a fresh slice — never truncate or
// append onto the input slice's backing array — so a caller's slice is
// never mutated out from under it.
type SelectionStage interface {
	// Name returns the stage identifier for logging, metrics, and the
	// Source* constants on SelectionResult.
	Name() string

	// Evaluate narrows candidates (or returns them unchanged) and reports
	// whether this stage picked the final winner.
	//
	//   - err != nil: this stage could not be evaluated; the pipeline
	//     aborts and the error propagates to the caller of Select.
	//   - final != nil: this stage selected the winning service; the
	//     pipeline stops here. The returned narrowed slice is ignored in
	//     this case.
	//   - otherwise: narrowed becomes the input candidate set for the next
	//     stage (or, if this is the last stage, the pipeline reports "no
	//     service available").
	Evaluate(ctx *SelectionContext, candidates []*loadbalance.Service) (narrowed []*loadbalance.Service, final *SelectionResult, err error)
}
