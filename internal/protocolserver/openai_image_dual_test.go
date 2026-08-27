package protocolserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// TestImageForwarding_DualProviderResolvesOpenAIEndpoint proves the image
// surfaces resolve a dual provider to its OpenAI-side base URL before
// forwarding, exactly like the chat/responses paths do via ResolveStyle.
// Without that call the request would go to the dead primary APIBase and
// fail. Routed through /tingly/team (default-team scope), so the team
// scenario's image path is exercised through the real route chain too.
func TestImageForwarding_DualProviderResolvesOpenAIEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream, lastPath := newPathRecordingUpstream(t, map[string]any{
		"created": 1,
		"data":    []map[string]any{{"b64_json": editTestPNGBase64}},
		"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
	router := newDualProviderRouter(t, upstream.URL, protocol.APIStyleOpenAI,
		typ.ScenarioTeam, "img-model", "img-upstream-model")

	genBody := `{"model": "img-model", "prompt": "a cat"}`
	editBody := `{"model": "img-model", "prompt": "a cat", "image": "` + editTestPNGBase64 + `"}`

	tests := []struct{ name, path, body, wantPath string }{
		{name: "generation", path: "/tingly/team/v1/images/generations", body: genBody, wantPath: "/v1/images/generations"},
		{name: "edit", path: "/tingly/team/v1/images/edits", body: editBody, wantPath: "/v1/images/edits"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
			assert.Equal(t, tt.wantPath, lastPath())
		})
	}
}
