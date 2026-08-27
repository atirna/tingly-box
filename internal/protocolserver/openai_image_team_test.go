package protocolserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestImageEndpoints_TeamScenarioPassesTransportGate is the regression test
// for image generation/edit being unusable under the team scenario: the
// handlers gate on TransportImageGen, which the team descriptor did not
// declare, so /tingly/team/v1/images/* was rejected before rule lookup ever
// ran. An isolated team scope ("team:<id>", derived by teamScopeMiddleware
// from the authenticated team) must clear the gate and proceed to rule
// resolution — with no rules configured that means the "not configured"
// error, never the transport rejection. The default-team scope is covered
// end to end by TestImageForwarding_DualProviderResolvesOpenAIEndpoint.
func TestImageEndpoints_TeamScenarioPassesTransportGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(ProtocolHandlerDeps{}).RegisterRoutes(router, setTeamIDFromHeader)

	genBody := `{"model": "img-model", "prompt": "a cat"}`
	editBody := `{"model": "img-model", "prompt": "a cat", "image": "` + editTestPNGBase64 + `"}`

	tests := []struct{ name, path, body, teamID, wantErr string }{
		{name: "generation isolated team scope", path: "/tingly/team/v1/images/generations", body: genBody, teamID: "team-a", wantErr: "not configured"},
		{name: "edit isolated team scope", path: "/tingly/team/v1/images/edits", body: editBody, teamID: "team-a", wantErr: "not configured"},
		// Control: a scenario without TransportImageGen still fails closed.
		{name: "generation rejected for anthropic-only scenario", path: "/tingly/claude_code/v1/images/generations", body: genBody, wantErr: "does not support"},
		{name: "edit rejected for anthropic-only scenario", path: "/tingly/claude_code/v1/images/edits", body: editBody, wantErr: "does not support"},
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

			assert.Contains(t, w.Body.String(), tt.wantErr)
		})
	}
}
