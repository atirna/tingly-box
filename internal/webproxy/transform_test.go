package webproxy

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	protocoltransform "github.com/tingly-dev/tingly-box/internal/protocol/transform"
)

func applyTransform(t *testing.T, active bool, req any) {
	t.Helper()
	tr := NewToolTransform(active)
	if err := tr.Apply(&protocoltransform.TransformContext{Request: req}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func openAIToolNames(tools []openai.ChatCompletionToolUnionParam) []string {
	var out []string
	for _, tool := range tools {
		if fn := tool.GetFunction(); fn != nil {
			out = append(out, fn.Name)
		}
	}
	return out
}

func anthropicBetaToolNames(tools []anthropic.BetaToolUnionParam) []string {
	var out []string
	for _, tool := range tools {
		if tool.OfTool != nil {
			out = append(out, tool.OfTool.Name)
		}
	}
	return out
}

func contains(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestToolTransform_InactiveIsNoOp(t *testing.T) {
	req := &openai.ChatCompletionNewParams{
		Tools: []openai.ChatCompletionToolUnionParam{
			openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{Name: "web_search"}),
		},
	}
	applyTransform(t, false, req)

	names := openAIToolNames(req.Tools)
	if len(names) != 1 || names[0] != "web_search" {
		t.Fatalf("inactive transform must leave tools untouched, got %v", names)
	}
}

func TestToolTransform_OpenAIChat(t *testing.T) {
	req := &openai.ChatCompletionNewParams{
		Tools: []openai.ChatCompletionToolUnionParam{
			openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{Name: "Read"}),
			openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{Name: "web_search_preview"}),
		},
	}
	applyTransform(t, true, req)

	names := openAIToolNames(req.Tools)
	if contains(names, "web_search_preview") {
		t.Fatalf("provider-executed web tools must be stripped, got %v", names)
	}
	if !contains(names, "Read") {
		t.Fatalf("unrelated client tools must survive, got %v", names)
	}
	if !contains(names, NameWebSearch) || !contains(names, NameWebFetch) {
		t.Fatalf("both proxy tools must be injected, got %v", names)
	}
}

// Client-executed web tools must survive untouched. Claude Code declares
// `WebSearch` / `WebFetch` as ordinary tools and performs the search or the
// fetch itself — the downstream model never needs web access for them to work.
// Stripping them would delete a working capability (with the client's domain
// permissions and safety checks) and substitute a worse one.
func TestToolTransform_LeavesClientExecutedWebToolsAlone(t *testing.T) {
	clientTools := []string{"WebSearch", "WebFetch", "web_search", "web_fetch"}

	t.Run("openai chat", func(t *testing.T) {
		var tools []openai.ChatCompletionToolUnionParam
		for _, name := range clientTools {
			tools = append(tools, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{Name: name}))
		}
		req := &openai.ChatCompletionNewParams{Tools: tools}
		applyTransform(t, true, req)

		names := openAIToolNames(req.Tools)
		for _, name := range clientTools {
			if !contains(names, name) {
				t.Errorf("client-executed tool %q was stripped; tools=%v", name, names)
			}
		}
	})

	t.Run("anthropic beta", func(t *testing.T) {
		var tools []anthropic.BetaToolUnionParam
		for _, name := range clientTools {
			tools = append(tools, anthropic.BetaToolUnionParam{OfTool: &anthropic.BetaToolParam{Name: name}})
		}
		req := &anthropic.BetaMessageNewParams{Tools: tools}
		applyTransform(t, true, req)

		names := anthropicBetaToolNames(req.Tools)
		for _, name := range clientTools {
			if !contains(names, name) {
				t.Errorf("client-executed tool %q was stripped; tools=%v", name, names)
			}
		}
	})

	// Same client tool, same fate, whatever the target protocol — the two
	// paths above used to disagree (Anthropic kept `WebSearch`, OpenAI Chat
	// stripped it) because one matched structurally and the other by name.
}

func TestToolTransform_AnthropicBetaStripsNativeServerTools(t *testing.T) {
	req := &anthropic.BetaMessageNewParams{
		Tools: []anthropic.BetaToolUnionParam{
			{OfWebSearchTool20250305: &anthropic.BetaWebSearchTool20250305Param{}},
			{OfWebFetchTool20250910: &anthropic.BetaWebFetchTool20250910Param{}},
			{OfTool: &anthropic.BetaToolParam{Name: "Bash"}},
		},
	}
	applyTransform(t, true, req)

	for _, tool := range req.Tools {
		if tool.OfWebSearchTool20250305 != nil || tool.OfWebFetchTool20250910 != nil {
			t.Fatal("native Anthropic server web tools must be stripped")
		}
	}
	names := anthropicBetaToolNames(req.Tools)
	if !contains(names, "Bash") {
		t.Fatalf("unrelated client tools must survive, got %v", names)
	}
	if !contains(names, NameWebSearch) || !contains(names, NameWebFetch) {
		t.Fatalf("both proxy tools must be injected, got %v", names)
	}
}

func TestToolTransform_AnthropicV1(t *testing.T) {
	req := &anthropic.MessageNewParams{
		Tools: []anthropic.ToolUnionParam{
			{OfWebSearchTool20250305: &anthropic.WebSearchTool20250305Param{}},
			{OfTool: &anthropic.ToolParam{Name: "Bash"}},
		},
	}
	applyTransform(t, true, req)

	var names []string
	for _, tool := range req.Tools {
		if tool.OfWebSearchTool20250305 != nil {
			t.Fatal("native Anthropic v1 web_search must be stripped")
		}
		if tool.OfTool != nil {
			names = append(names, tool.OfTool.Name)
		}
	}
	if !contains(names, NameWebSearch) || !contains(names, NameWebFetch) {
		t.Fatalf("both proxy tools must be injected, got %v", names)
	}
}

// The Responses target never enters the server-side tool loop, so nothing can
// answer an injected tool call. Stripping still applies; injection must not.
func TestToolTransform_ResponsesStripsButDoesNotInject(t *testing.T) {
	req := &responses.ResponseNewParams{
		Tools: []responses.ToolUnionParam{
			responses.ToolParamOfWebSearchPreview(responses.WebSearchPreviewToolTypeWebSearchPreview),
			{OfFunction: &responses.FunctionToolParam{Name: "Read"}},
		},
	}
	applyTransform(t, true, req)

	for _, tool := range req.Tools {
		if tool.OfWebSearch != nil || tool.OfWebSearchPreview != nil {
			t.Fatal("native Responses web tools must be stripped")
		}
		if tool.OfFunction != nil && (tool.OfFunction.Name == NameWebSearch || tool.OfFunction.Name == NameWebFetch) {
			t.Fatal("proxy tools must not be injected on the Responses path")
		}
	}
	if len(req.Tools) != 1 || req.Tools[0].OfFunction == nil || req.Tools[0].OfFunction.Name != "Read" {
		t.Fatalf("unrelated client tools must survive, got %+v", req.Tools)
	}
}

// A client that already declares a tool by one of the proxy's namespaced names
// keeps its own definition — re-declaring the name would make the request
// invalid.
func TestToolTransform_DoesNotDuplicateExistingName(t *testing.T) {
	req := &openai.ChatCompletionNewParams{
		Tools: []openai.ChatCompletionToolUnionParam{
			openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{Name: NameWebSearch}),
		},
	}
	applyTransform(t, true, req)

	var seen int
	for _, name := range openAIToolNames(req.Tools) {
		if name == NameWebSearch {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("expected exactly one %s tool, got %d", NameWebSearch, seen)
	}
}

func TestToolNaming(t *testing.T) {
	if !IsWebProxyTool(NameWebSearch) || !IsWebProxyTool(NameWebFetch) {
		t.Fatal("proxy tool names must be recognised as web proxy tools")
	}
	if IsWebProxyTool("tingly_box_mcp__webtools__mcp_web_search") {
		t.Fatal("MCP webtools must not be claimed by the web proxy")
	}
	if IsWebProxyTool("web_search") {
		t.Fatal("bare native names must not be claimed by the web proxy")
	}
	if bare, ok := BareToolName(NameWebFetch); !ok || bare != ToolWebFetch {
		t.Fatalf("BareToolName(%s) = %q,%v", NameWebFetch, bare, ok)
	}
}
