package request

import (
	"encoding/json"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// ConvertOpenAIResponsesToChat converts OpenAI Responses API params to Chat Completions format.
// This is useful when translating between the two API formats.
func ConvertOpenAIResponsesToChat(params *responses.ResponseNewParams, defaultMaxTokens int64) *openai.ChatCompletionNewParams {
	result := &openai.ChatCompletionNewParams{
		Model: openai.ChatModel(params.Model),
	}

	// Convert instructions to system message if present
	if !param.IsOmitted(params.Instructions) && params.Instructions.Value != "" {
		result.Messages = append(result.Messages, openai.SystemMessage(params.Instructions.Value))
	}

	// Convert input to messages. The Responses API accepts either a plain
	// string (shorthand for a single user message — the SDKs' idiomatic form)
	// or a list of input items.
	if !param.IsOmitted(params.Input.OfString) && params.Input.OfString.Value != "" {
		result.Messages = append(result.Messages, openai.UserMessage(params.Input.OfString.Value))
	} else if !param.IsOmitted(params.Input.OfInputItemList) {
		messages := ConvertResponsesInputToMessages(params.Input.OfInputItemList)
		result.Messages = append(result.Messages, messages...)
	}

	// Convert max_output_tokens to max_tokens
	if !param.IsOmitted(params.MaxOutputTokens) {
		result.MaxTokens = openai.Opt(params.MaxOutputTokens.Value)
	} else if defaultMaxTokens > 0 {
		result.MaxTokens = openai.Opt(defaultMaxTokens)
	}

	// Copy temperature
	if !param.IsOmitted(params.Temperature) {
		result.Temperature = openai.Opt(params.Temperature.Value)
	}

	// Copy top_p
	if !param.IsOmitted(params.TopP) {
		result.TopP = openai.Opt(params.TopP.Value)
	}

	// Convert tools if present
	if !param.IsOmitted(params.Tools) && len(params.Tools) > 0 {
		result.Tools = ConvertResponsesToolsToChatTools(params.Tools)
	}

	// Convert tool choice if present
	if !param.IsOmitted(params.ToolChoice) {
		result.ToolChoice = ConvertResponsesToolChoiceToChat(params.ToolChoice)
	}

	result.PromptCacheKey = params.PromptCacheKey
	result.PromptCacheOptions = openai.ChatCompletionNewParamsPromptCacheOptions{
		Mode: params.PromptCacheOptions.Mode,
		Ttl:  params.PromptCacheOptions.Ttl,
	}
	result.PromptCacheRetention = openai.ChatCompletionNewParamsPromptCacheRetention(params.PromptCacheRetention)

	return result
}

// pendingToolCall holds a single tool call during input-to-message conversion.
// Consecutive function_call input items are accumulated and flushed together
// as a single assistant message with all tool_calls, so the resulting message
// sequence satisfies providers (DeepSeek) that require tool messages to
// immediately follow the assistant message that requested them.
type pendingToolCall struct {
	CallID    string
	Name      string
	Arguments string
}

// ConvertResponsesInputToMessages converts Responses API input items to Chat Completion messages.
func ConvertResponsesInputToMessages(items responses.ResponseInputParam) []openai.ChatCompletionMessageParamUnion {
	var messages []openai.ChatCompletionMessageParamUnion

	flushCalls := func(calls []pendingToolCall) {
		if len(calls) == 0 {
			return
		}
		toolCalls := make([]map[string]interface{}, 0, len(calls))
		for _, tc := range calls {
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   tc.CallID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      tc.Name,
					"arguments": tc.Arguments,
				},
			})
		}
		msgMap := map[string]interface{}{
			"role":       "assistant",
			"content":    "",
			"tool_calls": toolCalls,
		}
		msgBytes, _ := json.Marshal(msgMap)
		var result openai.ChatCompletionMessageParamUnion
		_ = json.Unmarshal(msgBytes, &result)
		messages = append(messages, result)
	}

	var pendingCalls []pendingToolCall

	for _, item := range items {
		// Handle message items — do NOT flush pending function_calls.
		// function_call_output flushes them so that assistant(tool_calls)
		// appears immediately before the corresponding tool messages.
		// Flushing here would cause messages to be inserted between
		// assistant(tool_calls) and its tool responses.
		if !param.IsOmitted(item.OfMessage) {
			msg := item.OfMessage
			role := string(msg.Role)

			// Extract content based on type
			if !param.IsOmitted(msg.Content.OfString) {
				// Simple string content
				content := msg.Content.OfString.Value
				messages = append(messages, createMessage(role, content))
			} else if !param.IsOmitted(msg.Content.OfInputItemContentList) {
				if converted, ok := createMessageFromResponsesContent(role, msg.Content.OfInputItemContentList); ok {
					messages = append(messages, converted)
				}
			}
			continue
		}

		// Accumulate consecutive function_call items into a single assistant message.
		// Flushed on the next message boundary or first function_call_output.
		if !param.IsOmitted(item.OfFunctionCall) {
			fnCall := item.OfFunctionCall
			pendingCalls = append(pendingCalls, pendingToolCall{
				CallID:    fnCall.CallID,
				Name:      fnCall.Name,
				Arguments: fnCall.Arguments,
			})
			continue
		}

		// Handle function call output items (tool results)
		// Flush pending function calls as a single assistant message first
		if !param.IsOmitted(item.OfFunctionCallOutput) {
			flushCalls(pendingCalls)
			pendingCalls = nil

			output := item.OfFunctionCallOutput

			// Extract output content
			if !param.IsOmitted(output.Output.OfString) {
				messages = append(messages, openai.ToolMessage(output.Output.OfString.Value, output.CallID))
				continue
			}
			parts := make([]openai.ChatCompletionContentPartTextParam, 0,
				len(output.Output.OfResponseFunctionCallOutputItemArray))
			for _, item := range output.Output.OfResponseFunctionCallOutputItemArray {
				if item.OfInputText == nil || item.OfInputText.Text == "" {
					continue
				}
				part := openai.ChatCompletionContentPartTextParam{Text: item.OfInputText.Text}
				if !param.IsOmitted(item.OfInputText.PromptCacheBreakpoint) {
					part.PromptCacheBreakpoint = openai.NewChatCompletionContentPartTextPromptCacheBreakpointParam()
				}
				parts = append(parts, part)
			}
			if len(parts) > 0 {
				messages = append(messages, openai.ChatCompletionMessageParamUnion{
					OfTool: &openai.ChatCompletionToolMessageParam{
						ToolCallID: output.CallID,
						Content: openai.ChatCompletionToolMessageParamContentUnion{
							OfArrayOfContentParts: parts,
						},
					},
				})
			} else {
				messages = append(messages, openai.ToolMessage("", output.CallID))
			}
		}
	}

	// Flush remaining pending calls at end of input
	flushCalls(pendingCalls)

	return messages
}

func createMessageFromResponsesContent(role string, content responses.ResponseInputMessageContentListParam) (openai.ChatCompletionMessageParamUnion, bool) {
	var text string
	var hasImage, hasCacheBreakpoint bool
	for _, item := range content {
		switch {
		case item.OfInputText != nil:
			text += item.OfInputText.Text
			hasCacheBreakpoint = hasCacheBreakpoint || !param.IsOmitted(item.OfInputText.PromptCacheBreakpoint)
		case item.OfInputImage != nil:
			hasImage = true
			hasCacheBreakpoint = hasCacheBreakpoint || !param.IsOmitted(item.OfInputImage.PromptCacheBreakpoint)
		}
	}

	if !hasImage && !hasCacheBreakpoint {
		if text == "" {
			return openai.ChatCompletionMessageParamUnion{}, false
		}
		return createMessage(role, text), true
	}

	switch strings.ToLower(role) {
	case "user":
		parts := make([]openai.ChatCompletionContentPartUnionParam, 0, len(content))
		for _, item := range content {
			switch {
			case item.OfInputText != nil:
				part := openai.ChatCompletionContentPartTextParam{Text: item.OfInputText.Text}
				if !param.IsOmitted(item.OfInputText.PromptCacheBreakpoint) {
					part.PromptCacheBreakpoint = openai.NewChatCompletionContentPartTextPromptCacheBreakpointParam()
				}
				parts = append(parts, openai.ChatCompletionContentPartUnionParam{OfText: &part})
			case item.OfInputImage != nil && item.OfInputImage.ImageURL.Valid():
				part := openai.ChatCompletionContentPartImageParam{
					ImageURL: openai.ChatCompletionContentPartImageImageURLParam{URL: item.OfInputImage.ImageURL.Value},
				}
				if !param.IsOmitted(item.OfInputImage.PromptCacheBreakpoint) {
					part.PromptCacheBreakpoint = openai.NewChatCompletionContentPartImagePromptCacheBreakpointParam()
				}
				parts = append(parts, openai.ChatCompletionContentPartUnionParam{OfImageURL: &part})
			}
		}
		if len(parts) == 0 {
			return openai.ChatCompletionMessageParamUnion{}, false
		}
		return openai.UserMessage(parts), true

	case "system":
		parts := responseTextContentToChatTextParts(content)
		if len(parts) == 0 {
			return openai.ChatCompletionMessageParamUnion{}, false
		}
		return openai.SystemMessage(parts), true

	case "developer":
		parts := responseTextContentToChatTextParts(content)
		if len(parts) == 0 {
			return openai.ChatCompletionMessageParamUnion{}, false
		}
		return openai.DeveloperMessage(parts), true

	case "assistant":
		textParts := responseTextContentToChatTextParts(content)
		if len(textParts) == 0 {
			return openai.ChatCompletionMessageParamUnion{}, false
		}
		parts := make([]openai.ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion, 0, len(textParts))
		for i := range textParts {
			part := textParts[i]
			parts = append(parts, openai.ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion{OfText: &part})
		}
		return openai.ChatCompletionMessageParamUnion{
			OfAssistant: &openai.ChatCompletionAssistantMessageParam{
				Content: openai.ChatCompletionAssistantMessageParamContentUnion{
					OfArrayOfContentParts: parts,
				},
			},
		}, true
	}
	return openai.ChatCompletionMessageParamUnion{}, false
}

func responseTextContentToChatTextParts(content responses.ResponseInputMessageContentListParam) []openai.ChatCompletionContentPartTextParam {
	parts := make([]openai.ChatCompletionContentPartTextParam, 0, len(content))
	for _, item := range content {
		if item.OfInputText == nil || item.OfInputText.Text == "" {
			continue
		}
		part := openai.ChatCompletionContentPartTextParam{Text: item.OfInputText.Text}
		if !param.IsOmitted(item.OfInputText.PromptCacheBreakpoint) {
			part.PromptCacheBreakpoint = openai.NewChatCompletionContentPartTextPromptCacheBreakpointParam()
		}
		parts = append(parts, part)
	}
	return parts
}

// createMessage creates a ChatCompletionMessageParamUnion based on role and content.
func createMessage(role, content string) openai.ChatCompletionMessageParamUnion {
	switch strings.ToLower(role) {
	case "system":
		return openai.SystemMessage(content)
	case "user":
		return openai.UserMessage(content)
	case "assistant":
		return openai.AssistantMessage(content)
	default:
		// Default to user message for unknown roles
		return openai.UserMessage(content)
	}
}

// ConvertResponsesToolsToChatTools converts Responses API tools to Chat Completions tools.
func ConvertResponsesToolsToChatTools(tools []responses.ToolUnionParam) []openai.ChatCompletionToolUnionParam {
	if len(tools) == 0 {
		return nil
	}

	result := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))

	for _, tool := range tools {
		// Handle function tools
		if !param.IsOmitted(tool.OfFunction) {
			fn := tool.OfFunction

			// Convert parameters map to proper format
			var parameters map[string]interface{}
			if fn.Parameters != nil {
				parameters = fn.Parameters
			} else {
				// Create empty parameters object
				parameters = map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				}
			}

			functionDef := shared.FunctionDefinitionParam{
				Name:        fn.Name,
				Parameters:  parameters,
				Description: param.Opt[string]{},
			}

			// Set description if present
			if !param.IsOmitted(fn.Description) {
				functionDef.Description = fn.Description
			}

			// Set strict mode if present
			if !param.IsOmitted(fn.Strict) {
				// Note: strict mode is set via ExtraFields if needed
			}

			result = append(result, openai.ChatCompletionFunctionTool(functionDef))
		}
	}

	return result
}

// ConvertResponsesToolChoiceToChat converts Responses API tool choice to Chat Completions format.
func ConvertResponsesToolChoiceToChat(choice responses.ResponseNewParamsToolChoiceUnion) openai.ChatCompletionToolChoiceOptionUnionParam {
	// Handle "auto", "none", "required" modes
	if !param.IsOmitted(choice.OfToolChoiceMode) {
		mode := string(choice.OfToolChoiceMode.Value)
		switch mode {
		case "auto", "none", "required":
			return openai.ChatCompletionToolChoiceOptionUnionParam{
				OfAuto: openai.Opt(mode),
			}
		}
	}

	// Handle specific function tool choice
	if !param.IsOmitted(choice.OfFunctionTool) {
		fn := choice.OfFunctionTool
		functionChoice := openai.ChatCompletionNamedToolChoiceFunctionParam{
			Name: fn.Name,
		}
		return openai.ToolChoiceOptionFunctionToolChoice(functionChoice)
	}

	// Default to auto
	return openai.ChatCompletionToolChoiceOptionUnionParam{
		OfAuto: openai.Opt("auto"),
	}
}
