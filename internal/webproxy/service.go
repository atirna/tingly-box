package webproxy

import (
	"context"
	"encoding/json"

	"github.com/sirupsen/logrus"

	"github.com/tingly-dev/tingly-box/internal/client"
	coretool "github.com/tingly-dev/tingly-box/internal/tool"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// Service is the execution half of the web proxy plugin, covering both the
// rule-level and scenario-level scopes. The request half is ToolTransform
// (transform.go); this side answers the tool calls that transform made
// possible.
type Service struct {
	Client WebClient
}

// NewService builds a Service around the given web client. A nil client makes
// Execute return a tool error rather than panicking, so a partially wired
// server degrades to "web proxy unavailable" instead of crashing a request.
func NewService(cl WebClient) *Service {
	return &Service{Client: cl}
}

// NewServiceFromPool builds a Service backed by the production web client,
// dispatching borrowed calls through the shared ClientPool. Called once during
// server boot, after the ClientPool and config (provider resolver) exist.
func NewServiceFromPool(pool *client.ClientPool, resolver providerResolver) *Service {
	return NewService(NewPoolWebClient(pool, resolver))
}

// Handles reports whether toolName is a web proxy tool this service should
// execute. The server-side tool loop calls this to decide whether to route a
// tool call here instead of into the MCP runtime.
func (s *Service) Handles(toolName string) bool {
	return s != nil && IsWebProxyTool(toolName)
}

// Execute runs one web proxy tool call against the service resolved for this
// request (read from ctx — see WithService). The returned ToolResult is fed
// back to the downstream model as the tool's output.
//
// Every failure path returns a non-nil ToolResult carrying an error payload,
// on top of the error: the downstream model is mid-tool-loop and needs
// *something* to continue with. Returning an empty result would strand it.
func (s *Service) Execute(ctx context.Context, toolName, arguments string) (coretool.ToolResult, error) {
	bare, ok := BareToolName(toolName)
	if !ok {
		return errResult("web proxy: not a web proxy tool: " + toolName)
	}

	ref := ServiceFromContext(ctx)
	svc := toLoadBalanceService(ref)
	if svc == nil {
		return errResult("web proxy: no web service configured for this request")
	}
	if s == nil || s.Client == nil {
		return errResult("web proxy: web client not configured")
	}

	log := logrus.WithContext(ctx).WithFields(logrus.Fields{
		"component": "web_proxy",
		"tool":      bare,
		"provider":  svc.Provider,
		"model":     svc.Model,
	})

	var (
		text string
		err  error
	)
	switch bare {
	case ToolWebSearch:
		args, valid := parseSearchArgs(arguments)
		if !valid {
			return errResult("web proxy: web_search requires a non-empty \"query\" argument")
		}
		log.Debugf("web search: %s", args.Query)
		text, err = s.Client.Search(ctx, svc, SearchRequest{
			Query:          args.Query,
			AllowedDomains: args.AllowedDomains,
			BlockedDomains: args.BlockedDomains,
		})
	case ToolWebFetch:
		args, valid := parseFetchArgs(arguments)
		if !valid {
			return errResult("web proxy: web_fetch requires a non-empty \"url\" argument")
		}
		log.Debugf("web fetch: %s", args.URL)
		text, err = s.Client.Fetch(ctx, svc, FetchRequest{URL: args.URL, Prompt: args.Prompt})
	default:
		return errResult("web proxy: unknown tool: " + bare)
	}

	if err != nil {
		log.WithError(err).Warn("web proxy call failed")
		return errResult("web proxy: " + err.Error())
	}
	if text == "" {
		// Not an error: the borrowed model ran and found nothing. Say so
		// plainly so the downstream model stops retrying the same query.
		return coretool.TextToolResult("No results."), nil
	}
	return coretool.TextToolResult(text), nil
}

// errResult builds the JSON error payload shape the server-side tool loop
// already uses for failed tool calls, so web proxy failures render the same
// way as MCP ones in both the model's context and the recordings.
func errResult(msg string) (coretool.ToolResult, error) {
	payload, _ := json.Marshal(map[string]string{"error": msg})
	return coretool.ErrorToolResult(string(payload)), nil
}

// Active reports whether the web proxy is configured for the given rule /
// scenario pair. Thin wrapper over Resolve for call sites that only need the
// boolean.
func Active(ref *typ.WebProxyService) bool { return ref.IsActive() }
