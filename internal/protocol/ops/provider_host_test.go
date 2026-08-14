package ops

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitProviderHostPath(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantHost string
		wantPath string
	}{
		{"scheme + path", "https://api.deepseek.com/v1", "api.deepseek.com", "/v1"},
		{"bare host, no scheme", "api.deepseek.com", "api.deepseek.com", ""},
		{"bare host + path, no scheme", "opencode.ai/zen/go", "opencode.ai", "/zen/go"},
		{"port stripped", "https://api.deepseek.com:8443/v1", "api.deepseek.com", "/v1"},
		{"userinfo stripped", "https://user:pass@api.deepseek.com/v1", "api.deepseek.com", "/v1"},
		{"uppercase normalized", "HTTPS://API.DeepSeek.COM/V1", "api.deepseek.com", "/v1"},
		{"query string excluded from path", "https://gateway.example.com/relay?target=api.deepseek.com", "gateway.example.com", "/relay"},
		{"IPv6 host + port", "https://[::1]:8080/v1", "::1", "/v1"},
		{"IPv6 host, no port", "https://[2001:db8::1]/v1", "2001:db8::1", "/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, path := SplitProviderHostPath(tt.url)
			assert.Equal(t, tt.wantHost, host)
			assert.Equal(t, tt.wantPath, path)
		})
	}
}
