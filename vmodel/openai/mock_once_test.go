package openai

import (
	"testing"

	sdk "github.com/openai/openai-go/v3"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/vmodel"
)

func onceModel() *MockModel {
	return NewMockModel(&MockModelConfig{
		ID:      "once",
		Content: "final answer",
		ToolCall: &vmodel.ToolCallConfig{
			Name:      "tingly_box_mcp__webproxy__web_search",
			Arguments: map[string]interface{}{"query": "x"},
			Once:      true,
		},
	})
}

func chatReq(msgs ...sdk.ChatCompletionMessageParamUnion) *protocol.OpenAIChatCompletionRequest {
	return &protocol.OpenAIChatCompletionRequest{
		ChatCompletionNewParams: &sdk.ChatCompletionNewParams{Messages: msgs},
	}
}

// A one-shot tool mock asks for the tool while nothing has answered it, then
// switches to its static content. Without that second half a server-side tool
// loop never terminates — it re-requests the same call until the round budget
// runs out and the client gets an empty response.
func TestMockModel_ToolCallOnce(t *testing.T) {
	m := onceModel()

	first, err := m.HandleOpenAIChat(chatReq(sdk.UserMessage("hi")))
	if err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if len(first.ToolCalls) != 1 {
		t.Fatalf("round 1 must request the tool, got %+v", first)
	}

	second, err := m.HandleOpenAIChat(chatReq(
		sdk.UserMessage("hi"),
		sdk.ToolMessage("search results", "toolu_virtual"),
	))
	if err != nil {
		t.Fatalf("round 2: %v", err)
	}
	if len(second.ToolCalls) != 0 {
		t.Fatalf("round 2 must not re-request the tool, got %+v", second.ToolCalls)
	}
	if second.Content != "final answer" {
		t.Fatalf("round 2 content = %q, want the static answer", second.Content)
	}
}

// Without Once the mock stays stateless — the pre-existing behavior every
// other tool fixture relies on.
func TestMockModel_ToolCallWithoutOnceIsStateless(t *testing.T) {
	m := NewMockModel(&MockModelConfig{
		ID:       "stateless",
		Content:  "final answer",
		ToolCall: &vmodel.ToolCallConfig{Name: "ask_user_question"},
	})

	resp, err := m.HandleOpenAIChat(chatReq(
		sdk.UserMessage("hi"),
		sdk.ToolMessage("result", "toolu_virtual"),
	))
	if err != nil {
		t.Fatalf("HandleOpenAIChat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("a stateless tool mock must keep requesting the tool, got %+v", resp)
	}
}
