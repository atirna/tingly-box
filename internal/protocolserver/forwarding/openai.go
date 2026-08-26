package forwarding

import (
	"context"
	"fmt"

	"github.com/openai/openai-go/v3"
	openaistream "github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
	"github.com/sirupsen/logrus"
	"github.com/tingly-dev/tingly-box/internal/client"
	"github.com/tingly-dev/tingly-box/internal/vision/videogen"
)

// ForwardOpenAIChat sends a non-streaming OpenAI chat completion request.
// IMPORTANT: All transformations (protocol conversion + vendor-specific) should
// be applied by the transform chain BEFORE calling this function.
func ForwardOpenAIChat(fc *ForwardContext, wrapper client.OpenAIClientInterface, req *openai.ChatCompletionNewParams) (*openai.ChatCompletion, context.CancelFunc, error) {
	if wrapper == nil {
		return nil, nil, fmt.Errorf("failed to get OpenAI client for provider: %s", fc.Provider.Name)
	}

	ctx, cancel := fc.PrepareContext(req)

	// Clear empty tools array
	if len(req.Tools) == 0 {
		req.Tools = nil
	}

	logrus.Infof("provider: %s, model: %s", fc.Provider.Name, req.Model)

	resp, err := wrapper.ChatCompletionsNew(ctx, *req)
	fc.Complete(ctx, resp, err)

	return resp, cancel, err
}

// ForwardOpenAIEmbeddings sends an OpenAI embeddings request.
// Embeddings have no streaming and skip the chat transform chain.
func ForwardOpenAIEmbeddings(fc *ForwardContext, wrapper client.OpenAIClientInterface, req *openai.EmbeddingNewParams) (*openai.CreateEmbeddingResponse, context.CancelFunc, error) {
	if wrapper == nil {
		return nil, nil, fmt.Errorf("failed to get OpenAI client for provider: %s", fc.Provider.Name)
	}

	ctx, cancel := fc.PrepareContext(req)

	logrus.Infof("provider: %s, model: %s (embeddings)", fc.Provider.Name, req.Model)

	resp, err := wrapper.EmbeddingsNew(ctx, *req)
	fc.Complete(ctx, resp, err)

	return resp, cancel, err
}

// ForwardOpenAIImageGeneration sends an image generation request. The wrapper's
// ImagesGenerate handles vendor fragmentation internally — OpenAI-compatible
// providers go through the SDK directly, DashScope / MiniMax are dispatched to
// their native adapters, and Codex rides the Responses API — so this forwarder
// stays a thin, uniform entry point. Image generation has no streaming and
// skips the chat transform chain.
func ForwardOpenAIImageGeneration(fc *ForwardContext, wrapper client.OpenAIClientInterface, req *openai.ImageGenerateParams) (*openai.ImagesResponse, context.CancelFunc, error) {
	if wrapper == nil {
		return nil, nil, fmt.Errorf("failed to get OpenAI client for provider: %s", fc.Provider.Name)
	}

	ctx, cancel := fc.PrepareContext(req)

	logrus.Infof("provider: %s, model: %s (image generation)", fc.Provider.Name, req.Model)

	resp, err := wrapper.ImagesGenerate(ctx, *req)
	fc.Complete(ctx, resp, err)

	return resp, cancel, err
}

// ForwardOpenAIImageEdit sends an image edit request. Like generation, the
// wrapper's ImagesEdit hides vendor fragmentation — OpenAI-compatible
// providers go through the SDK's multipart /images/edits, Codex uses its
// native JSON images endpoint — keeping this forwarder a thin, uniform entry
// point. Image editing has no streaming and skips the chat transform chain.
func ForwardOpenAIImageEdit(fc *ForwardContext, wrapper client.OpenAIClientInterface, req *openai.ImageEditParams) (*openai.ImagesResponse, context.CancelFunc, error) {
	if wrapper == nil {
		return nil, nil, fmt.Errorf("failed to get OpenAI client for provider: %s", fc.Provider.Name)
	}

	ctx, cancel := fc.PrepareContext(req)

	logrus.Infof("provider: %s, model: %s (image edit)", fc.Provider.Name, req.Model)

	resp, err := wrapper.ImagesEdit(ctx, *req)
	fc.Complete(ctx, resp, err)

	return resp, cancel, err
}

// ForwardOpenAIVideoCreate submits a video generation job. The wrapper's
// VideoCreate handles vendor fragmentation internally — OpenAI providers go
// through the SDK's Videos service, DashScope / MiniMax are dispatched to
// their native videogen adapters — so this forwarder stays a thin, uniform
// entry point. Job creation has no streaming and skips the chat transform
// chain; polling and download run outside the routing pipeline (the job id
// carries the provider) and call the wrapper directly.
func ForwardOpenAIVideoCreate(fc *ForwardContext, vg client.VideoGenerator, req *openai.VideoNewParams) (*videogen.Job, context.CancelFunc, error) {
	if vg == nil {
		return nil, nil, fmt.Errorf("failed to get video-capable client for provider: %s", fc.Provider.Name)
	}

	ctx, cancel := fc.PrepareContext(req)

	logrus.Infof("provider: %s, model: %s (video generation)", fc.Provider.Name, req.Model)

	job, err := vg.VideoCreate(ctx, *req)
	fc.Complete(ctx, job, err)

	return job, cancel, err
}

// ForwardOpenAIChatStream sends a streaming OpenAI chat completion request.
// IMPORTANT: All transformations (protocol conversion + vendor-specific) should
// be applied by the transform chain BEFORE calling this function.
// Note: Pass request context (c.Request.Context()) as baseCtx in NewForwardContext for client cancellation support.
func ForwardOpenAIChatStream(fc *ForwardContext, wrapper client.OpenAIClientInterface, req *openai.ChatCompletionNewParams) (*openaistream.Stream[openai.ChatCompletionChunk], context.CancelFunc, error) {
	if wrapper == nil {
		return nil, nil, fmt.Errorf("failed to get OpenAI client for provider: %s", fc.Provider.Name)
	}
	logrus.Debugf("provider: %s (streaming)", fc.Provider.Name)

	ctx, cancel := fc.PrepareContext(req)

	stream := wrapper.ChatCompletionsNewStreaming(ctx, *req)
	return stream, cancel, nil
}

// ForwardOpenAIResponses sends a non-streaming OpenAI Responses API request.
func ForwardOpenAIResponses(fc *ForwardContext, wrapper client.OpenAIClientInterface, params responses.ResponseNewParams) (*responses.Response, context.CancelFunc, error) {
	if wrapper == nil {
		return nil, nil, fmt.Errorf("failed to get OpenAI client for provider: %s", fc.Provider.Name)
	}

	ctx, cancel := fc.PrepareContext(params)
	resp, err := wrapper.ResponsesNew(ctx, params)
	fc.Complete(ctx, resp, err)
	return resp, cancel, err
}

// ForwardOpenAIResponsesStream sends a streaming OpenAI Responses API request.
// Note: Pass request context (c.Request.Context()) as baseCtx in NewForwardContext for client cancellation support.
func ForwardOpenAIResponsesStream(fc *ForwardContext, wrapper client.OpenAIClientInterface, params responses.ResponseNewParams) (*openaistream.Stream[responses.ResponseStreamEventUnion], context.CancelFunc, error) {
	if wrapper == nil {
		return nil, nil, fmt.Errorf("failed to get OpenAI client for provider: %s", fc.Provider.Name)
	}

	ctx, cancel := fc.PrepareContext(params)
	stream := wrapper.ResponsesNewStreaming(ctx, params)
	return stream, cancel, nil
}
