package videogen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// minimaxClient implements video generation against MiniMax (Hailuo / T2V-01
// family). MiniMax's video API is asynchronous with a bespoke three-step
// lifecycle — NOT the OpenAI /videos contract:
//
//	POST {APIBase}/video_generation                  -> task_id
//	GET  {APIBase}/query/video_generation?task_id=…  -> status (+ file_id on success)
//	GET  {APIBase}/files/retrieve?file_id=…          -> download_url
//
// Reference: https://platform.minimax.io/docs/guides/video-generation
type minimaxClient struct {
	provider    *typ.Provider
	httpClient  *http.Client
	submitURL   string
	queryURL    string
	retrieveURL string
}

func newMinimaxClient(provider *typ.Provider) (*minimaxClient, error) {
	base := strings.TrimRight(provider.APIBase, "/")
	if base == "" {
		return nil, fmt.Errorf("videogen: minimax provider %q has no API base", provider.Name)
	}
	return &minimaxClient{
		provider:    provider,
		httpClient:  &http.Client{Transport: http.DefaultTransport},
		submitURL:   base + "/video_generation",
		queryURL:    base + "/query/video_generation",
		retrieveURL: base + "/files/retrieve",
	}, nil
}

func (c *minimaxClient) Provider() *typ.Provider { return c.provider }

func (c *minimaxClient) Vendor() Vendor { return VendorMinimax }

func (c *minimaxClient) Close() error {
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
	return nil
}

type minimaxBaseResp struct {
	StatusCode int64  `json:"status_code"`
	StatusMsg  string `json:"status_msg"`
}

type minimaxSubmitRequest struct {
	Model      string `json:"model"`
	Prompt     string `json:"prompt"`
	Duration   int    `json:"duration,omitempty"`
	Resolution string `json:"resolution,omitempty"`
	// FirstFrameImage feeds the image-to-video models; passed through from
	// Extra["first_frame_image"].
	FirstFrameImage string `json:"first_frame_image,omitempty"`
}

type minimaxSubmitResponse struct {
	TaskID   string          `json:"task_id"`
	BaseResp minimaxBaseResp `json:"base_resp"`
}

type minimaxQueryResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	// FileID is documented as a string but has been observed as a bare
	// number; json.Number tolerates both.
	FileID   json.Number     `json:"file_id"`
	BaseResp minimaxBaseResp `json:"base_resp"`
}

type minimaxRetrieveResponse struct {
	File struct {
		FileID      json.Number `json:"file_id"`
		DownloadURL string      `json:"download_url"`
	} `json:"file"`
	BaseResp minimaxBaseResp `json:"base_resp"`
}

func (c *minimaxClient) Create(ctx context.Context, req *Request) (*Job, error) {
	body := minimaxSubmitRequest{
		Model:      req.Model,
		Prompt:     req.Prompt,
		Duration:   parseSeconds(req.Seconds),
		Resolution: minimaxResolution(req),
	}
	if img, ok := req.Extra["first_frame_image"].(string); ok {
		body.FirstFrameImage = img
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("videogen: minimax marshal request: %w", err)
	}

	raw, err := c.do(ctx, http.MethodPost, c.submitURL, payload)
	if err != nil {
		return nil, err
	}
	var parsed minimaxSubmitResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("videogen: minimax parse submit response: %w", err)
	}
	// MiniMax reports business errors inside base_resp with HTTP 200.
	if parsed.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("videogen: minimax error %d: %s", parsed.BaseResp.StatusCode, parsed.BaseResp.StatusMsg)
	}
	if parsed.TaskID == "" {
		return nil, fmt.Errorf("videogen: minimax submit returned no task_id")
	}
	logrus.Debugf("[MiniMax] video task submitted: %s", parsed.TaskID)

	return &Job{
		ID:      parsed.TaskID,
		Status:  StatusQueued,
		Model:   req.Model,
		Prompt:  req.Prompt,
		Seconds: req.Seconds,
		Size:    req.Size,
	}, nil
}

func (c *minimaxClient) Get(ctx context.Context, jobID string) (*Job, error) {
	parsed, err := c.query(ctx, jobID)
	if err != nil {
		return nil, err
	}
	job := &Job{
		ID:     jobID,
		Status: minimaxStatus(parsed.Status),
	}
	if job.Status == StatusFailed {
		job.Error = &JobError{Code: parsed.Status, Message: parsed.BaseResp.StatusMsg}
	}
	return job, nil
}

func (c *minimaxClient) Download(ctx context.Context, jobID string) (*Content, error) {
	parsed, err := c.query(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if status := minimaxStatus(parsed.Status); status != StatusCompleted {
		return nil, fmt.Errorf("videogen: minimax task %s is %s, not completed", jobID, status)
	}
	fileID := parsed.FileID.String()
	if fileID == "" {
		return nil, fmt.Errorf("videogen: minimax task %s completed without a file_id", jobID)
	}

	raw, err := c.do(ctx, http.MethodGet, c.retrieveURL+"?file_id="+url.QueryEscape(fileID), nil)
	if err != nil {
		return nil, err
	}
	var retrieved minimaxRetrieveResponse
	if err := json.Unmarshal(raw, &retrieved); err != nil {
		return nil, fmt.Errorf("videogen: minimax parse retrieve response: %w", err)
	}
	if retrieved.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("videogen: minimax error %d: %s", retrieved.BaseResp.StatusCode, retrieved.BaseResp.StatusMsg)
	}
	if retrieved.File.DownloadURL == "" {
		return nil, fmt.Errorf("videogen: minimax file %s has no download url", fileID)
	}
	return &Content{URL: retrieved.File.DownloadURL}, nil
}

func (c *minimaxClient) query(ctx context.Context, taskID string) (*minimaxQueryResponse, error) {
	raw, err := c.do(ctx, http.MethodGet, c.queryURL+"?task_id="+url.QueryEscape(taskID), nil)
	if err != nil {
		return nil, err
	}
	var parsed minimaxQueryResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("videogen: minimax parse query response: %w", err)
	}
	if parsed.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("videogen: minimax error %d: %s", parsed.BaseResp.StatusCode, parsed.BaseResp.StatusMsg)
	}
	return &parsed, nil
}

func (c *minimaxClient) do(ctx context.Context, method, endpoint string, payload []byte) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.provider.GetAccessToken())

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("videogen: minimax request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("videogen: minimax returned %d: %s", resp.StatusCode, string(raw))
	}
	return raw, nil
}

// minimaxStatus maps MiniMax task states onto the normalized lifecycle.
func minimaxStatus(status string) JobStatus {
	switch strings.ToLower(status) {
	case "queueing", "preparing":
		return StatusQueued
	case "processing":
		return StatusInProgress
	case "success":
		return StatusCompleted
	default:
		// "Fail" and anything unrecognized.
		return StatusFailed
	}
}

// minimaxResolutions is the fixed set of resolution classes MiniMax accepts.
var minimaxResolutions = map[string]string{
	"512":  "512P",
	"768":  "768P",
	"1080": "1080P",
}

// minimaxResolution resolves the resolution class MiniMax expects. An explicit
// Extra["resolution"] wins; otherwise it is derived from the normalized
// "WIDTHxHEIGHT" size when one dimension matches a supported class — falling
// back to the upstream default for anything else.
func minimaxResolution(req *Request) string {
	if req.Extra != nil {
		if r, ok := req.Extra["resolution"].(string); ok && r != "" {
			return r
		}
	}
	size := strings.ToLower(strings.TrimSpace(req.Size))
	if size == "" {
		return ""
	}
	for _, dim := range strings.Split(size, "x") {
		if r, ok := minimaxResolutions[strings.TrimSpace(dim)]; ok {
			return r
		}
	}
	logrus.Debugf("[MiniMax] size %q maps to no supported resolution class, using upstream default", req.Size)
	return ""
}
