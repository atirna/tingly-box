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

// TestEmbeddingsForwarding_DualProviderResolvesOpenAIEndpoint is the
// embeddings counterpart of the image dual-endpoint regression test. The dual
// provider is doubly adversarial: its primary APIStyle is anthropic (so the
// handler's OpenAI-only style check would reject it without resolution) and
// its primary APIBase is a dead address (so a served request proves the
// OpenAI-side dual URL was used).
func TestEmbeddingsForwarding_DualProviderResolvesOpenAIEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream, lastPath := newPathRecordingUpstream(t, map[string]any{
		"object": "list",
		"data": []map[string]any{{
			"object": "embedding", "index": 0, "embedding": []float64{0.1, 0.2},
		}},
		"model": "embed-upstream-model",
		"usage": map[string]any{"prompt_tokens": 2, "total_tokens": 2},
	})
	router := newDualProviderRouter(t, upstream.URL, protocol.APIStyleAnthropic,
		typ.ScenarioEmbed, "embed-model", "embed-upstream-model")

	body := `{"model": "embed-model", "input": "hello"}`
	req := httptest.NewRequest(http.MethodPost, "/tingly/embed/v1/embeddings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, "/v1/embeddings", lastPath())
	// The response model must echo the caller's request model, not the routed one.
	assert.Contains(t, w.Body.String(), `"model":"embed-model"`)
}
