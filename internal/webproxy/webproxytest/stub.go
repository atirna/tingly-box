// Package webproxytest holds the shared test doubles for the web proxy, so
// this package's own tests and the internal/protocolserver tests that drive
// Service.Execute through the real handler call order use the same fakes.
package webproxytest

import (
	"context"
	"sync"

	"github.com/tingly-dev/tingly-box/internal/loadbalance"
	"github.com/tingly-dev/tingly-box/internal/typ"
	"github.com/tingly-dev/tingly-box/internal/webproxy"
)

// StubWebClient records the calls it receives and returns canned answers.
// Safe for concurrent use; the tool loop executes calls sequentially today but
// the doubles should not be the reason that stays true.
type StubWebClient struct {
	mu sync.Mutex

	// SearchAnswer / FetchAnswer are returned verbatim when the matching
	// error field is nil.
	SearchAnswer string
	FetchAnswer  string
	SearchErr    error
	FetchErr     error

	SearchCalls []webproxy.SearchRequest
	FetchCalls  []webproxy.FetchRequest
	Services    []*loadbalance.Service
}

func (s *StubWebClient) Search(_ context.Context, service *loadbalance.Service, args webproxy.SearchRequest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SearchCalls = append(s.SearchCalls, args)
	s.Services = append(s.Services, service)
	return s.SearchAnswer, s.SearchErr
}

func (s *StubWebClient) Fetch(_ context.Context, service *loadbalance.Service, args webproxy.FetchRequest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.FetchCalls = append(s.FetchCalls, args)
	s.Services = append(s.Services, service)
	return s.FetchAnswer, s.FetchErr
}

// NewService wraps a StubWebClient in a real webproxy.Service, which is what
// the wiring under test actually holds.
func NewService(stub *StubWebClient) *webproxy.Service {
	return webproxy.NewService(stub)
}

// Ctx returns a context carrying the given provider/model as the resolved web
// service, matching what the request path attaches before dispatch.
func Ctx(ctx context.Context, provider, model string) context.Context {
	return webproxy.WithService(ctx, &typ.WebProxyService{Provider: provider, Model: model})
}
