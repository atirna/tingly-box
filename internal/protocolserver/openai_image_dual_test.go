package protocolserver

import (
	"encoding/base64"
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

// TestImageForwarding_DualProviderResolvesOpenAIEndpoint proves the image
// surfaces resolve a dual provider to its OpenAI-side base URL before
// forwarding, exactly like the chat/responses paths do via ResolveStyle.
//
// The dual provider is set up adversarially: its primary APIBase points at a
// dead address and only APIBaseOpenAI reaches the mock upstream. Without the
// ResolveStyle call in the image handlers the request would be sent to the
// dead primary URL and fail; with it, both generation and edit land on the
// OpenAI-side endpoint. Routed through /tingly/team so the whole
// team-scenario image path is exercised end to end.
func TestImageForwarding_DualProviderResolvesOpenAIEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	loadbalance.DefaultBreakerStore().Reset()

	var upstreamPaths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPaths = append(upstreamPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1,
			"data":    []map[string]any{{"b64_json": base64.StdEncoding.EncodeToString(editTestPNG)}},
			"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer upstream.Close()

	cfg, err := config.NewConfig(config.WithConfigDir(t.TempDir()))
	require.NoError(t, err)
	require.NoError(t, cfg.AddProvider(&typ.Provider{
		UUID: "prov-dual", Name: "prov-dual", Enabled: true,
		AuthType: typ.AuthTypeAPIKey, Token: "test-key",
		APIStyle: protocol.APIStyleOpenAI,
		// Primary URL is a dead address on purpose: only the dual OpenAI-side
		// URL reaches the upstream, so a hit proves ResolveStyle ran.
		APIBase:          "http://127.0.0.1:1/v1",
		APIBaseOpenAI:    upstream.URL + "/v1",
		APIBaseAnthropic: "http://127.0.0.1:1/anthropic",
	}))
	require.NoError(t, cfg.AddRule(typ.Rule{
		Scenario: typ.ScenarioTeam, RequestModel: "img-model", Active: true,
		Services: []*loadbalance.Service{{Provider: "prov-dual", Model: "img-upstream-model", Active: true}},
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
	router.POST("/tingly/:scenario/v1/images/generations", ph.teamScopeMiddleware, ph.HandleOpenAIImageGeneration)
	router.POST("/tingly/:scenario/v1/images/edits", ph.teamScopeMiddleware, ph.HandleOpenAIImageEdit)

	tests := []struct {
		name     string
		path     string
		body     string
		wantPath string
	}{
		{
			name:     "generation",
			path:     "/tingly/team/v1/images/generations",
			body:     `{"model": "img-model", "prompt": "a cat"}`,
			wantPath: "/v1/images/generations",
		},
		{
			name: "edit",
			path: "/tingly/team/v1/images/edits",
			body: `{"model": "img-model", "prompt": "a cat", "image": "` +
				base64.StdEncoding.EncodeToString(editTestPNG) + `"}`,
			wantPath: "/v1/images/edits",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstreamPaths = nil
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
			require.NotEmpty(t, upstreamPaths, "request never reached the dual OpenAI-side upstream")
			assert.Equal(t, tt.wantPath, upstreamPaths[0])
		})
	}
}
