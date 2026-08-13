package ops

import (
	"encoding/json"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/tingly-dev/tingly-box/internal/protocol"
)

// ApplyProviderTransforms applies provider-specific transformations to an
// OpenAI Chat request. The dispatch is a flat strings.Contains chain — short,
// explicit, and parallel to the per-shape dispatch in VendorTransform.
//
// New providers are added as new cases here; aliases (e.g. multiple URLs that
// share a vendor's quirks) sit in the same case body.
func ApplyProviderTransforms(req *openai.ChatCompletionNewParams, providerURL, model string, config *protocol.OpenAIConfig) *openai.ChatCompletionNewParams {
	url := strings.ToLower(providerURL)
	modelLower := strings.ToLower(model)

	// prompt_cache_options / prompt_cache_retention / prompt_cache_breakpoint
	// are OpenAI-specific fields, not part of the long-stable Chat Completions
	// schema every OpenAI-compatible vendor cloned. Most vendors don't
	// implement them, and strict-schema gateways reject the whole request over
	// an unknown field — Azure OpenAI ignores them, NVIDIA NIM 400s on the
	// top-level ones ("Unsupported parameter(s): `prompt_cache_options`",
	// #1548). Default to stripping; only vendors confirmed to accept them opt
	// in below.
	if !supportsExplicitPromptCache(url) {
		stripOpenAIPromptCacheFields(req)
	}

	switch {
	case strings.Contains(url, "api.deepseek.com"),
		strings.Contains(url, "api.moonshot.cn"),
		strings.Contains(url, "api.moonshot.ai"),
		strings.Contains(url, "api.kimi.com/coding/v1"),
		strings.Contains(url, "opencode.ai/zen/go") && strings.Contains(modelLower, "deepseek"):
		return applyDeepSeekTransform(req, providerURL, model, config)

	case strings.Contains(url, "generativelanguage.googleapis.com") && strings.Contains(modelLower, "gemini"):
		return applyGeminiTransform(req, providerURL, model, config)

	case strings.Contains(url, "poe.com") && strings.Contains(modelLower, "gemini"):
		return applyGeminiPoeTransform(req, providerURL, model, config)
	}

	// api.openai.com falls through to here too: it has no vendor-specific
	// request shaping beyond applyDefaultTransform's thinking fallback — the
	// request already carries the correct prompt_cache_options /
	// prompt_cache_breakpoint fields from the shared Anthropic→OpenAI
	// conversion, and it's the one vendor supportsExplicitPromptCache
	// confirms accepts them as-is.
	return applyDefaultTransform(req, config)
}

// supportsExplicitPromptCache reports whether providerURL is confirmed to
// accept OpenAI's gpt-5.6+ explicit prompt-cache fields. Extend this
// allowlist only once a vendor has been verified to accept the fields —
// the default (stripped) is the safe outcome for an unverified vendor.
func supportsExplicitPromptCache(url string) bool {
	return strings.Contains(url, "api.openai.com")
}

// stripOpenAIPromptCacheFields removes the OpenAI-specific prompt-cache
// fields from a request — the top-level prompt_cache_options and
// prompt_cache_retention, and the per-content-part prompt_cache_breakpoint
// markers. It's the default for every vendor not on the
// supportsExplicitPromptCache allowlist: the fields are pure caching hints,
// so dropping them changes nothing about message/tool semantics — vendors
// with their own automatic prefix caching (DeepSeek, Moonshot, most
// self-hosted OpenAI-compatible backends) still get cache hits without them.
// All the fields carry omitzero, so zeroing them omits the keys from the
// marshaled request without a JSON round-trip (which would drop per-message
// extra fields such as x_thinking / reasoning_content).
func stripOpenAIPromptCacheFields(req *openai.ChatCompletionNewParams) {
	req.PromptCacheOptions = openai.ChatCompletionNewParamsPromptCacheOptions{}
	req.PromptCacheRetention = ""

	for i := range req.Messages {
		msg := &req.Messages[i]
		switch {
		case msg.OfDeveloper != nil:
			stripTextPartBreakpoints(msg.OfDeveloper.Content.OfArrayOfContentParts)
		case msg.OfSystem != nil:
			stripTextPartBreakpoints(msg.OfSystem.Content.OfArrayOfContentParts)
		case msg.OfUser != nil:
			for j := range msg.OfUser.Content.OfArrayOfContentParts {
				part := &msg.OfUser.Content.OfArrayOfContentParts[j]
				switch {
				case part.OfText != nil:
					part.OfText.PromptCacheBreakpoint = openai.ChatCompletionContentPartTextPromptCacheBreakpointParam{}
				case part.OfImageURL != nil:
					part.OfImageURL.PromptCacheBreakpoint = openai.ChatCompletionContentPartImagePromptCacheBreakpointParam{}
				case part.OfInputAudio != nil:
					part.OfInputAudio.PromptCacheBreakpoint = openai.ChatCompletionContentPartInputAudioPromptCacheBreakpointParam{}
				case part.OfFile != nil:
					part.OfFile.PromptCacheBreakpoint = openai.ChatCompletionContentPartFilePromptCacheBreakpointParam{}
				}
			}
		case msg.OfAssistant != nil:
			for j := range msg.OfAssistant.Content.OfArrayOfContentParts {
				part := &msg.OfAssistant.Content.OfArrayOfContentParts[j]
				if part.OfText != nil {
					part.OfText.PromptCacheBreakpoint = openai.ChatCompletionContentPartTextPromptCacheBreakpointParam{}
				}
			}
		case msg.OfTool != nil:
			stripTextPartBreakpoints(msg.OfTool.Content.OfArrayOfContentParts)
		}
	}
}

func stripTextPartBreakpoints(parts []openai.ChatCompletionContentPartTextParam) {
	for i := range parts {
		parts[i].PromptCacheBreakpoint = openai.ChatCompletionContentPartTextPromptCacheBreakpointParam{}
	}
}

// ApplyCursorCompatContentNormalization flattens rich content in messages for
// Cursor compatibility. Applies to ALL providers when cursor_compat is enabled.
func ApplyCursorCompatContentNormalization(req *openai.ChatCompletionNewParams) {
	for i := range req.Messages {
		msgMap, err := messageToMap(req.Messages[i])
		if err != nil {
			continue
		}
		content, ok := msgMap["content"]
		if !ok {
			continue
		}
		contentParts, ok := content.([]interface{})
		if !ok {
			continue
		}
		flattened, _ := flattenRichContent(contentParts)
		msgMap["content"] = flattened

		updatedBytes, err := json.Marshal(msgMap)
		if err != nil {
			continue
		}
		var updated openai.ChatCompletionMessageParamUnion
		if err := json.Unmarshal(updatedBytes, &updated); err != nil {
			continue
		}
		req.Messages[i] = updated
	}
}

func messageToMap(msg openai.ChatCompletionMessageParamUnion) (map[string]interface{}, error) {
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(msgBytes, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func flattenRichContent(parts []interface{}) (string, bool) {
	var segments []string
	var dropped bool
	for _, part := range parts {
		switch value := part.(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				segments = append(segments, value)
			}
		case map[string]interface{}:
			if textValue, ok := value["text"].(string); ok {
				if strings.TrimSpace(textValue) != "" {
					segments = append(segments, textValue)
				}
			} else if contentValue, ok := value["content"].(string); ok {
				if strings.TrimSpace(contentValue) != "" {
					segments = append(segments, contentValue)
				}
			} else {
				dropped = true
			}
		default:
			dropped = true
		}
	}
	if len(segments) == 0 && dropped {
		return "[non-text content omitted]", true
	}
	if dropped {
		segments = append(segments, "[non-text content omitted]")
	}
	return strings.Join(segments, "\n"), dropped
}

// applyDefaultTransform applies the standard OpenAI-compatible thinking
// fallback when no vendor-specific transform matched. Sets reasoning_effort
// from config, or falls back to a `thinking.type=enabled` extra field for
// providers that accept the Anthropic-style extension.
func applyDefaultTransform(req *openai.ChatCompletionNewParams, config *protocol.OpenAIConfig) *openai.ChatCompletionNewParams {
	if config.HasThinking && config.ReasoningEffort != "" {
		req.ReasoningEffort = config.ReasoningEffort
	} else if config.HasThinking {
		extra := req.ExtraFields()
		if extra == nil {
			extra = map[string]interface{}{
				"thinking": map[string]interface{}{"type": "enabled"},
			}
		} else {
			extra["thinking"] = map[string]interface{}{"type": "enabled"}
		}
		req.SetExtraFields(extra)
	}
	return req
}
