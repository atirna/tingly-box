package anthropic

import (
	"context"
	"encoding/json"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/protocol/token"
	"github.com/tingly-dev/tingly-box/vmodel"
)

// MockModelConfig holds configuration for an Anthropic mock virtual model
// (static or tool).
type MockModelConfig struct {
	ID           string
	Name         string
	Description  string
	Content      string        // static: fixed response text
	StopReason   string        // default: "stop" (or "tool_use" if ToolCall is set)
	Delay        time.Duration // simulated latency
	StreamChunks []string      // optional custom stream chunks

	// tool-type: if set, the response includes a tool_use block.
	ToolCall *vmodel.ToolCallConfig

	// Usage, when set, is emitted as a UsageEvent immediately before
	// DoneEvent (rendered by virtualserver inside message_delta.usage).
	Usage *vmodel.MockUsage

	// Error, when non-nil, makes this mock simulate a failure. See
	// vmodel.ErrorInjection for the two supported stages.
	Error *vmodel.ErrorInjection
}

// MockModel is an Anthropic-only mock virtual model. It returns a fixed
// content block (or tool_use block) and supports streaming via per-chunk
// token splitting.
type MockModel struct {
	vmodel.BaseMockModel
	cfg *MockModelConfig
}

// Compile-time interface check.
var _ VirtualModel = (*MockModel)(nil)

// NewMockModel creates a MockModel. StopReason defaults to "stop"
// (or "tool_use" if ToolCall is set).
func NewMockModel(cfg *MockModelConfig) *MockModel {
	if cfg.StopReason == "" {
		if cfg.ToolCall != nil {
			cfg.StopReason = "tool_use"
		} else {
			cfg.StopReason = "stop"
		}
	}
	description := cfg.Description
	if description == "" {
		description = vmodel.DefaultMockDescription
	}
	typ := vmodel.VirtualModelTypeStatic
	if cfg.ToolCall != nil {
		typ = vmodel.VirtualModelTypeTool
	}
	return &MockModel{
		BaseMockModel: vmodel.BaseMockModel{
			ID:          cfg.ID,
			Name:        cfg.Name,
			Description: description,
			Type:        typ,
			Delay:       cfg.Delay,
		},
		cfg: cfg,
	}
}

// ErrorInjection implements vmodel.ErrorInjectingModel.
func (m *MockModel) ErrorInjection() *vmodel.ErrorInjection { return m.cfg.Error }

// chunksFor splits the text of one content block for streaming.
//
// It takes the block's own text rather than cfg.Content because a tool
// response's text block is the tool's display line, not the static answer —
// streaming cfg.Content there would emit the answer during the tool round and
// again after it. Configured StreamChunks still win, but only for the static
// block they were written for.
func (m *MockModel) chunksFor(text string) []string {
	if len(m.cfg.StreamChunks) > 0 && text == m.cfg.Content {
		return m.cfg.StreamChunks
	}
	if text == "" {
		return nil
	}
	return token.SplitIntoChunks(text)
}

// HandleAnthropic returns fixed content from config in Anthropic format.
//
// A one-shot tool mock (ToolCallConfig.Once) answers with its static content
// once the conversation carries a tool result, so a server-side tool loop
// terminates instead of re-requesting the same call every round.
func (m *MockModel) HandleAnthropic(req *protocol.AnthropicBetaMessagesRequest) (VModelResponse, error) {
	if m.cfg.ToolCall != nil && !(m.cfg.ToolCall.Once && anthropicHasToolResult(req)) {
		return m.toolResponse(), nil
	}
	return m.staticResponse(), nil
}

// anthropicHasToolResult reports whether the conversation already carries a
// tool result — i.e. a previous round's tool call has been answered.
func anthropicHasToolResult(req *protocol.AnthropicBetaMessagesRequest) bool {
	if req == nil || req.BetaMessageNewParams == nil {
		return false
	}
	for _, msg := range req.Messages {
		for _, block := range msg.Content {
			if block.OfToolResult != nil {
				return true
			}
		}
	}
	return false
}

func (m *MockModel) staticResponse() VModelResponse {
	// An empty stop_reason is not valid on the wire — the Anthropic protocol
	// expects a terminal reason on message_delta, and downstream consumers
	// that find none fall back to whatever they last saw (a preceding tool
	// round's "tool_use", for instance). Default a plain text answer to
	// end_turn; configs that mean something else still set it explicitly.
	stopReason := m.cfg.StopReason
	if stopReason == "" {
		stopReason = string(sdk.BetaStopReasonEndTurn)
	}
	return VModelResponse{
		Content: []sdk.BetaContentBlockParamUnion{
			{OfText: &sdk.BetaTextBlockParam{Text: m.cfg.Content}},
		},
		StopReason: sdk.BetaStopReason(stopReason),
	}
}

func (m *MockModel) toolResponse() VModelResponse {
	tc := m.cfg.ToolCall
	displayText := vmodel.ToolCallDisplayContent(tc.Arguments)
	inputJSON, _ := json.Marshal(tc.Arguments)

	return VModelResponse{
		Content: []sdk.BetaContentBlockParamUnion{
			{OfText: &sdk.BetaTextBlockParam{Text: displayText}},
			{OfToolUse: &sdk.BetaToolUseBlockParam{
				ID:    "toolu_virtual",
				Name:  tc.Name,
				Input: json.RawMessage(inputJSON),
			}},
		},
		StopReason: "tool_use",
	}
}

// HandleAnthropicStream streams fixed content using configured chunks with simulated delay.
func (m *MockModel) HandleAnthropicStream(ctx context.Context, req *protocol.AnthropicBetaMessagesRequest, emit func(any)) error {
	resp, err := m.HandleAnthropic(req)
	if err != nil {
		return err
	}
	emit(StreamStartEvent{MsgID: "msg_virtual", Model: m.cfg.ID})
	// Content-block indices must be contiguous from 0 — the SDK accumulator
	// rejects a gap — so they count emitted blocks, not source blocks. A tool
	// response whose display text is empty contributes no text block at all.
	index := 0
	for _, blk := range resp.Content {
		if blk.OfText != nil {
			chunks := m.chunksFor(blk.OfText.Text)
			if len(chunks) == 0 {
				continue
			}
			perChunk := vmodel.ResolveChunkDelay(m.cfg.Delay, len(chunks))
			i := index
			if err := vmodel.EmitChunks(ctx, chunks, perChunk, func(_ int, chunk string) bool {
				emit(TextDeltaEvent{Index: i, Text: chunk})
				return true
			}); err != nil {
				return err
			}
			index++
		} else if blk.OfToolUse != nil {
			inputJSON, _ := json.Marshal(blk.OfToolUse.Input)
			emit(ToolUseEvent{
				Index: index,
				ID:    blk.OfToolUse.ID,
				Name:  blk.OfToolUse.Name,
				Input: inputJSON,
			})
			index++
		}
	}
	if m.cfg.Usage != nil {
		emit(UsageEvent{Usage: *m.cfg.Usage})
	}
	emit(DoneEvent{StopReason: string(resp.StopReason)})
	return nil
}
