package protocolserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tingly-dev/tingly-box/internal/client"
	"github.com/tingly-dev/tingly-box/internal/loadbalance"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/routing"
	"github.com/tingly-dev/tingly-box/internal/server/config"
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
	loadbalance.DefaultBreakerStore().Reset()

	var upstreamPaths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPaths = append(upstreamPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{{
				"object": "embedding", "index": 0, "embedding": []float64{0.1, 0.2},
			}},
			"model": "embed-upstream-model",
			"usage": map[string]any{"prompt_tokens": 2, "total_tokens": 2},
		})
	}))
	defer upstream.Close()

	cfg, err := config.NewConfig(config.WithConfigDir(t.TempDir()))
	require.NoError(t, err)
	require.NoError(t, cfg.AddProvider(&typ.Provider{
		UUID: "prov-dual", Name: "prov-dual", Enabled: true,
		AuthType: typ.AuthTypeAPIKey, Token: "test-key",
		APIStyle:         protocol.APIStyleAnthropic,
		APIBase:          "http://127.0.0.1:1/anthropic",
		APIBaseOpenAI:    upstream.URL + "/v1",
		APIBaseAnthropic: "http://127.0.0.1:1/anthropic",
	}))
	require.NoError(t, cfg.AddRule(typ.Rule{
		Scenario: typ.ScenarioEmbed, RequestModel: "embed-model", Active: true,
		Services: []*loadbalance.Service{{Provider: "prov-dual", Model: "embed-upstream-model", Active: true}},
	}))

	hm := loadbalance.NewHealthMonitor(loadbalance.HealthMonitorConfig{ProbeEnabled: false})
	lb := NewLoadBalancer(cfg, routing.NewHealthFilter(hm))
	selector := routing.NewSimpleSelector(routing.NewServiceSelector(cfg, NewAffinityStore(0), lb))
	ph := NewHandler(ProtocolHandlerDeps{
		Config:          cfg,
		ClientPool:      client.NewClientPool(),
		RoutingSelector: selector,
		LoadBalancer:    lb,
		HealthMonitor:   hm,
	})

	router := gin.New()
	router.POST("/tingly/:scenario/v1/embeddings", ph.teamScopeMiddleware, ph.HandleOpenAIEmbeddings)

	body := `{"model": "embed-model", "input": "hello"}`
	req := httptest.NewRequest(http.MethodPost, "/tingly/embed/v1/embeddings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	require.NotEmpty(t, upstreamPaths, "request never reached the dual OpenAI-side upstream")
	assert.Equal(t, "/v1/embeddings", upstreamPaths[0])
	// The response model must echo the caller's request model, not the routed one.
	assert.Contains(t, w.Body.String(), `"model":"embed-model"`)
}
