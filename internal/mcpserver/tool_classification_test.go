package mcpserver

import (
	"testing"

	coretool "github.com/tingly-dev/tingly-box/internal/tool"
)

// The web proxy's tools are server-executed but never registered in the MCP
// virtual registry, so they must be classified virtual by namespace alone —
// including when the registry is empty (MCP disabled).
func TestIsVirtualTool_WebProxyNamespace(t *testing.T) {
	empty := coretool.NewVirtualToolRegistry()

	for _, name := range []string{
		coretool.NormalizeToolName(coretool.WebProxySourceID, "web_search"),
		coretool.NormalizeToolName(coretool.WebProxySourceID, "web_fetch"),
	} {
		if !IsVirtualTool(name, empty) {
			t.Errorf("%s must be virtual with an empty registry", name)
		}
		if !IsVirtualTool(name, nil) {
			t.Errorf("%s must be virtual with a nil registry", name)
		}
	}

	// An unregistered MCP tool from another source stays external.
	other := coretool.NormalizeToolName("webtools", "mcp_web_search")
	if IsVirtualTool(other, empty) {
		t.Errorf("%s must not be virtual when it is not registered", other)
	}
	// A plain client tool is never virtual.
	if IsVirtualTool("web_search", empty) {
		t.Error("a bare tool name must not be virtual")
	}
}

// Every adapter must answer the classification question identically — they
// used to each carry their own copy of the logic.
func TestAdapters_IsVirtualToolAgree(t *testing.T) {
	empty := coretool.NewVirtualToolRegistry()

	name := coretool.NormalizeToolName(coretool.WebProxySourceID, "web_search")
	stub := stubTool{name: name}

	adapters := map[string]FormatAdapter{
		"openai-chat":    NewOpenAIChatAdapter(),
		"anthropic-v1":   NewAnthropicV1Adapter(),
		"anthropic-beta": NewAnthropicBetaAdapter(),
	}
	for id, a := range adapters {
		if !a.IsVirtualTool(stub, empty) {
			t.Errorf("%s adapter did not classify %s as virtual", id, name)
		}
	}
}

type stubTool struct{ name string }

func (s stubTool) ID() string        { return "call_1" }
func (s stubTool) Name() string      { return s.name }
func (s stubTool) Arguments() string { return "{}" }
