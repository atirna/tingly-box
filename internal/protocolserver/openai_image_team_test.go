package protocolserver

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/tingly-dev/tingly-box/internal/constant"
)

// TestImageEndpoints_TeamScenarioPassesTransportGate is the regression test
// for image generation/edit being unusable under the team scenario: the
// handlers gate on TransportImageGen, which the team descriptor did not
// declare, so /tingly/team/v1/images/* was rejected with "does not support"
// before rule lookup ever ran. Requests must now clear the transport gate for
// both the default team ("team") and an isolated team scope ("team:<id>")
// and proceed to rule resolution — with no rules configured that means the
// "not configured" error, never the transport rejection.
func TestImageEndpoints_TeamScenarioPassesTransportGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ph := &ProtocolHandler{}
	router := gin.New()
	teamAuthStub := func(c *gin.Context) {
		if teamID := c.GetHeader("X-Test-Team-ID"); teamID != "" {
			c.Set(constant.CtxKeyTeamID, teamID)
		}
	}
	router.POST("/tingly/:scenario/v1/images/generations",
		teamAuthStub, ph.teamScopeMiddleware, ph.HandleOpenAIImageGeneration)
	router.POST("/tingly/:scenario/v1/images/edits",
		teamAuthStub, ph.teamScopeMiddleware, ph.HandleOpenAIImageEdit)

	genBody := `{"model": "img-model", "prompt": "a cat"}`
	editBody := `{"model": "img-model", "prompt": "a cat", "image": "` +
		base64.StdEncoding.EncodeToString(editTestPNG) + `"}`

	tests := []struct {
		name         string
		path         string
		body         string
		teamID       string
		wantGatePass bool
	}{
		{name: "generation default team", path: "/tingly/team/v1/images/generations", body: genBody, wantGatePass: true},
		{name: "generation isolated team scope", path: "/tingly/team/v1/images/generations", body: genBody, teamID: "team-a", wantGatePass: true},
		{name: "edit default team", path: "/tingly/team/v1/images/edits", body: editBody, wantGatePass: true},
		{name: "edit isolated team scope", path: "/tingly/team/v1/images/edits", body: editBody, teamID: "team-a", wantGatePass: true},
		// Control: a scenario without TransportImageGen still fails closed.
		{name: "generation rejected for anthropic-only scenario", path: "/tingly/claude_code/v1/images/generations", body: genBody, wantGatePass: false},
		{name: "edit rejected for anthropic-only scenario", path: "/tingly/claude_code/v1/images/edits", body: editBody, wantGatePass: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			if tt.teamID != "" {
				req.Header.Set("X-Test-Team-ID", tt.teamID)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			if tt.wantGatePass {
				assert.NotContains(t, w.Body.String(), "does not support")
				assert.Contains(t, w.Body.String(), "not configured")
			} else {
				assert.Contains(t, w.Body.String(), "does not support")
			}
		})
	}
}
