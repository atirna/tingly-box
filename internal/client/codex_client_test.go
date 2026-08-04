package client

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

func TestApplyCodexDefaultsToParams_FastSuffix(t *testing.T) {
	req := responses.ResponseNewParams{Model: "gpt-5.6-sol:fast"}

	applyCodexDefaultsToParams(&req)

	if req.Model != "gpt-5.6-sol" {
		t.Fatalf("expected model to be stripped to %q, got %q", "gpt-5.6-sol", req.Model)
	}
	if req.ServiceTier != responses.ResponseNewParamsServiceTierPriority {
		t.Fatalf("expected service tier %q, got %q", responses.ResponseNewParamsServiceTierPriority, req.ServiceTier)
	}
}

func TestApplyCodexDefaultsToParams_NoFastSuffix(t *testing.T) {
	req := responses.ResponseNewParams{Model: "gpt-5.6-sol"}

	applyCodexDefaultsToParams(&req)

	if req.Model != "gpt-5.6-sol" {
		t.Fatalf("expected model to remain %q, got %q", "gpt-5.6-sol", req.Model)
	}
	if req.ServiceTier != "" {
		t.Fatalf("expected empty service tier, got %q", req.ServiceTier)
	}
}

// systemInputMessage builds an EasyInputMessage input item with the given role and text.
func systemInputMessage(role, text string) responses.ResponseInputItemUnionParam {
	return responses.ResponseInputItemUnionParam{
		OfMessage: &responses.EasyInputMessageParam{
			Type:    responses.EasyInputMessageTypeMessage,
			Role:    responses.EasyInputMessageRole(role),
			Content: responses.EasyInputMessageContentUnionParam{OfString: param.NewOpt(text)},
		},
	}
}

// TestCodexWireBody_NoSystemMessageOrCacheFields runs the real request-shaping
// path (applyCodexDefaultsToParams → SDK marshal → filterField) and asserts the
// final wire body carries the system prompt via instructions — the ChatGPT
// backend rejects system input messages with 400 "System messages are not
// allowed" (issue #1457) — and no prompt-cache extension fields.
func TestCodexWireBody_NoSystemMessageOrCacheFields(t *testing.T) {
	req := responses.ResponseNewParams{
		Model: "gpt-5-codex",
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: responses.ResponseInputParam{
				{
					OfMessage: &responses.EasyInputMessageParam{
						Type: responses.EasyInputMessageTypeMessage,
						Role: responses.EasyInputMessageRole("system"),
						Content: responses.EasyInputMessageContentUnionParam{
							OfInputItemContentList: responses.ResponseInputMessageContentListParam{
								{OfInputText: &responses.ResponseInputTextParam{
									Text:                  "You are Claude Code.",
									PromptCacheBreakpoint: responses.NewResponseInputTextPromptCacheBreakpointParam(),
								}},
							},
						},
					},
				},
				systemInputMessage("user", "hello"),
			},
		},
	}
	req.PromptCacheOptions.Mode = "explicit"
	applyCodexDefaultsToParams(&req)

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	rt := &codexRoundTripper{}
	filtered, err := rt.filterField(raw)
	if err != nil {
		t.Fatalf("filterField failed: %v", err)
	}

	var out struct {
		Instructions string `json:"instructions"`
		Input        []struct {
			Role string `json:"role"`
		} `json:"input"`
	}
	if err := json.Unmarshal(filtered, &out); err != nil {
		t.Fatalf("unmarshal filtered body: %v", err)
	}

	if out.Instructions != "You are Claude Code." {
		t.Fatalf("expected instructions from system message, got %q", out.Instructions)
	}
	for _, item := range out.Input {
		if item.Role == "system" || item.Role == "developer" {
			t.Fatalf("system/developer message leaked into wire body: %s", filtered)
		}
	}
	if strings.Contains(string(filtered), "prompt_cache") {
		t.Fatalf("prompt cache extension fields leaked into wire body: %s", filtered)
	}
}

// TestApplyCodexDefaultsToParams_ServiceTierSurvivesWireBody guards against the
// request body being silently stripped of "service_tier" by the second body-shaping
// pass in codexRoundTripper.filterField (a deny-list JSON filter applied to the
// already-marshaled body, separate from the SDK struct marshaling).
func TestApplyCodexDefaultsToParams_ServiceTierSurvivesWireBody(t *testing.T) {
	req := responses.ResponseNewParams{Model: "gpt-5.6-sol:fast"}
	applyCodexDefaultsToParams(&req)

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	rt := &codexRoundTripper{}
	filtered, err := rt.filterField(raw)
	if err != nil {
		t.Fatalf("filterField failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(filtered, &out); err != nil {
		t.Fatalf("unmarshal filtered body: %v", err)
	}

	if out["service_tier"] != "priority" {
		t.Fatalf("expected service_tier=priority in final wire body, got %v (full body: %s)", out["service_tier"], filtered)
	}
	if out["model"] != "gpt-5.6-sol" {
		t.Fatalf("expected model=gpt-5.6-sol in final wire body, got %v", out["model"])
	}
}
