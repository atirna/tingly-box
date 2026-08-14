package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tingly-dev/tingly-box/internal/typ"
)

func TestIsDeepSeekProvider(t *testing.T) {
	tests := []struct {
		name    string
		apiBase string
		want    bool
	}{
		{"deepseek host with scheme", "https://api.deepseek.com/v1", true},
		{"deepseek host without scheme", "api.deepseek.com", true},
		{"other host", "https://api.openai.com/v1", false},
		// Regression: the old strings.Contains(apiBase, "api.deepseek.com")
		// matched this too, mistaking a proxy relaying to DeepSeek for
		// DeepSeek itself.
		{"hostname merely mentioned in path/query", "https://gateway.example.com/relay?target=api.deepseek.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isDeepSeekProvider(tt.apiBase))
		})
	}
}

func TestIsOpenRouterProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider *typ.Provider
		want     bool
	}{
		{
			"APIBase matches",
			&typ.Provider{APIBase: "https://openrouter.ai/api/v1"},
			true,
		},
		{
			"APIBaseOpenAI matches",
			&typ.Provider{APIBaseOpenAI: "https://openrouter.ai/api/v1"},
			true,
		},
		{
			"APIBaseAnthropic matches",
			&typ.Provider{APIBaseAnthropic: "https://openrouter.ai/api"},
			true,
		},
		{
			"no match",
			&typ.Provider{APIBase: "https://api.openai.com/v1"},
			false,
		},
		// Regression: the old strings.Contains(base, "openrouter.ai") matched
		// this too, mistaking a proxy relaying to OpenRouter for OpenRouter
		// itself.
		{
			"hostname merely mentioned in path/query",
			&typ.Provider{APIBase: "https://gateway.example.com/relay?target=openrouter.ai"},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isOpenRouterProvider(tt.provider))
		})
	}
}
