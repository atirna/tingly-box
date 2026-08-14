package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tingly-dev/tingly-box/internal/typ"
)

// newModelLister's DeepSeek host match (see model_resolve.go) is a one-line
// switch on ops.SplitProviderHostPath, whose own correctness — including the
// false-positive-substring case a proxy URL merely mentioning the hostname
// used to trigger — is covered by internal/protocol/ops's
// TestSplitProviderHostPath and TestProviderDispatch* tests. No separate
// coverage is added here for that one-line dispatch.

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
