package protocolserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openai/openai-go/v3"
	"github.com/sirupsen/logrus"

	"github.com/tingly-dev/tingly-box/internal/client"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/protocolserver/forwarding"
	"github.com/tingly-dev/tingly-box/internal/typ"
	"github.com/tingly-dev/tingly-box/internal/vision/videogen"
)

// Video generation rides the OpenAI Videos job surface (Sora shape): POST
// /videos submits a job, GET /videos/{id} polls it, GET /videos/{id}/content
// fetches the finished asset. Jobs run for minutes, so unlike image
// generation the gateway never blocks a request until completion — it exposes
// the job lifecycle itself.
//
// Only job creation goes through routing (a rule resolves the model to a
// provider). The job id handed back embeds that provider (videogen.EncodeJobID),
// so poll/download requests are routed back to the same upstream statelessly —
// no server-side job store, and ids stay valid across gateway restarts.

// HandleOpenAIVideoCreate serves OpenAI-compatible video generation job
// submission. The client wrapper handles vendor fragmentation internally:
// OpenAI providers go through the SDK's Videos service, DashScope / MiniMax /
// Ark (Seedance) are dispatched to their native videogen adapters.
//
// The inbound body is JSON (prompt/model/seconds/size). OpenAI's own surface
// also accepts multipart for the input_reference asset; that variant is not
// supported yet — vendor-specific references (image URLs) ride the JSON body's
// extra fields instead.
func (ph *ProtocolHandler) HandleOpenAIVideoCreate(c *gin.Context) {
	scenario := c.Param("scenario")
	scenarioType := typ.RuleScenario(scenario)

	if !ph.validateVideoScenario(c, scenarioType) {
		return
	}

	bodyBytes, err := c.GetRawData()
	if err != nil {
		videoError(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body: "+err.Error())
		return
	}

	var req openai.VideoNewParams
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		videoError(c, http.StatusBadRequest, "invalid_request_error", "Invalid request body: "+err.Error())
		return
	}
	if string(req.Model) == "" {
		videoError(c, http.StatusBadRequest, "invalid_request_error", "Model is required")
		return
	}
	if req.Prompt == "" {
		videoError(c, http.StatusBadRequest, "invalid_request_error", "Prompt is required")
		return
	}

	requestModel := string(req.Model)
	responseModel := requestModel

	rule, err := ph.determineRuleWithScenario(c, scenarioType, requestModel)
	if err != nil {
		videoError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	provider, selectedService, err := ph.selectServiceForVideoGeneration(c, scenarioType, rule)
	if err != nil {
		videoError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	actualModel := selectedService.Model
	req.Model = openai.VideoModel(actualModel)

	sessionID := resolveSessionID(c, &req)
	c.Request = c.Request.WithContext(typ.WithSessionID(c.Request.Context(), sessionID))

	SetTrackingContext(c, rule, provider, actualModel, responseModel, false)

	fc := forwarding.NewForwardContext(c.Request.Context(), provider)

	wrapper := ph.deps.ClientPool.GetOpenAIClient(c.Request.Context(), provider, actualModel)
	vg, ok := wrapper.(client.VideoGenerator)
	if !ok {
		videoError(c, http.StatusNotImplemented, "invalid_request_error",
			fmt.Sprintf("provider %s does not support video generation", provider.Name))
		return
	}

	job, cancel, err := forwarding.ForwardOpenAIVideoCreate(fc, vg, &req)
	if cancel != nil {
		defer cancel()
	}
	if err != nil {
		usage := protocol.NewTokenUsageWithCache(0, 0, 0)
		ph.trackUsageWithTokenUsage(c, usage, err)
		logrus.Errorf("Failed to forward video generation request: %v", err)
		videoError(c, protocol.UpstreamStatus(err, http.StatusInternalServerError), "api_error",
			"Failed to forward request: "+err.Error())
		return
	}

	// Job submission consumes no tokens itself; usage is billed by the
	// upstream on completion. Track the request so rate/health accounting
	// still sees it.
	ph.trackUsageWithTokenUsage(c, protocol.NewTokenUsageWithCache(0, 0, 0), nil)

	// Hand out a gateway job id that embeds the serving provider, and echo
	// the model alias the caller asked for.
	job.ID = videogen.EncodeJobID(provider.UUID, job.ID)
	job.Model = responseModel

	c.JSON(http.StatusOK, job.ToOpenAI())
}

// HandleOpenAIVideoGet serves job status polling. The provider is recovered
// from the job id itself, so no rule/model routing happens here.
func (ph *ProtocolHandler) HandleOpenAIVideoGet(c *gin.Context) {
	scenarioType := typ.RuleScenario(c.Param("scenario"))
	if !ph.validateVideoScenario(c, scenarioType) {
		return
	}

	vg, gatewayID, nativeID, ok := ph.videoClientFromJobID(c)
	if !ok {
		return
	}

	job, err := vg.VideoGet(c.Request.Context(), nativeID)
	if err != nil {
		logrus.Errorf("Failed to fetch video job: %v", err)
		videoError(c, protocol.UpstreamStatus(err, http.StatusInternalServerError), "api_error", err.Error())
		return
	}
	job.ID = gatewayID
	c.JSON(http.StatusOK, job.ToOpenAI())
}

// HandleOpenAIVideoContent serves the finished asset of a completed job.
// Vendors that host results on a CDN yield a redirect; vendors that stream
// bytes (OpenAI) are proxied through.
func (ph *ProtocolHandler) HandleOpenAIVideoContent(c *gin.Context) {
	scenarioType := typ.RuleScenario(c.Param("scenario"))
	if !ph.validateVideoScenario(c, scenarioType) {
		return
	}

	vg, _, nativeID, ok := ph.videoClientFromJobID(c)
	if !ok {
		return
	}

	content, err := vg.VideoDownload(c.Request.Context(), nativeID)
	if err != nil {
		logrus.Errorf("Failed to download video content: %v", err)
		videoError(c, protocol.UpstreamStatus(err, http.StatusInternalServerError), "api_error", err.Error())
		return
	}

	if content.URL != "" {
		c.Redirect(http.StatusFound, content.URL)
		return
	}
	if content.Body == nil {
		videoError(c, http.StatusInternalServerError, "api_error", "upstream returned no video content")
		return
	}
	defer content.Body.Close()

	contentType := content.ContentType
	if contentType == "" {
		contentType = "video/mp4"
	}
	c.Header("Content-Type", contentType)
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, content.Body); err != nil {
		// Headers are already out; all we can do is log the broken stream.
		logrus.Errorf("Failed to stream video content: %v", err)
	}
}

// validateVideoScenario rejects unknown scenarios and scenarios whose
// descriptor does not declare the videogen transport.
func (ph *ProtocolHandler) validateVideoScenario(c *gin.Context, scenarioType typ.RuleScenario) bool {
	if !IsValidRuleScenario(scenarioType) {
		videoError(c, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("invalid scenario: %s", scenarioType))
		return false
	}
	if !typ.ScenarioSupportsTransport(scenarioType, typ.TransportVideoGen) {
		videoError(c, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("scenario %s does not support video generation", scenarioType))
		return false
	}
	return true
}

// videoClientFromJobID decodes the gateway job id from the request path,
// recovers the provider embedded in it, and returns a video-capable client
// for that provider. On failure it writes the error response and returns
// ok=false.
func (ph *ProtocolHandler) videoClientFromJobID(c *gin.Context) (vg client.VideoGenerator, gatewayID, nativeID string, ok bool) {
	gatewayID = c.Param("video_id")
	providerUUID, nativeID, err := videogen.DecodeJobID(gatewayID)
	if err != nil {
		videoError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, "", "", false
	}

	provider, err := ph.deps.Config.GetProviderByUUID(providerUUID)
	if err != nil || provider == nil {
		videoError(c, http.StatusNotFound, "invalid_request_error",
			"the provider that served this video job is no longer configured")
		return nil, "", "", false
	}

	wrapper := ph.deps.ClientPool.GetOpenAIClient(c.Request.Context(), provider, "")
	vg, isVG := wrapper.(client.VideoGenerator)
	if !isVG {
		videoError(c, http.StatusNotImplemented, "invalid_request_error",
			fmt.Sprintf("provider %s does not support video generation", provider.Name))
		return nil, "", "", false
	}
	return vg, gatewayID, nativeID, true
}

func videoError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, ErrorResponse{Error: ErrorDetail{Message: message, Type: errType}})
}
