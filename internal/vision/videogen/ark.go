package videogen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// arkClient implements video generation against Volcengine Ark / BytePlus
// (ByteDance Doubao Seedance models). Ark's video surface is its own async
// "content generation tasks" API — NOT the OpenAI /videos contract, even
// though Ark's chat surface is OpenAI-compatible on the same base:
//
//	POST {APIBase}/contents/generations/tasks       -> {id}
//	GET  {APIBase}/contents/generations/tasks/{id}  -> status + content.video_url
//
// Generation knobs (ratio, resolution, duration, ...) ride as "--flag value"
// text commands appended to the prompt, per Ark's convention.
//
// Reference: https://www.volcengine.com/docs/82379/1330310 (视频生成)
type arkClient struct {
	provider   *typ.Provider
	httpClient *http.Client
	tasksURL   string
}

func newArkClient(provider *typ.Provider) (*arkClient, error) {
	base := strings.TrimRight(provider.APIBase, "/")
	if base == "" {
		return nil, fmt.Errorf("videogen: ark provider %q has no API base", provider.Name)
	}
	return &arkClient{
		provider:   provider,
		httpClient: &http.Client{Transport: http.DefaultTransport},
		tasksURL:   base + "/contents/generations/tasks",
	}, nil
}

func (c *arkClient) Provider() *typ.Provider { return c.provider }

func (c *arkClient) Vendor() Vendor { return VendorArk }

func (c *arkClient) Close() error {
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
	return nil
}

type arkContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *arkImageURL `json:"image_url,omitempty"`
	// Role marks an image part as first/last frame reference on models that
	// support it; passed through from Extra.
	Role string `json:"role,omitempty"`
}

type arkImageURL struct {
	URL string `json:"url"`
}

type arkSubmitRequest struct {
	Model   string           `json:"model"`
	Content []arkContentPart `json:"content"`
}

type arkError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type arkTaskResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Status  string `json:"status"`
	Content struct {
		VideoURL string `json:"video_url"`
	} `json:"content"`
	Usage struct {
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	CreatedAt int64     `json:"created_at"`
	UpdatedAt int64     `json:"updated_at"`
	Error     *arkError `json:"error,omitempty"`
}

func (c *arkClient) Create(ctx context.Context, req *Request) (*Job, error) {
	content := []arkContentPart{{Type: "text", Text: arkPrompt(req)}}
	// First-frame (image-to-video) reference; passed through from
	// Extra["image_url"], optionally with Extra["image_role"].
	if img, ok := req.Extra["image_url"].(string); ok && img != "" {
		part := arkContentPart{Type: "image_url", ImageURL: &arkImageURL{URL: img}}
		if role, ok := req.Extra["image_role"].(string); ok {
			part.Role = role
		}
		content = append(content, part)
	}

	payload, err := json.Marshal(arkSubmitRequest{Model: req.Model, Content: content})
	if err != nil {
		return nil, fmt.Errorf("videogen: ark marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tasksURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.provider.GetAccessToken())

	parsed, err := c.doTask(httpReq)
	if err != nil {
		return nil, err
	}
	if parsed.ID == "" {
		return nil, fmt.Errorf("videogen: ark submit returned no task id")
	}
	logrus.Debugf("[Ark] video task submitted: %s", parsed.ID)

	job := c.toJob(parsed)
	job.Model = req.Model
	job.Prompt = req.Prompt
	job.Seconds = req.Seconds
	job.Size = req.Size
	if job.Status == "" {
		job.Status = StatusQueued
	}
	return job, nil
}

func (c *arkClient) Get(ctx context.Context, jobID string) (*Job, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.tasksURL+"/"+jobID, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.provider.GetAccessToken())

	parsed, err := c.doTask(httpReq)
	if err != nil {
		return nil, err
	}
	return c.toJob(parsed), nil
}

func (c *arkClient) Download(ctx context.Context, jobID string) (*Content, error) {
	job, err := c.Get(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if job.Status != StatusCompleted {
		return nil, fmt.Errorf("videogen: ark task %s is %s, not completed", jobID, job.Status)
	}
	if job.VideoURL == "" {
		return nil, fmt.Errorf("videogen: ark task %s completed without a video url", jobID)
	}
	return &Content{URL: job.VideoURL}, nil
}

func (c *arkClient) doTask(httpReq *http.Request) (*arkTaskResponse, error) {
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("videogen: ark request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("videogen: ark returned %d: %s", resp.StatusCode, string(raw))
	}
	var parsed arkTaskResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("videogen: ark parse response: %w", err)
	}
	return &parsed, nil
}

func (c *arkClient) toJob(parsed *arkTaskResponse) *Job {
	job := &Job{
		ID:        parsed.ID,
		Status:    arkStatus(parsed.Status),
		Model:     parsed.Model,
		VideoURL:  parsed.Content.VideoURL,
		CreatedAt: parsed.CreatedAt,
	}
	if job.Status == StatusCompleted {
		job.CompletedAt = parsed.UpdatedAt
	}
	if parsed.Error != nil {
		job.Error = &JobError{Code: parsed.Error.Code, Message: parsed.Error.Message}
	}
	return job
}

// arkStatus maps Ark content-generation task states onto the normalized
// lifecycle.
func arkStatus(status string) JobStatus {
	switch strings.ToLower(status) {
	case "queued":
		return StatusQueued
	case "running":
		return StatusInProgress
	case "succeeded":
		return StatusCompleted
	default:
		// "failed" / "cancelled" and anything unrecognized.
		return StatusFailed
	}
}

// arkResolutions maps a pixel height onto Ark's "--resolution" classes.
var arkResolutions = map[int]string{
	480:  "480p",
	720:  "720p",
	1080: "1080p",
}

// arkPrompt renders the prompt plus Ark's "--flag value" text commands derived
// from the normalized fields: Size becomes --ratio (reduced W:H) and, when the
// height matches a class, --resolution; Seconds becomes --duration. A flag
// already present in the prompt is left alone so explicit user commands win.
func arkPrompt(req *Request) string {
	var sb strings.Builder
	sb.WriteString(req.Prompt)

	appendFlag := func(flag, value string) {
		if value == "" || strings.Contains(req.Prompt, "--"+flag) {
			return
		}
		sb.WriteString(" --")
		sb.WriteString(flag)
		sb.WriteString(" ")
		sb.WriteString(value)
	}

	if w, h, ok := parseSize(req.Size); ok && w > 0 && h > 0 {
		g := gcd(w, h)
		appendFlag("ratio", fmt.Sprintf("%d:%d", w/g, h/g))
		appendFlag("resolution", arkResolutions[h])
	}
	if secs := parseSeconds(req.Seconds); secs > 0 {
		appendFlag("duration", strconv.Itoa(secs))
	}
	return sb.String()
}

func parseSize(size string) (int, int, bool) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(size)), "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return w, h, true
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}
