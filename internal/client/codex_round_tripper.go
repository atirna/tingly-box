package client

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const reasoningMarker = "reasoning.encrypted_content"
const defaultInstructions = "You are a helpful AI assistant."

// codexRoundTripper wraps an http.RoundTripper to transform ChatGPT backend API
// responses to OpenAI Responses API format. The ChatGPT backend API returns a custom format
// that differs from the standard OpenAI Responses API spec.
//
// This RoundTripper transforms the response format to match what the OpenAI SDK expects.
type codexRoundTripper struct {
	http.RoundTripper
}

func (t *codexRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {

	// codexHook applies ChatGPT/Codex OAuth specific request modifications:
	// - Rewrites URL paths from /v1/... to /codex/... for ChatGPT backend API
	// - Handles special cases for responses endpoint
	// - Adds required ChatGPT backend API headers
	// - Transforms X-ChatGPT-Account-ID to ChatGPT-Account-ID header
	originalPath := req.URL.Path
	newPath := rewriteCodexPath(originalPath)

	if newPath != originalPath {
		logrus.WithContext(req.Context()).Debugf("[Codex] Rewriting URL path: %s -> %s", originalPath, newPath)
		req.URL.Path = newPath
	}

	req.Header.Set("OpenAI-Beta", "responses=experimental")
	//req.Header.Set("originator", "tingly-box")

	if accountID := req.Header.Get("X-ChatGPT-Account-ID"); accountID != "" {
		req.Header.Set("ChatGPT-Account-ID", accountID)
		req.Header.Del("X-ChatGPT-Account-ID")
	}

	// Filter out unsupported parameters for ChatGPT backend API
	// ChatGPT backend API does NOT support: max_tokens, max_completion_tokens, temperature, top_p, max_output_tokens

	var filtered []byte
	if req.Body != nil && req.Method == "POST" {
		body, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}

		filtered, err = t.filterField(body)
		if err != nil {
			return nil, fmt.Errorf("failed to filter field: %w", err)
		}

		// Trim capacity to length to avoid excessive memory usage
		filtered = append([]byte(nil), filtered...)
		// Set GetBody to allow retries and redirects
		req.Body = io.NopCloser(bytes.NewReader(filtered))
		req.ContentLength = int64(len(filtered))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(filtered)), nil
		}
	}

	resp, err := t.RoundTripper.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		errorBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("request failed with status %s: %s", resp.Status, string(errorBody))
	}

	if err := validateCodexStreamResponse(resp); err != nil {
		return nil, err
	}

	resp.Header.Set("Content-Type", "text/event-stream")
	logrus.WithContext(req.Context()).Debugf("[Codex] Must use stream: %s", resp.Status)

	return resp, nil
}

func validateCodexStreamResponse(resp *http.Response) error {
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return nil
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.ToLower(strings.Split(contentType, ";")[0]))
	}
	if mediaType == "" || mediaType == "text/event-stream" {
		return nil
	}
	if !isClearlyNonSSEMediaType(mediaType) {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	return fmt.Errorf("codex returned non-SSE 200 response content-type %q: %s", contentType, strings.TrimSpace(string(body)))
}

func isClearlyNonSSEMediaType(mediaType string) bool {
	if mediaType == "application/json" || mediaType == "application/problem+json" || mediaType == "text/html" {
		return true
	}
	return strings.HasSuffix(mediaType, "+json")
}

func (t *codexRoundTripper) filterField(body []byte) ([]byte, error) {
	// Filter the request body to remove unsupported parameters using sjson
	// This is more efficient than unmarshaling to map and marshaling back

	bodyStr := string(body)

	// codex require false here
	bodyStr, _ = sjson.SetRaw(bodyStr, "store", "false")
	bodyStr, _ = sjson.SetRaw(bodyStr, "stream", "true")

	// Remove unsupported parameters (ignore errors if key doesn't exist)
	bodyStr, _ = sjson.Delete(bodyStr, "max_tokens")
	bodyStr, _ = sjson.Delete(bodyStr, "max_completion_tokens")
	bodyStr, _ = sjson.Delete(bodyStr, "max_output_tokens")
	bodyStr, _ = sjson.Delete(bodyStr, "temperature")
	bodyStr, _ = sjson.Delete(bodyStr, "top_p")

	// The standard Responses API exposes explicit prompt-cache controls, but
	// ChatGPT's Codex endpoint rejects them. Keep prompt_cache_key (supported by
	// Codex for affinity) while removing the unsupported policy and per-content
	// breakpoint fields at this provider boundary.
	bodyStr = sanitizeCodexPromptCacheJSON(bodyStr)

	// Final gate: ChatGPT backend rejects items with empty or non-conforming id.
	// The SDK-level sanitizer only covers a subset of input item variants, so
	// scrub the marshaled JSON to catch every variant the SDK may emit.
	bodyStr = sanitizeCodexInputIDsJSON(bodyStr)

	// Drop message items whose content serialized as "" — Codex treats empty
	// string content as a missing required parameter.
	bodyStr = sanitizeCodexEmptyContentJSON(bodyStr)

	// ChatGPT's Codex backend rejects role="system" input messages. Responses
	// requests normally carry system text in `instructions`, but protocol
	// conversion must use an input message when the source system block has an
	// explicit cache breakpoint. Lift that text back into `instructions` at this
	// provider boundary; standard Responses providers keep the richer cache
	// representation unchanged.
	bodyStr = normalizeCodexSystemMessagesJSON(bodyStr)

	return []byte(bodyStr), nil
}

// sanitizeCodexInputIDsJSON walks the input array and removes invalid ids.
// For types whose id is required, the entire item is dropped (the backend
// would reject the request anyway). For types whose id is optional, only
// the id field is removed.
func sanitizeCodexInputIDsJSON(bodyStr string) string {
	input := gjson.Get(bodyStr, "input")
	if !input.IsArray() {
		return bodyStr
	}

	items := input.Array()
	// Iterate in reverse so deletions don't shift earlier indices.
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		idVal := item.Get("id")
		if !idVal.Exists() {
			continue
		}
		idStr := strings.TrimSpace(idVal.String())
		if idStr != "" && isValidCodexID(idStr) {
			continue
		}

		itemType := item.Get("type").String()
		path := fmt.Sprintf("input.%d", i)
		if codexInputItemIDRequired(itemType) {
			logrus.Warnf("[Codex] Dropping input[%d] of type %q with invalid id %q", i, itemType, idStr)
			if updated, err := sjson.Delete(bodyStr, path); err == nil {
				bodyStr = updated
			}
		} else {
			logrus.Debugf("[Codex] Clearing invalid id on input[%d] type %q", i, itemType)
			if updated, err := sjson.Delete(bodyStr, path+".id"); err == nil {
				bodyStr = updated
			}
		}
	}
	return bodyStr
}

// sanitizeCodexEmptyContentJSON drops input items of type "message" whose content
// field is an empty string. Codex treats "content": "" as a missing required
// parameter, which results in an invalid_request_error.
func sanitizeCodexEmptyContentJSON(bodyStr string) string {
	input := gjson.Get(bodyStr, "input")
	if !input.IsArray() {
		return bodyStr
	}

	items := input.Array()
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		content := item.Get("content")
		if !content.Exists() || content.Type != gjson.String || content.String() != "" {
			continue
		}
		logrus.Warnf("[Codex] Dropping input[%d] (type=%q) with empty string content", i, item.Get("type").String())
		path := fmt.Sprintf("input.%d", i)
		if updated, err := sjson.Delete(bodyStr, path); err == nil {
			bodyStr = updated
		}
	}
	return bodyStr
}

// sanitizeCodexPromptCacheJSON removes standard Responses cache-control fields
// that the ChatGPT Codex backend does not accept. Content breakpoints can occur
// on message content and function-call output content.
func sanitizeCodexPromptCacheJSON(bodyStr string) string {
	bodyStr, _ = sjson.Delete(bodyStr, "prompt_cache_options")
	bodyStr, _ = sjson.Delete(bodyStr, "prompt_cache_retention")

	input := gjson.Get(bodyStr, "input")
	if !input.IsArray() {
		return bodyStr
	}
	for i, item := range input.Array() {
		for _, field := range []string{"content", "output"} {
			parts := item.Get(field)
			if !parts.IsArray() {
				continue
			}
			for j, part := range parts.Array() {
				if !part.Get("prompt_cache_breakpoint").Exists() {
					continue
				}
				path := fmt.Sprintf("input.%d.%s.%d.prompt_cache_breakpoint", i, field, j)
				bodyStr, _ = sjson.Delete(bodyStr, path)
			}
		}
	}
	return bodyStr
}

// normalizeCodexSystemMessagesJSON moves system-role input messages into the
// top-level instructions field. The ChatGPT Codex endpoint does not accept
// system messages in `input`, while the standard Responses API uses that shape
// to attach prompt-cache breakpoints to system content.
func normalizeCodexSystemMessagesJSON(bodyStr string) string {
	input := gjson.Get(bodyStr, "input")
	if !input.IsArray() {
		return bodyStr
	}

	var systemTexts []string
	var systemIndexes []int
	for i, item := range input.Array() {
		if item.Get("type").String() != "message" || item.Get("role").String() != "system" {
			continue
		}
		systemIndexes = append(systemIndexes, i)
		content := item.Get("content")
		if content.Type == gjson.String {
			if text := strings.TrimSpace(content.String()); text != "" {
				systemTexts = append(systemTexts, text)
			}
			continue
		}
		if !content.IsArray() {
			continue
		}
		for _, part := range content.Array() {
			if text := strings.TrimSpace(part.Get("text").String()); text != "" {
				systemTexts = append(systemTexts, text)
			}
		}
	}

	if len(systemIndexes) == 0 {
		return bodyStr
	}

	if existing := strings.TrimSpace(gjson.Get(bodyStr, "instructions").String()); existing != "" {
		systemTexts = append([]string{existing}, systemTexts...)
	}
	if len(systemTexts) > 0 {
		bodyStr, _ = sjson.Set(bodyStr, "instructions", strings.Join(systemTexts, "\n\n"))
	}

	// Delete in reverse so earlier indexes remain stable.
	for i := len(systemIndexes) - 1; i >= 0; i-- {
		bodyStr, _ = sjson.Delete(bodyStr, fmt.Sprintf("input.%d", systemIndexes[i]))
	}
	return bodyStr
}

// codexInputItemIDRequired reports whether the given input item type requires
// the `id` field per the OpenAI Responses API schema. For these types, the
// SDK marshals an empty id as "" rather than omitting it, and the ChatGPT
// backend rejects it.
func codexInputItemIDRequired(itemType string) bool {
	switch itemType {
	case "reasoning",
		"code_interpreter_call",
		"computer_call",
		"file_search_call",
		"web_search_call",
		"image_generation_call",
		"local_shell_call",
		"local_shell_call_output",
		"mcp_list_tools",
		"mcp_approval_request",
		"mcp_call",
		"item_reference":
		return true
	}
	return false
}

func rewriteCodexPath(path string) string {
	if strings.HasPrefix(path, "/backend-api/") {
		return rewriteCodexAPIPath(path)
	}
	if strings.HasPrefix(path, "/v1/") && !strings.Contains(path, "/codex/") {
		return strings.Replace(path, "/v1/", "/codex/", 1)
	}
	return path
}

func rewriteCodexAPIPath(path string) string {
	switch {
	case strings.HasPrefix(path, "/backend-api/chat/completions"):
		return "/backend-api/codex/responses"
	case path == "/backend-api/responses":
		return "/backend-api/codex/responses"
	case strings.HasPrefix(path, "/backend-api/v1/"):
		return strings.Replace(path, "/backend-api/v1/", "/backend-api/codex/", 1)
	default:
		return path
	}
}
