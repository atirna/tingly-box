package webproxy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tingly-dev/tingly-box/internal/loadbalance"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// stubClient records what the service asked for and returns canned answers.
type stubClient struct {
	searchAnswer string
	fetchAnswer  string
	searchErr    error
	fetchErr     error

	searchCalls []SearchRequest
	fetchCalls  []FetchRequest
	services    []*loadbalance.Service
}

func (s *stubClient) Search(_ context.Context, svc *loadbalance.Service, args SearchRequest) (string, error) {
	s.searchCalls = append(s.searchCalls, args)
	s.services = append(s.services, svc)
	return s.searchAnswer, s.searchErr
}

func (s *stubClient) Fetch(_ context.Context, svc *loadbalance.Service, args FetchRequest) (string, error) {
	s.fetchCalls = append(s.fetchCalls, args)
	s.services = append(s.services, svc)
	return s.fetchAnswer, s.fetchErr
}

func activeCtx() context.Context {
	return WithService(context.Background(), &typ.WebProxyService{Provider: "p-uuid", Model: "web-model"})
}

func TestExecute_Search(t *testing.T) {
	stub := &stubClient{searchAnswer: "Go 1.26 shipped (https://go.dev)"}
	svc := NewService(stub)

	result, err := svc.Execute(activeCtx(), NameWebSearch,
		`{"query":"latest go release","allowed_domains":["go.dev"]}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.FirstText())
	}
	if got := result.FirstText(); got != stub.searchAnswer {
		t.Fatalf("result = %q, want %q", got, stub.searchAnswer)
	}
	if len(stub.searchCalls) != 1 {
		t.Fatalf("expected one search call, got %d", len(stub.searchCalls))
	}
	call := stub.searchCalls[0]
	if call.Query != "latest go release" {
		t.Fatalf("query = %q", call.Query)
	}
	if len(call.AllowedDomains) != 1 || call.AllowedDomains[0] != "go.dev" {
		t.Fatalf("allowed_domains = %v", call.AllowedDomains)
	}
	// The borrowed service must be the one resolved into the context.
	if got := stub.services[0]; got.Provider != "p-uuid" || got.Model != "web-model" {
		t.Fatalf("borrowed service = %s/%s", got.Provider, got.Model)
	}
}

func TestExecute_Fetch(t *testing.T) {
	stub := &stubClient{fetchAnswer: "# Title\nbody"}
	svc := NewService(stub)

	result, err := svc.Execute(activeCtx(), NameWebFetch,
		`{"url":"https://example.com","prompt":"the changelog"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError || result.FirstText() != stub.fetchAnswer {
		t.Fatalf("result = %+v", result)
	}
	if len(stub.fetchCalls) != 1 || stub.fetchCalls[0].URL != "https://example.com" {
		t.Fatalf("fetch calls = %+v", stub.fetchCalls)
	}
}

// Every failure must still hand the downstream model something to continue
// with: it is mid-tool-loop and an empty result would strand it.
func TestExecute_FailuresAreToolErrorsNotEmpty(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		svc      *Service
		tool     string
		args     string
		contains string
	}{
		{
			name:     "no service configured",
			ctx:      context.Background(),
			svc:      NewService(&stubClient{}),
			tool:     NameWebSearch,
			args:     `{"query":"x"}`,
			contains: "no web service configured",
		},
		{
			name:     "no client wired",
			ctx:      activeCtx(),
			svc:      NewService(nil),
			tool:     NameWebSearch,
			args:     `{"query":"x"}`,
			contains: "web client not configured",
		},
		{
			name:     "not a web proxy tool",
			ctx:      activeCtx(),
			svc:      NewService(&stubClient{}),
			tool:     "tingly_box_mcp__webtools__mcp_web_search",
			args:     `{"query":"x"}`,
			contains: "not a web proxy tool",
		},
		{
			name:     "missing query",
			ctx:      activeCtx(),
			svc:      NewService(&stubClient{}),
			tool:     NameWebSearch,
			args:     `{}`,
			contains: "requires a non-empty \\\"query\\\"",
		},
		{
			name:     "malformed arguments",
			ctx:      activeCtx(),
			svc:      NewService(&stubClient{}),
			tool:     NameWebFetch,
			args:     `not json`,
			contains: "requires a non-empty \\\"url\\\"",
		},
		{
			name:     "upstream error",
			ctx:      activeCtx(),
			svc:      NewService(&stubClient{searchErr: errors.New("429 rate limited")}),
			tool:     NameWebSearch,
			args:     `{"query":"x"}`,
			contains: "429 rate limited",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.svc.Execute(tc.ctx, tc.tool, tc.args)
			if err != nil {
				t.Fatalf("Execute must not return a transport error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("expected a tool error, got %+v", result)
			}
			if got := result.FirstText(); !strings.Contains(got, tc.contains) {
				t.Fatalf("result %q does not contain %q", got, tc.contains)
			}
		})
	}
}

// An empty answer is a legitimate outcome, not a failure: say so plainly so
// the downstream model stops retrying the same query.
func TestExecute_EmptyAnswerIsNotAnError(t *testing.T) {
	svc := NewService(&stubClient{searchAnswer: ""})
	result, err := svc.Execute(activeCtx(), NameWebSearch, `{"query":"x"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("empty answer must not be a tool error: %+v", result)
	}
	if result.FirstText() != "No results." {
		t.Fatalf("result = %q", result.FirstText())
	}
}

func TestHandles(t *testing.T) {
	svc := NewService(&stubClient{})
	if !svc.Handles(NameWebSearch) || !svc.Handles(NameWebFetch) {
		t.Fatal("service must claim its own tools")
	}
	if svc.Handles("tingly_box_mcp__builtin__advisor") {
		t.Fatal("service must not claim MCP tools")
	}
	var nilSvc *Service
	if nilSvc.Handles(NameWebSearch) {
		t.Fatal("a nil service must claim nothing")
	}
}
