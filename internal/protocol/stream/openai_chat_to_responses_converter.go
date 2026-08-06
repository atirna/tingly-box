package stream

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"

	"github.com/tingly-dev/tingly-box/internal/protocol"
	protocolusage "github.com/tingly-dev/tingly-box/internal/protocol/usage"
	"github.com/tingly-dev/tingly-box/internal/protocol/wire"
)

// chatToResponsesConverter converts an OpenAI Chat Completions stream into
// a sequence of Responses API events. It implements StreamConverter.
type chatToResponsesConverter struct {
	stream        OpenAIChatStream
	responseModel string

	// internal state
	responseID        string
	createdAt         int64
	sequenceNumber    int64
	outputIndex       int
	textItemID        string
	textOutputIndex   int
	hasTextItem       bool
	pendingToolCalls  map[int]*pendingToolCallResponse
	accumulatedText   strings.Builder
	promptTokensTotal int64
	usage             *protocol.TokenUsage
	hasSentCreated    bool
	hasUsage          bool
	completedSent     bool
	finishReason      string

	// pending is an internal queue of events to yield one-by-one
	pending []wire.ResponsesEvent
}

// pendingToolCallResponse tracks a tool call being assembled from stream chunks
type pendingToolCallResponse struct {
	itemID    string
	callID    string
	outputIdx int
	name      string
	arguments strings.Builder
}

// NewChatToResponsesConverter creates a converter that reads from an OpenAI
// Chat Completions stream and yields Responses API wire events.
func NewChatToResponsesConverter(stream OpenAIChatStream, responseModel string) *chatToResponsesConverter {
	return &chatToResponsesConverter{
		stream:           stream,
		responseModel:    responseModel,
		responseID:       fmt.Sprintf("resp_%d", time.Now().Unix()),
		createdAt:        time.Now().Unix(),
		textItemID:       fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		textOutputIndex:  -1,
		usage:            protocol.ZeroTokenUsage(),
		pendingToolCalls: make(map[int]*pendingToolCallResponse),
	}
}

func (c *chatToResponsesConverter) Next() (interface{}, bool, error) {
	// Drain buffered events first
	if len(c.pending) > 0 {
		evt := c.pending[0]
		c.pending = c.pending[1:]
		return evt, false, nil
	}

	// Read upstream chunks until we have at least one event to yield
	for {
		if !c.stream.Next() {
			if err := c.stream.Err(); err != nil {
				return nil, false, err
			}
			if !c.completedSent {
				return nil, false, fmt.Errorf("chat stream ended without a finish reason")
			}
			return nil, true, nil
		}

		chunk := c.stream.Current()
		c.processChunk(&chunk)

		if len(c.pending) > 0 {
			evt := c.pending[0]
			c.pending = c.pending[1:]
			return evt, false, nil
		}
	}
}

func (c *chatToResponsesConverter) Usage() *protocol.TokenUsage {
	return c.usage
}

// processChunk handles a single upstream ChatCompletionChunk and appends
// zero or more Responses API events to c.pending.
func (c *chatToResponsesConverter) processChunk(chunk *openai.ChatCompletionChunk) {
	// Emit response.created / response.in_progress on first chunk
	c.emitCreated()

	// Track usage
	if chunkHasUsage(chunk.Usage) {
		c.usage = protocolusage.FromOpenAIChatCompletion(chunk.Usage)
		c.promptTokensTotal = int64(c.usage.InputTokens + c.usage.CacheReadTokens)
		c.hasUsage = true
	}

	if len(chunk.Choices) == 0 {
		return
	}

	choice := chunk.Choices[0]

	// Handle content delta
	if choice.Delta.Content != "" {
		if !c.hasTextItem {
			c.textOutputIndex = c.outputIndex
			c.outputIndex++
			c.emitTextItemAdded()
			c.hasTextItem = true
		}
		c.accumulatedText.WriteString(choice.Delta.Content)
		c.pending = append(c.pending, wire.ResponsesOutputTextDeltaEvent{
			Type:           "response.output_text.delta",
			SequenceNumber: c.nextSeq(),
			ItemID:         c.textItemID,
			OutputIndex:    c.textOutputIndex,
			ContentIndex:   0,
			Delta:          choice.Delta.Content,
			Logprobs:       []interface{}{},
		})
	}

	// Handle tool_calls delta
	for _, toolCall := range choice.Delta.ToolCalls {
		openaiIndex := int(toolCall.Index)

		if _, exists := c.pendingToolCalls[openaiIndex]; !exists {
			itemID := fmt.Sprintf("fc_%d_%d", time.Now().Unix(), openaiIndex)
			if toolCall.ID != "" {
				itemID = truncateToolCallID(toolCall.ID)
			}

			toolOutputIndex := c.outputIndex
			c.outputIndex++

			c.pendingToolCalls[openaiIndex] = &pendingToolCallResponse{
				itemID:    itemID,
				callID:    toolCall.ID,
				outputIdx: toolOutputIndex,
				name:      toolCall.Function.Name,
			}

			callID := toolCall.ID
			if callID == "" {
				callID = itemID
			}
			c.pending = append(c.pending, wire.ResponsesOutputItemAddedEvent{
				Type:           "response.output_item.added",
				SequenceNumber: c.nextSeq(),
				OutputIndex:    toolOutputIndex,
				Item:           newResponsesFunctionCallItem(itemID, callID, toolCall.Function.Name, "", "in_progress"),
			})
		}

		if toolCall.Function.Arguments != "" {
			ptc := c.pendingToolCalls[openaiIndex]
			ptc.arguments.WriteString(toolCall.Function.Arguments)
			c.pending = append(c.pending, wire.ResponsesFunctionCallArgumentsDeltaEvent{
				Type:           "response.function_call_arguments.delta",
				SequenceNumber: c.nextSeq(),
				ItemID:         ptc.itemID,
				OutputIndex:    ptc.outputIdx,
				Delta:          toolCall.Function.Arguments,
			})
		}
	}

	// Check for completion
	if choice.FinishReason != "" {
		c.finishReason = string(choice.FinishReason)

		if !c.hasUsage && c.usage.OutputTokens == 0 {
			outputTokens := int64(c.accumulatedText.Len() / 4)
			for _, ptc := range c.pendingToolCalls {
				outputTokens += int64(ptc.arguments.Len() / 4)
			}
			c.usage = protocol.NewTokenUsageFull(c.usage.InputTokens, int(outputTokens),
				c.usage.CacheReadTokens, c.usage.CacheWriteTokens, c.usage.ReasoningTokens)
		}

		c.emitCompletionEvents()
	}
}

// emitCompletionEvents appends the terminal sequence of events (text done,
// tool call done, response.completed) to c.pending.
func (c *chatToResponsesConverter) emitCompletionEvents() {
	if c.completedSent {
		return
	}
	c.completedSent = true

	c.emitCreated()

	if c.finishReason == "" {
		c.finishReason = "stop"
	}

	if c.hasTextItem {
		text := c.accumulatedText.String()
		c.pending = append(c.pending, wire.ResponsesOutputTextDoneEvent{
			Type:           "response.output_text.done",
			SequenceNumber: c.nextSeq(),
			ItemID:         c.textItemID,
			OutputIndex:    c.textOutputIndex,
			ContentIndex:   0,
			Text:           text,
			Logprobs:       []interface{}{},
		})
		c.pending = append(c.pending, wire.ResponsesContentPartDoneEvent{
			Type:           "response.content_part.done",
			SequenceNumber: c.nextSeq(),
			OutputIndex:    c.textOutputIndex,
			ItemID:         c.textItemID,
			ContentIndex:   0,
			Part: wire.ResponsesContentPartWire{
				Type:        "output_text",
				Text:        text,
				Annotations: []interface{}{},
			},
		})
		c.pending = append(c.pending, wire.ResponsesOutputItemDoneEvent{
			Type:           "response.output_item.done",
			SequenceNumber: c.nextSeq(),
			OutputIndex:    c.textOutputIndex,
			Item:           newResponsesMessageItem(c.textItemID, "completed", text),
		})
	}

	sortedIndexes := make([]int, 0, len(c.pendingToolCalls))
	for idx := range c.pendingToolCalls {
		sortedIndexes = append(sortedIndexes, idx)
	}
	sort.Ints(sortedIndexes)

	for _, idx := range sortedIndexes {
		ptc := c.pendingToolCalls[idx]
		callID := ptc.callID
		if callID == "" {
			callID = ptc.itemID
		}
		arguments := ptc.arguments.String()
		c.pending = append(c.pending, wire.ResponsesFunctionCallArgumentsDoneEvent{
			Type:           "response.function_call_arguments.done",
			SequenceNumber: c.nextSeq(),
			ItemID:         ptc.itemID,
			OutputIndex:    ptc.outputIdx,
			Name:           ptc.name,
			Arguments:      arguments,
		})
		c.pending = append(c.pending, wire.ResponsesOutputItemDoneEvent{
			Type:           "response.output_item.done",
			SequenceNumber: c.nextSeq(),
			OutputIndex:    ptc.outputIdx,
			Item:           newResponsesFunctionCallItem(ptc.itemID, callID, ptc.name, arguments, "completed"),
		})
	}

	isIncomplete, incompleteReason := chatFinishReasonToIncomplete(c.finishReason)
	itemStatus := "completed"
	if isIncomplete {
		itemStatus = "incomplete"
	}

	output := make([]wire.ResponsesOutputItemWire, c.outputIndex)
	if c.hasTextItem {
		output[c.textOutputIndex] = newResponsesMessageItem(c.textItemID, itemStatus, c.accumulatedText.String())
	}
	for _, idx := range sortedIndexes {
		ptc := c.pendingToolCalls[idx]
		callID := ptc.callID
		if callID == "" {
			callID = ptc.itemID
		}
		output[ptc.outputIdx] = newResponsesFunctionCallItem(ptc.itemID, callID, ptc.name, ptc.arguments.String(), itemStatus)
	}

	if isIncomplete {
		resp := c.wireResponse("incomplete", output)
		resp.IncompleteDetails = &wire.ResponsesIncompleteDetailsWire{Reason: incompleteReason}
		c.pending = append(c.pending, wire.ResponsesIncompleteEvent{
			Type:           "response.incomplete",
			SequenceNumber: c.nextSeq(),
			Response:       resp,
		})
	} else {
		c.pending = append(c.pending, wire.ResponsesCompletedEvent{
			Type:           "response.completed",
			SequenceNumber: c.nextSeq(),
			Response:       c.wireResponse("completed", output),
		})
	}
}

// chatFinishReasonToIncomplete maps an OpenAI Chat finish_reason to the
// Responses API incomplete status. Returns (true, reason) when the response
// should be marked incomplete, or (false, "") for normal completion.
func chatFinishReasonToIncomplete(finishReason string) (bool, string) {
	switch finishReason {
	case "length":
		return true, "max_output_tokens"
	case "content_filter":
		return true, "content_filter"
	default:
		return false, ""
	}
}

// emitCreated appends response.created + response.in_progress once. The real
// Responses API opens every stream with both events, and strict clients (and
// the sibling Anthropic-to-Responses converter) expect the pair.
func (c *chatToResponsesConverter) emitCreated() {
	if c.hasSentCreated {
		return
	}
	c.hasSentCreated = true
	c.pending = append(c.pending, wire.ResponsesCreatedEvent{
		Type:           "response.created",
		SequenceNumber: c.nextSeq(),
		Response:       c.wireResponse("in_progress", nil),
	})
	c.pending = append(c.pending, wire.ResponsesInProgressEvent{
		Type:           "response.in_progress",
		SequenceNumber: c.nextSeq(),
		Response:       c.wireResponse("in_progress", nil),
	})
}

func (c *chatToResponsesConverter) emitTextItemAdded() {
	c.pending = append(c.pending, wire.ResponsesOutputItemAddedEvent{
		Type:           "response.output_item.added",
		SequenceNumber: c.nextSeq(),
		OutputIndex:    c.textOutputIndex,
		Item:           newResponsesMessageItem(c.textItemID, "in_progress", ""),
	})
	c.pending = append(c.pending, wire.ResponsesContentPartAddedEvent{
		Type:           "response.content_part.added",
		SequenceNumber: c.nextSeq(),
		ItemID:         c.textItemID,
		OutputIndex:    c.textOutputIndex,
		ContentIndex:   0,
		Part: wire.ResponsesContentPartWire{
			Type:        "output_text",
			Annotations: []interface{}{},
		},
	})
}

func (c *chatToResponsesConverter) nextSeq() int64 {
	seq := c.sequenceNumber
	c.sequenceNumber++
	return seq
}

func (c *chatToResponsesConverter) wireResponse(status string, output []wire.ResponsesOutputItemWire) wire.ResponsesWireResponse {
	if output == nil {
		output = []wire.ResponsesOutputItemWire{}
	}
	return wire.ResponsesWireResponse{
		ID:        c.responseID,
		Object:    "response",
		CreatedAt: c.createdAt,
		Status:    status,
		Output:    output,
		Usage:     protocolusage.ToResponsesUsageWire(c.Usage()),
		Model:     c.responseModel,
	}
}

func newResponsesMessageItem(itemID, status, text string) wire.ResponsesOutputItemWire {
	return wire.ResponsesOutputItemWire{
		ID:     itemID,
		Type:   "message",
		Role:   "assistant",
		Status: status,
		Content: []wire.ResponsesContentPartWire{
			{
				Type:        "output_text",
				Text:        text,
				Annotations: []interface{}{},
			},
		},
	}
}

func newResponsesFunctionCallItem(itemID, callID, name, arguments, status string) wire.ResponsesOutputItemWire {
	return wire.ResponsesOutputItemWire{
		ID:        itemID,
		CallID:    callID,
		Type:      "function_call",
		Name:      name,
		Arguments: &arguments,
		Status:    status,
	}
}
