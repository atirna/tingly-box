package videogen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// dashscopeClient implements video generation against Alibaba Model Studio /
// DashScope (Tongyi Wanxiang "Wan" text-to-video models). DashScope's video
// API is asynchronous: a submit call returns a task_id, and the caller polls a
// task endpoint until the task reaches a terminal state; the finished asset is
// a CDN URL in the task result.
//
// Endpoints (host derived from the provider APIBase, so both the Beijing
// dashscope.aliyuncs.com and the Singapore dashscope-intl.aliyuncs.com sites
// are supported):
//
//	POST {scheme}://{host}/api/v1/services/aigc/video-generation/video-synthesis
//	     header: X-DashScope-Async: enable
//	GET  {scheme}://{host}/api/v1/tasks/{task_id}
//
// Reference: https://www.alibabacloud.com/help/en/model-studio/text-to-video-api-reference
type dashscopeClient struct {
	provider    *typ.Provider
	httpClient  *http.Client
	submitURL   string
	taskBaseURL string
}

func newDashScopeClient(provider *typ.Provider) (*dashscopeClient, error) {
	host := apiHost(provider.APIBase)
	if host == "" {
		return nil, fmt.Errorf("videogen: dashscope provider %q has no API base host", provider.Name)
	}
	scheme := apiScheme(provider.APIBase)
	base := fmt.Sprintf("%s://%s/api/v1", scheme, host)

	return &dashscopeClient{
		provider:    provider,
		httpClient:  &http.Client{Transport: http.DefaultTransport},
		submitURL:   base + "/services/aigc/video-generation/video-synthesis",
		taskBaseURL: base + "/tasks/",
	}, nil
}

func (c *dashscopeClient) Provider() *typ.Provider { return c.provider }

func (c *dashscopeClient) Vendor() Vendor { return VendorDashScope }

func (c *dashscopeClient) Close() error {
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
	return nil
}

// dashscopeSubmitBody is the async task submission payload.
type dashscopeSubmitBody struct {
	Model      string                 `json:"model"`
	Input      dashscopeInput         `json:"input"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

type dashscopeInput struct {
	Prompt         string `json:"prompt"`
	NegativePrompt string `json:"negative_prompt,omitempty"`
	// ImgURL feeds the image-to-video models (wan*-i2v-*); passed through
	// from Extra["img_url"].
	ImgURL string `json:"img_url,omitempty"`
}

// dashscopeTaskResponse covers both the submit response and the poll response.
type dashscopeTaskResponse struct {
	RequestID string `json:"request_id"`
	Output    struct {
		TaskID       string `json:"task_id"`
		TaskStatus   string `json:"task_status"`
		VideoURL     string `json:"video_url"`
		SubmitTime   string `json:"submit_time"`
		EndTime      string `json:"end_time"`
		OrigPrompt   string `json:"orig_prompt"`
		ActualPrompt string `json:"actual_prompt"`
		Message      string `json:"message"`
		Code         string `json:"code"`
	} `json:"output"`
	Usage struct {
		VideoDuration int64  `json:"video_duration"`
		VideoRatio    string `json:"video_ratio"`
	} `json:"usage"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c *dashscopeClient) Create(ctx context.Context, req *Request) (*Job, error) {
	params := map[string]interface{}{}
	if size := dashscopeSize(req.Size); size != "" {
		params["size"] = size
	}
	if secs := parseSeconds(req.Seconds); secs > 0 {
		params["duration"] = secs
	}
	input := dashscopeInput{Prompt: req.Prompt}
	// Allow callers to pass DashScope-native knobs (seed, prompt_extend,
	// watermark, ...) straight through; input-level fields are lifted out of
	// the parameter map.
	extra := maps.Clone(req.Extra)
	if imgURL, ok := extra["img_url"].(string); ok {
		input.ImgURL = imgURL
		delete(extra, "img_url")
	}
	if negative, ok := extra["negative_prompt"].(string); ok {
		input.NegativePrompt = negative
		delete(extra, "negative_prompt")
	}
	maps.Copy(params, extra)

	body := dashscopeSubmitBody{
		Model:      req.Model,
		Input:      input,
		Parameters: params,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("videogen: dashscope marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.submitURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.provider.GetAccessToken())
	httpReq.Header.Set("X-DashScope-Async", "enable")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("videogen: dashscope submit: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("videogen: dashscope submit returned %d: %s", resp.StatusCode, string(raw))
	}

	var parsed dashscopeTaskResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("videogen: dashscope parse submit response: %w", err)
	}
	if parsed.Code != "" {
		return nil, fmt.Errorf("videogen: dashscope submit error %s: %s", parsed.Code, parsed.Message)
	}
	if parsed.Output.TaskID == "" {
		return nil, fmt.Errorf("videogen: dashscope submit returned no task_id")
	}
	logrus.Debugf("[DashScope] video task submitted: %s", parsed.Output.TaskID)

	job := c.toJob(&parsed)
	job.Model = req.Model
	job.Prompt = req.Prompt
	job.Seconds = req.Seconds
	job.Size = req.Size
	if job.Status == "" {
		job.Status = StatusQueued
	}
	return job, nil
}

func (c *dashscopeClient) Get(ctx context.Context, jobID string) (*Job, error) {
	parsed, err := c.fetchTask(ctx, jobID)
	if err != nil {
		return nil, err
	}
	return c.toJob(parsed), nil
}

func (c *dashscopeClient) Download(ctx context.Context, jobID string) (*Content, error) {
	parsed, err := c.fetchTask(ctx, jobID)
	if err != nil {
		return nil, err
	}
	job := c.toJob(parsed)
	if job.Status != StatusCompleted {
		return nil, fmt.Errorf("videogen: dashscope task %s is %s, not completed", jobID, job.Status)
	}
	if job.VideoURL == "" {
		return nil, fmt.Errorf("videogen: dashscope task %s completed without a video url", jobID)
	}
	return &Content{URL: job.VideoURL}, nil
}

func (c *dashscopeClient) fetchTask(ctx context.Context, taskID string) (*dashscopeTaskResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.taskBaseURL+taskID, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.provider.GetAccessToken())

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("videogen: dashscope poll: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("videogen: dashscope poll returned %d: %s", resp.StatusCode, string(raw))
	}
	var parsed dashscopeTaskResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("videogen: dashscope parse poll response: %w", err)
	}
	return &parsed, nil
}

func (c *dashscopeClient) toJob(parsed *dashscopeTaskResponse) *Job {
	job := &Job{
		ID:       parsed.Output.TaskID,
		Status:   dashscopeStatus(parsed.Output.TaskStatus),
		VideoURL: parsed.Output.VideoURL,
		Prompt:   parsed.Output.OrigPrompt,
	}
	if d := parsed.Usage.VideoDuration; d > 0 {
		job.Seconds = strconv.FormatInt(d, 10)
	}
	if job.Status == StatusFailed {
		code := parsed.Output.Code
		msg := parsed.Output.Message
		if msg == "" {
			msg = parsed.Message
		}
		if code == "" {
			code = parsed.Code
		}
		job.Error = &JobError{Code: code, Message: msg}
	}
	return job
}

// dashscopeStatus maps DashScope task states onto the normalized lifecycle.
func dashscopeStatus(status string) JobStatus {
	switch strings.ToUpper(status) {
	case "PENDING":
		return StatusQueued
	case "RUNNING":
		return StatusInProgress
	case "SUCCEEDED":
		return StatusCompleted
	default:
		// FAILED / CANCELED / UNKNOWN — all terminal, all failures from the
		// caller's point of view.
		return StatusFailed
	}
}

// dashscopeSize converts a normalized "WIDTHxHEIGHT" size into DashScope's
// "WIDTH*HEIGHT" form. Empty / non-conforming values are passed through so the
// upstream can apply its own default.
func dashscopeSize(size string) string {
	size = strings.TrimSpace(size)
	if size == "" {
		return ""
	}
	return strings.ReplaceAll(strings.ToLower(size), "x", "*")
}

// parseSeconds parses the normalized decimal-string duration; 0 means unset.
func parseSeconds(seconds string) int {
	seconds = strings.TrimSpace(seconds)
	if seconds == "" {
		return 0
	}
	n, err := strconv.Atoi(seconds)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
