package tool

import "context"

type advisorContextKey struct{}
type advisorDepthKey struct{}

// AdvisorContext holds conversation state for the advisor tool.
type AdvisorContext struct {
	Messages      []map[string]any
	UsesRemaining *int
}

// WithAdvisorContext attaches advisor context to the context.
func WithAdvisorContext(ctx context.Context, ac *AdvisorContext) context.Context {
	return context.WithValue(ctx, advisorContextKey{}, ac)
}

// GetAdvisorContext retrieves advisor context from the context.
func GetAdvisorContext(ctx context.Context) (*AdvisorContext, bool) {
	ac, ok := ctx.Value(advisorContextKey{}).(*AdvisorContext)
	return ac, ok
}

// WithAdvisorDepth sets the adviser call depth in context.
func WithAdvisorDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, advisorDepthKey{}, depth)
}

// GetAdvisorDepth retrieves the current adviser call depth from context.
func GetAdvisorDepth(ctx context.Context) int {
	v, _ := ctx.Value(advisorDepthKey{}).(int)
	return v
}
