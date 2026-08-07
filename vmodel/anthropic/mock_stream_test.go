package anthropic

import (
	"context"
	"testing"

	"github.com/tingly-dev/tingly-box/vmodel"
)

// A one-shot tool mock must not stream its static answer during the tool
// round. It used to: the stream path chunked cfg.Content for any text block,
// so the answer went out once with the tool call and again after the tool
// result — the client saw it twice.
func TestMockModel_ToolRoundDoesNotStreamTheStaticAnswer(t *testing.T) {
	m := NewMockModel(&MockModelConfig{
		ID:      "once",
		Content: "the static answer",
		ToolCall: &vmodel.ToolCallConfig{
			Name:      "tingly_box_mcp__webproxy__web_search",
			Arguments: map[string]interface{}{"query": "x"},
			Once:      true,
		},
	})

	var text string
	var toolUses, indices = 0, []int{}
	err := m.HandleAnthropicStream(context.Background(), makeReq("hi"), func(ev any) {
		switch e := ev.(type) {
		case TextDeltaEvent:
			text += e.Text
			indices = append(indices, e.Index)
		case ToolUseEvent:
			toolUses++
			indices = append(indices, e.Index)
		}
	})
	if err != nil {
		t.Fatalf("HandleAnthropicStream: %v", err)
	}
	if toolUses != 1 {
		t.Fatalf("expected one tool use, got %d", toolUses)
	}
	if text != "" {
		t.Fatalf("tool round streamed text %q; the static answer belongs to the round after the tool result", text)
	}
	// Content-block indices must start at 0 and be contiguous, or strict SDK
	// accumulators reject the stream.
	for _, idx := range indices {
		if idx != 0 {
			t.Fatalf("content block index %d; the only emitted block must be index 0, got %v", idx, indices)
		}
	}
}
