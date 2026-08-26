// Package videogen provides vendor adapters for text-to-video generation
// surfaces that do NOT speak the OpenAI /videos contract. Video generation is
// even more fragmented than image generation: every vendor ships a bespoke
// async job API (submit -> poll -> fetch asset), and unlike images there is no
// broadly adopted OpenAI-compatible surface yet — OpenAI's own /videos (Sora)
// is the closest thing to a de-facto standard, so this package normalizes on
// its job-based shape (queued / in_progress / completed / failed).
//
// It is an implementation detail of client.OpenAIClient: that client's video
// methods dispatch DashScope / MiniMax providers here and serve OpenAI
// providers through the SDK's native Videos service. The package is
// intentionally a leaf (it does not import internal/client) so the client
// layer can depend on it without an import cycle — the same layering as the
// sibling imagegen package.
//
// Vendor landscape (derived from internal/data/providers.json):
//
//	OpenAI native async job API (POST /videos, GET /videos/{id},
//	GET /videos/{id}/content — Sora):
//	  openai-com.
//	  -> NOT handled here; client.OpenAIClient serves these via the SDK.
//
//	Native async task API (submit -> poll task_id):
//	  dashscope-cn, dashscope-intl (Alibaba Wan text-to-video,
//	  POST /api/v1/services/aigc/video-generation/video-synthesis).
//	  -> handled by dashscopeClient.
//
//	Native async task API (submit -> query -> file retrieve):
//	  minimaxi-com, minimax-io (MiniMax Hailuo / T2V-01,
//	  POST /v1/video_generation).
//	  -> handled by minimaxClient.
//
//	Native async content-generation task API (submit -> poll):
//	  volces-com, byteplus (Volcengine Ark / BytePlus, Doubao Seedance,
//	  POST /api/v3/contents/generations/tasks). Ark's chat surface is
//	  OpenAI-compatible but its video surface is not.
//	  -> handled by arkClient.
//
// Unlike image generation there is no sync path: jobs run for minutes, so the
// gateway exposes the job surface itself instead of blocking the request. The
// job id handed back to the caller embeds the serving provider (see id.go), so
// the later GET /videos/{id} and /videos/{id}/content requests can be routed
// back to the same upstream without any server-side state.
package videogen

import (
	"context"
	"errors"
	"io"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared/constant"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// ErrUnsupported is returned by New when a provider has no native video
// generation adapter in this package. The caller (client.OpenAIClient) only
// dispatches DashScope / MiniMax providers here, so in practice this signals a
// detection/routing bug rather than an expected condition.
var ErrUnsupported = errors.New("videogen: provider does not support video generation")

// Client is the vendor-neutral video generation contract. Video generation is
// job-based on every vendor, so the contract mirrors the OpenAI Videos API
// lifecycle: Create submits a job, Get polls it, Download fetches the finished
// asset. Adapters translate each step into their native schema.
type Client interface {
	// Create submits a video generation job and returns it in its initial
	// (usually queued) state. It never blocks until completion.
	Create(ctx context.Context, req *Request) (*Job, error)
	// Get fetches the current state of a previously submitted job. jobID is
	// the upstream-native id (Job.ID as returned by Create).
	Get(ctx context.Context, jobID string) (*Job, error)
	// Download resolves the playable asset of a completed job. Vendors that
	// host results on a CDN return Content.URL; vendors that stream bytes
	// return Content.Body (caller closes).
	Download(ctx context.Context, jobID string) (*Content, error)
	// Provider returns the upstream provider this client is bound to.
	Provider() *typ.Provider
	// Vendor returns the detected vendor family for diagnostics.
	Vendor() Vendor
	// Close releases any resources held by the client.
	Close() error
}

// Request is the normalized video generation request. It mirrors the common
// subset of the OpenAI Videos API; vendor adapters translate it into their
// native schema and ignore fields they do not support (logging a warning).
type Request struct {
	// Model is the upstream model id (already resolved by routing).
	Model string
	// Prompt is the text description of the desired video.
	Prompt string
	// Seconds is the clip duration in seconds as a decimal string ("4", "8",
	// ...). Vendors with a numeric duration parameter parse it.
	Seconds string
	// Size is "WIDTHxHEIGHT" (e.g. "1280x720"). Adapters that expect an
	// aspect ratio, a "W*H" form, or a resolution class convert from this.
	Size string
	// Extra carries vendor-specific passthrough parameters that have no
	// normalized field (seed, negative prompt, resolution class, ...).
	// Adapters merge these into their native request body.
	Extra map[string]any
}

// JobStatus is the normalized job lifecycle state, using the OpenAI Videos API
// vocabulary. Vendor adapters map their native states onto these four.
type JobStatus string

const (
	StatusQueued     JobStatus = "queued"
	StatusInProgress JobStatus = "in_progress"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
)

// JobError describes why a job failed, when the upstream reports it.
type JobError struct {
	Code    string
	Message string
}

// Job is the normalized video generation job.
type Job struct {
	// ID is the upstream-native job/task id. The gateway wraps it with the
	// serving provider before handing it to callers (see EncodeJobID).
	ID string
	// Status is the normalized lifecycle state.
	Status JobStatus
	// Progress is the approximate completion percentage when the upstream
	// reports it (0 otherwise).
	Progress int64
	// Model is the model id the job runs on.
	Model string
	// Prompt echoes the submitted prompt when the upstream returns it.
	Prompt string
	// Seconds / Size echo the requested duration and resolution when known.
	Seconds string
	Size    string
	// CreatedAt / CompletedAt / ExpiresAt are unix seconds; zero when the
	// upstream does not report them.
	CreatedAt   int64
	CompletedAt int64
	ExpiresAt   int64
	// VideoURL is the hosted asset URL for vendors that return one on
	// completion (DashScope, MiniMax). Empty for byte-streaming vendors.
	VideoURL string
	// Error is set when Status is StatusFailed and the upstream explains why.
	Error *JobError
}

// Content is the playable asset of a completed job. Exactly one of URL or Body
// is set: URL for CDN-hosted results (the gateway redirects), Body for
// byte-streamed results (the gateway proxies; caller must close).
type Content struct {
	URL         string
	Body        io.ReadCloser
	ContentType string
}

// RequestFromOpenAI converts an OpenAI Videos API create request into the
// normalized Request, so the OpenAI-compatible inbound surface feeds the
// adapters without each handler re-deriving the mapping.
func RequestFromOpenAI(p *openai.VideoNewParams) *Request {
	if p == nil {
		return nil
	}
	return &Request{
		Model:   string(p.Model),
		Prompt:  p.Prompt,
		Seconds: string(p.Seconds),
		Size:    string(p.Size),
	}
}

// JobFromOpenAI converts an SDK video job (returned by the native OpenAI
// Videos service) into the normalized Job, so client.OpenAIClient exposes one
// shape regardless of vendor.
func JobFromOpenAI(v *openai.Video) *Job {
	if v == nil {
		return nil
	}
	job := &Job{
		ID:          v.ID,
		Status:      JobStatus(v.Status),
		Progress:    v.Progress,
		Model:       string(v.Model),
		Prompt:      v.Prompt,
		Seconds:     string(v.Seconds),
		Size:        string(v.Size),
		CreatedAt:   v.CreatedAt,
		CompletedAt: v.CompletedAt,
		ExpiresAt:   v.ExpiresAt,
	}
	if v.Error.Code != "" || v.Error.Message != "" {
		job.Error = &JobError{Code: v.Error.Code, Message: v.Error.Message}
	}
	return job
}

// ToOpenAI converts the normalized Job back into the OpenAI Videos API job
// shape, so OpenAI-compatible inbound clients see a familiar payload no matter
// which vendor serves the job.
func (j *Job) ToOpenAI() *openai.Video {
	if j == nil {
		return nil
	}
	out := &openai.Video{
		ID:          j.ID,
		Status:      openai.VideoStatus(j.Status),
		Progress:    j.Progress,
		Model:       openai.VideoModel(j.Model),
		Prompt:      j.Prompt,
		Seconds:     openai.VideoSeconds(j.Seconds),
		Size:        openai.VideoSize(j.Size),
		CreatedAt:   j.CreatedAt,
		CompletedAt: j.CompletedAt,
		ExpiresAt:   j.ExpiresAt,
		Object:      constant.ValueOf[constant.Video](),
	}
	if j.Error != nil {
		out.Error = openai.VideoCreateError{Code: j.Error.Code, Message: j.Error.Message}
	}
	return out
}
