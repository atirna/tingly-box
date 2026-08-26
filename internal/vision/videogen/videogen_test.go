package videogen

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

func TestDetectVendor(t *testing.T) {
	cases := []struct {
		name  string
		base  string
		style protocol.APIStyle
		want  Vendor
	}{
		{"openai", "https://api.openai.com/v1", protocol.APIStyleOpenAI, VendorOpenAICompat},
		{"dashscope-cn", "https://dashscope.aliyuncs.com/compatible-mode/v1", protocol.APIStyleOpenAI, VendorDashScope},
		{"dashscope-intl", "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", protocol.APIStyleOpenAI, VendorDashScope},
		{"minimax-com", "https://api.minimaxi.com/v1", protocol.APIStyleOpenAI, VendorMinimax},
		{"minimax-io", "https://api.minimax.io/v1", protocol.APIStyleOpenAI, VendorMinimax},
		{"volcengine", "https://ark.cn-beijing.volces.com/api/v3", protocol.APIStyleOpenAI, VendorArk},
		{"byteplus", "https://ark.ap-southeast.bytepluses.com/api/v3", protocol.APIStyleOpenAI, VendorArk},
		{"anthropic-style", "https://api.anthropic.com", protocol.APIStyleAnthropic, VendorUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &typ.Provider{Name: tc.name, APIBase: tc.base, APIStyle: tc.style}
			if got := DetectVendor(p); got != tc.want {
				t.Fatalf("DetectVendor(%s) = %s, want %s", tc.base, got, tc.want)
			}
		})
	}
}

func TestDetectVendorNil(t *testing.T) {
	if got := DetectVendor(nil); got != VendorUnknown {
		t.Fatalf("DetectVendor(nil) = %s, want %s", got, VendorUnknown)
	}
}

func TestJobIDRoundTrip(t *testing.T) {
	id := EncodeJobID("prov-uuid-1234", "task_abc|def")
	if !strings.HasPrefix(id, "tbv_") {
		t.Fatalf("EncodeJobID = %q, want tbv_ prefix", id)
	}
	uuid, native, err := DecodeJobID(id)
	if err != nil {
		t.Fatalf("DecodeJobID: %v", err)
	}
	// The native id may itself contain the separator; only the first one is
	// the split point.
	if uuid != "prov-uuid-1234" || native != "task_abc|def" {
		t.Fatalf("DecodeJobID = (%q, %q)", uuid, native)
	}
}

func TestDecodeJobIDRejectsForeignIDs(t *testing.T) {
	for _, id := range []string{"video_123", "tbv_%%%", "tbv_" /* empty payload */, ""} {
		if _, _, err := DecodeJobID(id); err == nil {
			t.Fatalf("DecodeJobID(%q) expected error", id)
		}
	}
}

func TestRequestFromOpenAI(t *testing.T) {
	p := &openai.VideoNewParams{
		Model:   openai.VideoModel("sora-2"),
		Prompt:  "a red fox running",
		Seconds: openai.VideoSeconds("8"),
		Size:    openai.VideoSize("1280x720"),
	}
	req := RequestFromOpenAI(p)
	if req.Model != "sora-2" || req.Prompt != "a red fox running" || req.Seconds != "8" || req.Size != "1280x720" {
		t.Fatalf("unexpected request: %+v", req)
	}
}

func TestJobToOpenAIRoundTrip(t *testing.T) {
	job := &Job{
		ID:        "tbv_xyz",
		Status:    StatusFailed,
		Progress:  42,
		Model:     "sora-2",
		Prompt:    "a red fox",
		Seconds:   "8",
		Size:      "1280x720",
		CreatedAt: 123,
		Error:     &JobError{Code: "moderation_blocked", Message: "nope"},
	}
	out := job.ToOpenAI()
	if out.ID != "tbv_xyz" || out.Status != openai.VideoStatusFailed || out.Progress != 42 {
		t.Fatalf("unexpected video: %+v", out)
	}
	if out.Error.Code != "moderation_blocked" {
		t.Fatalf("unexpected error: %+v", out.Error)
	}
	// The wire shape must carry object=video for OpenAI SDK clients.
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["object"] != "video" {
		t.Fatalf("marshalled object = %v, want video", m["object"])
	}

	back := JobFromOpenAI(out)
	if back.ID != job.ID || back.Status != job.Status || back.Error == nil || back.Error.Code != "moderation_blocked" {
		t.Fatalf("JobFromOpenAI lost data: %+v", back)
	}
}

func TestStatusMappings(t *testing.T) {
	dashscope := map[string]JobStatus{
		"PENDING": StatusQueued, "RUNNING": StatusInProgress,
		"SUCCEEDED": StatusCompleted, "FAILED": StatusFailed,
		"CANCELED": StatusFailed, "UNKNOWN": StatusFailed,
	}
	for in, want := range dashscope {
		if got := dashscopeStatus(in); got != want {
			t.Fatalf("dashscopeStatus(%s) = %s, want %s", in, got, want)
		}
	}
	minimax := map[string]JobStatus{
		"Queueing": StatusQueued, "Preparing": StatusQueued,
		"Processing": StatusInProgress, "Success": StatusCompleted,
		"Fail": StatusFailed,
	}
	for in, want := range minimax {
		if got := minimaxStatus(in); got != want {
			t.Fatalf("minimaxStatus(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestMinimaxResolution(t *testing.T) {
	cases := []struct {
		size string
		want string
	}{
		{"1920x1080", "1080P"},
		{"1280x768", "768P"},
		{"1280x720", ""}, // no matching class -> upstream default
		{"", ""},
	}
	for _, tc := range cases {
		if got := minimaxResolution(&Request{Size: tc.size}); got != tc.want {
			t.Fatalf("minimaxResolution(%q) = %q, want %q", tc.size, got, tc.want)
		}
	}
	got := minimaxResolution(&Request{Size: "1280x720", Extra: map[string]any{"resolution": "1080P"}})
	if got != "1080P" {
		t.Fatalf("explicit resolution override = %q, want 1080P", got)
	}
}

func TestNewOpenAICompatReturnsUnsupported(t *testing.T) {
	// OpenAI providers are not served by videogen.New — client.OpenAIClient
	// forwards those to the SDK's native Videos service.
	p := &typ.Provider{Name: "compat", APIBase: "https://api.openai.com/v1", APIStyle: protocol.APIStyleOpenAI}
	if _, err := New(p, "sora-2"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("New err = %v, want ErrUnsupported", err)
	}
}

// TestDashScopeLifecycle drives the DashScope adapter end-to-end against a
// stub upstream: submit -> queued job, poll -> completed job with a video
// URL, download -> that URL.
func TestDashScopeLifecycle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/video-generation/video-synthesis"):
			if r.Header.Get("X-DashScope-Async") != "enable" {
				t.Errorf("missing X-DashScope-Async header")
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			params, _ := body["parameters"].(map[string]any)
			if params["size"] != "1280*720" {
				t.Errorf("size = %v, want 1280*720", params["size"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"request_id": "req-1",
				"output":     map[string]any{"task_id": "task-1", "task_status": "PENDING"},
			})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/tasks/task-1"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"request_id": "req-2",
				"output": map[string]any{
					"task_id": "task-1", "task_status": "SUCCEEDED",
					"video_url": "https://cdn.example.com/v.mp4",
				},
				"usage": map[string]any{"video_duration": 5},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := &typ.Provider{Name: "dashscope-test", APIBase: srv.URL, Token: "sk-test"}
	c, err := newDashScopeClient(p)
	if err != nil {
		t.Fatalf("newDashScopeClient: %v", err)
	}
	defer c.Close()

	job, err := c.Create(context.Background(), &Request{Model: "wan2.2-t2v-plus", Prompt: "a fox", Size: "1280x720"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if job.ID != "task-1" || job.Status != StatusQueued {
		t.Fatalf("unexpected job: %+v", job)
	}

	job, err = c.Get(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if job.Status != StatusCompleted || job.VideoURL != "https://cdn.example.com/v.mp4" || job.Seconds != "5" {
		t.Fatalf("unexpected job: %+v", job)
	}

	content, err := c.Download(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if content.URL != "https://cdn.example.com/v.mp4" {
		t.Fatalf("unexpected content: %+v", content)
	}
}

// TestMinimaxLifecycle drives the MiniMax adapter end-to-end against a stub
// upstream: submit -> task id, query -> success + file id, retrieve ->
// download URL.
func TestMinimaxLifecycle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/video_generation"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["duration"] != float64(6) {
				t.Errorf("duration = %v, want 6", body["duration"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"task_id":   "mm-task-1",
				"base_resp": map[string]any{"status_code": 0},
			})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/query/video_generation"):
			if r.URL.Query().Get("task_id") != "mm-task-1" {
				t.Errorf("task_id = %q", r.URL.Query().Get("task_id"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"task_id": "mm-task-1", "status": "Success", "file_id": 205258526306433,
				"base_resp": map[string]any{"status_code": 0},
			})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/files/retrieve"):
			if r.URL.Query().Get("file_id") != "205258526306433" {
				t.Errorf("file_id = %q", r.URL.Query().Get("file_id"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"file":      map[string]any{"file_id": 205258526306433, "download_url": "https://cdn.example.com/mm.mp4"},
				"base_resp": map[string]any{"status_code": 0},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := &typ.Provider{Name: "minimax-test", APIBase: srv.URL + "/v1", Token: "sk-test"}
	c, err := newMinimaxClient(p)
	if err != nil {
		t.Fatalf("newMinimaxClient: %v", err)
	}
	defer c.Close()

	job, err := c.Create(context.Background(), &Request{Model: "MiniMax-Hailuo-02", Prompt: "a fox", Seconds: "6"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if job.ID != "mm-task-1" || job.Status != StatusQueued {
		t.Fatalf("unexpected job: %+v", job)
	}

	job, err = c.Get(context.Background(), "mm-task-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if job.Status != StatusCompleted {
		t.Fatalf("unexpected job: %+v", job)
	}

	content, err := c.Download(context.Background(), "mm-task-1")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if content.URL != "https://cdn.example.com/mm.mp4" {
		t.Fatalf("unexpected content: %+v", content)
	}
}

// TestArkLifecycle drives the Volcengine Ark (Seedance) adapter end-to-end
// against a stub upstream: submit -> queued task, poll -> succeeded task with
// a video URL, download -> that URL. Also asserts the normalized Size/Seconds
// are rendered as Ark's "--flag value" text commands.
func TestArkLifecycle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/contents/generations/tasks"):
			var body struct {
				Model   string `json:"model"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Content) != 1 || body.Content[0].Type != "text" {
				t.Errorf("unexpected content: %+v", body.Content)
			}
			text := body.Content[0].Text
			for _, want := range []string{"--ratio 16:9", "--resolution 720p", "--duration 5"} {
				if !strings.Contains(text, want) {
					t.Errorf("prompt %q missing %q", text, want)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "cgt-1", "status": "queued"})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/contents/generations/tasks/cgt-1"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "cgt-1", "status": "succeeded",
				"content":    map[string]any{"video_url": "https://cdn.example.com/ark.mp4"},
				"usage":      map[string]any{"completion_tokens": 123},
				"created_at": 1, "updated_at": 2,
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := &typ.Provider{Name: "ark-test", APIBase: srv.URL + "/api/v3", Token: "sk-test"}
	c, err := newArkClient(p)
	if err != nil {
		t.Fatalf("newArkClient: %v", err)
	}
	defer c.Close()

	job, err := c.Create(context.Background(), &Request{
		Model: "doubao-seedance-1-0-pro", Prompt: "a fox", Size: "1280x720", Seconds: "5",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if job.ID != "cgt-1" || job.Status != StatusQueued {
		t.Fatalf("unexpected job: %+v", job)
	}

	job, err = c.Get(context.Background(), "cgt-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if job.Status != StatusCompleted || job.VideoURL != "https://cdn.example.com/ark.mp4" || job.CompletedAt != 2 {
		t.Fatalf("unexpected job: %+v", job)
	}

	content, err := c.Download(context.Background(), "cgt-1")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if content.URL != "https://cdn.example.com/ark.mp4" {
		t.Fatalf("unexpected content: %+v", content)
	}
}

// TestArkPromptRespectsExplicitFlags verifies user-supplied "--flag" commands
// in the prompt are not duplicated by derived ones.
func TestArkPromptRespectsExplicitFlags(t *testing.T) {
	got := arkPrompt(&Request{Prompt: "a fox --duration 10", Seconds: "5", Size: "1920x1080"})
	if strings.Contains(got, "--duration 5") {
		t.Fatalf("derived duration should not override explicit one: %q", got)
	}
	for _, want := range []string{"--ratio 16:9", "--resolution 1080p"} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt %q missing %q", got, want)
		}
	}
}

// TestMinimaxBusinessError verifies that a base_resp business error carried in
// an HTTP 200 surfaces as a Go error.
func TestMinimaxBusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"base_resp": map[string]any{"status_code": 1008, "status_msg": "insufficient balance"},
		})
	}))
	defer srv.Close()

	p := &typ.Provider{Name: "minimax-test", APIBase: srv.URL + "/v1", Token: "sk-test"}
	c, _ := newMinimaxClient(p)
	defer c.Close()

	if _, err := c.Create(context.Background(), &Request{Model: "T2V-01", Prompt: "a fox"}); err == nil || !strings.Contains(err.Error(), "1008") {
		t.Fatalf("Create err = %v, want business error 1008", err)
	}
}
