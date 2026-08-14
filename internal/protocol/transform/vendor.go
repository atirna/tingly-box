package transform

import (
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/protocol/ops"
)

// VendorTransform applies provider-specific request adjustments. Per-shape
// dispatch matches the provider URL's host (see ops.SplitProviderHostPath) —
// uniform across all request shapes so new vendors land in one place per
// shape. Provider data is read from TransformContext so this transform
// remains stateless and reusable.
type VendorTransform struct{}

// NewVendorTransform creates a new vendor transform.
func NewVendorTransform() *VendorTransform {
	return &VendorTransform{}
}

func (t *VendorTransform) Name() string { return "vendor_adjust" }

// Apply dispatches to the per-shape vendor logic. Unknown shapes are a no-op.
func (t *VendorTransform) Apply(ctx *TransformContext) error {
	providerURL := t.providerURL(ctx)
	host, _ := ops.SplitProviderHostPath(providerURL)
	switch req := ctx.Request.(type) {
	case *openai.ChatCompletionNewParams:
		ctx.Request = t.applyChat(ctx, req, providerURL)
	case *responses.ResponseNewParams:
		ctx.Request = t.applyResponses(ctx, req)
	case *anthropic.MessageNewParams:
		ctx.Request = t.applyAnthropicV1(ctx, req, host)
	case *anthropic.BetaMessageNewParams:
		ctx.Request = t.applyAnthropicBeta(ctx, req, host)
	}
	return nil
}

func (t *VendorTransform) providerURL(ctx *TransformContext) string {
	if ctx != nil && ctx.Provider != nil {
		return ctx.Provider.APIBase
	}
	return ""
}

func (t *VendorTransform) applyChat(ctx *TransformContext, req *openai.ChatCompletionNewParams, providerURL string) *openai.ChatCompletionNewParams {
	config := ctx.Config.OpenAIConfig
	if config == nil {
		config = &protocol.OpenAIConfig{}
	}
	return ops.ApplyProviderTransforms(req, providerURL, string(req.Model), config)
}

func (t *VendorTransform) applyResponses(ctx *TransformContext, req *responses.ResponseNewParams) *responses.ResponseNewParams {
	if req == nil || req.Model == "" {
		return req
	}
	// MENTION: no need to do transform here, the codex client will handle this
	//if t.providerURL(ctx) == protocol.CodexAPIBase {
	//	return ops.ApplyCodexResponsesTransform(req, ctx.OriginalRequest)
	//}
	return req
}

func (t *VendorTransform) applyAnthropicV1(ctx *TransformContext, req *anthropic.MessageNewParams, host string) *anthropic.MessageNewParams {
	if req.Model == "" {
		return req
	}
	switch host {
	case "api.anthropic.com", "claude.ai":
		req = ops.ApplyAnthropicV1ModelTransform(req, string(req.Model))
		req = ops.ApplyAnthropicV1MetadataTransform(req, ctx.configExtraForMetadata())
	case "api.deepseek.com":
		ops.SanitizeAnthropicV1ThinkingConfig(req)
		ops.ApplyAnthropicV1DeepSeekThinkingPatch(req)
	}
	return req
}

func (t *VendorTransform) applyAnthropicBeta(ctx *TransformContext, req *anthropic.BetaMessageNewParams, host string) *anthropic.BetaMessageNewParams {
	if req.Model == "" {
		return req
	}
	switch host {
	case "api.anthropic.com", "claude.ai":
		req = ops.ApplyAnthropicBetaModelTransform(req, string(req.Model))
		req = ops.ApplyAnthropicBetaMetadataTransform(req, ctx.configExtraForMetadata())
	case "api.deepseek.com":
		ops.SanitizeAnthropicBetaThinkingConfig(req)
		ops.ApplyAnthropicBetaDeepSeekThinkingPatch(req)
	}
	return req
}
