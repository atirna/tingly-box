package webproxy

import (
	"encoding/json"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/tingly-dev/tingly-box/internal/protocol/request"
	protocoltransform "github.com/tingly-dev/tingly-box/internal/protocol/transform"
)

// ToolTransform is the request half of the web proxy. It runs in the chain's
// preVendor slot — after protocol conversion, so it sees the upstream-bound
// shape and can only ever inject tools the target provider will actually
// receive.
//
// Two edits, in this order:
//
//  1. Strip *provider-executed* web tools. Anthropic's web_search_20250305 and
//     friends are server tools: the provider runs them. A downstream that does
//     not implement them either rejects the request or silently drops the
//     capability, so the declaration is removed when the proxy is active.
//     Client-executed web tools (Claude Code's `WebSearch` / `WebFetch`) are
//     explicitly NOT stripped — see nativeWebToolNames for why.
//  2. Inject the two web proxy function tools, so the downstream model still
//     has a way to ask for a search or a fetch. Service.Execute answers those
//     calls from the borrowed service.
//
// Both edits are skipped for request shapes the server-side tool loop does not
// cover — see the Responses case below. Stripping still happens there; only
// injection is withheld, because an injected tool nobody can execute would
// leak an unanswerable tool call to the client.
type ToolTransform struct {
	active bool
}

// NewToolTransform builds the transform. active is the resolved
// "is the web proxy configured for this request" decision; an inactive
// transform is a no-op, so callers may add it unconditionally.
func NewToolTransform(active bool) *ToolTransform {
	return &ToolTransform{active: active}
}

func (t *ToolTransform) Name() string { return "web_proxy_tools" }

func (t *ToolTransform) Apply(ctx *protocoltransform.TransformContext) error {
	if t == nil || !t.active || ctx == nil {
		return nil
	}

	switch req := ctx.Request.(type) {
	case *openai.ChatCompletionNewParams:
		req.Tools = mergeOpenAITools(stripOpenAINativeWebTools(req.Tools), InjectedTools())
	case *anthropic.MessageNewParams:
		req.Tools = mergeAnthropicV1Tools(stripAnthropicV1NativeWebTools(req.Tools), injectedAnthropicV1Tools())
	case *anthropic.BetaMessageNewParams:
		req.Tools = mergeAnthropicBetaTools(stripAnthropicBetaNativeWebTools(req.Tools), injectedAnthropicBetaTools())
	case *responses.ResponseNewParams:
		// Responses-target requests never enter the server-side tool loop, so
		// the proxy cannot answer a call to an injected tool. Strip the native
		// web tools the downstream cannot run, and stop there.
		req.Tools = stripResponsesNativeWebTools(req.Tools)
	}

	return nil
}

// injectedAnthropicBetaTools derives the Anthropic beta tool shapes from the
// canonical OpenAI definitions, reusing the same converter the MCP tool
// injection path uses so the schemas cannot drift between protocols.
func injectedAnthropicBetaTools() []anthropic.BetaToolUnionParam {
	return request.ConvertOpenAIToAnthropicTools(InjectedTools())
}

// injectedAnthropicV1Tools re-encodes the beta shapes into the v1 union. The
// two unions are structurally identical for plain function tools, and this is
// the same round-trip the MCP injection transform performs.
func injectedAnthropicV1Tools() []anthropic.ToolUnionParam {
	beta := injectedAnthropicBetaTools()
	if len(beta) == 0 {
		return nil
	}
	b, err := json.Marshal(beta)
	if err != nil {
		return nil
	}
	var v1 []anthropic.ToolUnionParam
	if err := json.Unmarshal(b, &v1); err != nil {
		return nil
	}
	return v1
}

// ── Native web tool stripping ──────────────────────────────────────────────
//
// Anthropic declares its web tools as dedicated union members, so the check is
// a nil test per known version. OpenAI Chat has no native web tool member —
// providers expose search either through a bare function tool name or through
// the Responses API — so the Chat path matches on the conventional names.

func stripAnthropicBetaNativeWebTools(tools []anthropic.BetaToolUnionParam) []anthropic.BetaToolUnionParam {
	if len(tools) == 0 {
		return tools
	}
	out := tools[:0:len(tools)]
	for _, tool := range tools {
		if tool.OfWebSearchTool20250305 != nil || tool.OfWebSearchTool20260209 != nil {
			continue
		}
		if tool.OfWebFetchTool20250910 != nil || tool.OfWebFetchTool20260209 != nil || tool.OfWebFetchTool20260309 != nil {
			continue
		}
		out = append(out, tool)
	}
	return out
}

func stripAnthropicV1NativeWebTools(tools []anthropic.ToolUnionParam) []anthropic.ToolUnionParam {
	if len(tools) == 0 {
		return tools
	}
	out := tools[:0:len(tools)]
	for _, tool := range tools {
		if tool.OfWebSearchTool20250305 != nil || tool.OfWebSearchTool20260209 != nil {
			continue
		}
		if tool.OfWebFetchTool20250910 != nil || tool.OfWebFetchTool20260209 != nil || tool.OfWebFetchTool20260309 != nil {
			continue
		}
		out = append(out, tool)
	}
	return out
}

// nativeWebToolNames are function-tool names that unambiguously denote a
// *provider-executed* web tool — one the downstream cannot run.
//
// This list is deliberately short. "Web tool" is not the test; "the provider
// has to execute it" is. A client that ships its own web tools and runs them
// itself — Claude Code's `WebSearch` / `WebFetch` are the canonical example —
// needs no help from us: the model emits a tool_use, the client performs the
// search or the fetch, and the downstream model never needed web access at
// all. Stripping those would delete a working capability and replace it with a
// slower, costlier round-trip through a second model that has none of the
// client's domain permissions or safety checks.
//
// So `WebSearch` / `WebFetch` / bare `web_search` / bare `web_fetch` are left
// alone. Only `web_search_preview` — OpenAI's own server-tool name, which has
// no client-executed meaning — is matched here. Provider-executed tools that
// travel as typed union members (Anthropic's `web_search_20250305`, Responses'
// `OfWebSearch`) are matched structurally instead, which is unambiguous.
var nativeWebToolNames = map[string]struct{}{
	"web_search_preview": {},
}

func isNativeWebToolName(name string) bool {
	_, ok := nativeWebToolNames[strings.ToLower(name)]
	return ok
}

func stripOpenAINativeWebTools(tools []openai.ChatCompletionToolUnionParam) []openai.ChatCompletionToolUnionParam {
	if len(tools) == 0 {
		return tools
	}
	out := tools[:0:len(tools)]
	for _, tool := range tools {
		if fn := tool.GetFunction(); fn != nil && isNativeWebToolName(fn.Name) {
			continue
		}
		out = append(out, tool)
	}
	return out
}

func stripResponsesNativeWebTools(tools []responses.ToolUnionParam) []responses.ToolUnionParam {
	if len(tools) == 0 {
		return tools
	}
	out := tools[:0:len(tools)]
	for _, tool := range tools {
		if tool.OfWebSearch != nil || tool.OfWebSearchPreview != nil {
			continue
		}
		if tool.OfFunction != nil && isNativeWebToolName(tool.OfFunction.Name) {
			continue
		}
		out = append(out, tool)
	}
	return out
}

// ── Merging ────────────────────────────────────────────────────────────────
//
// Existing tools always win on a name collision: the client's own declaration
// is authoritative, and re-declaring a name would make the request invalid.

func mergeOpenAITools(existing, injected []openai.ChatCompletionToolUnionParam) []openai.ChatCompletionToolUnionParam {
	seen := make(map[string]struct{}, len(existing))
	for _, t := range existing {
		if fn := t.GetFunction(); fn != nil && fn.Name != "" {
			seen[fn.Name] = struct{}{}
		}
	}
	out := existing
	for _, t := range injected {
		fn := t.GetFunction()
		if fn == nil || fn.Name == "" {
			continue
		}
		if _, dup := seen[fn.Name]; dup {
			continue
		}
		out = append(out, t)
	}
	return out
}

func mergeAnthropicV1Tools(existing, injected []anthropic.ToolUnionParam) []anthropic.ToolUnionParam {
	seen := make(map[string]struct{}, len(existing))
	for _, t := range existing {
		if t.OfTool != nil && t.OfTool.Name != "" {
			seen[t.OfTool.Name] = struct{}{}
		}
	}
	out := existing
	for _, t := range injected {
		if t.OfTool == nil || t.OfTool.Name == "" {
			continue
		}
		if _, dup := seen[t.OfTool.Name]; dup {
			continue
		}
		out = append(out, t)
	}
	return out
}

func mergeAnthropicBetaTools(existing, injected []anthropic.BetaToolUnionParam) []anthropic.BetaToolUnionParam {
	seen := make(map[string]struct{}, len(existing))
	for _, t := range existing {
		if t.OfTool != nil && t.OfTool.Name != "" {
			seen[t.OfTool.Name] = struct{}{}
		}
	}
	out := existing
	for _, t := range injected {
		if t.OfTool == nil || t.OfTool.Name == "" {
			continue
		}
		if _, dup := seen[t.OfTool.Name]; dup {
			continue
		}
		out = append(out, t)
	}
	return out
}
