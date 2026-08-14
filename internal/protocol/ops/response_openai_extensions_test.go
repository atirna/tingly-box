package ops

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetResponseTransformMatchesDeepSeekHost(t *testing.T) {
	transform := GetResponseTransform("https://api.deepseek.com/v1")
	assert.NotNil(t, transform)
}

func TestGetResponseTransformMatchesBareHostnameWithoutScheme(t *testing.T) {
	transform := GetResponseTransform("api.deepseek.com")
	assert.NotNil(t, transform)
}

// TestGetResponseTransformDoesNotMatchHostnameMentionedElsewhereInURL proves
// the fix for the old strings.Contains(providerURL, pattern) matching: a base
// URL whose path or query merely mentions a vendor's hostname as text (e.g. a
// proxy relaying to a target named in a query parameter) must not be
// mistaken for that vendor.
func TestGetResponseTransformDoesNotMatchHostnameMentionedElsewhereInURL(t *testing.T) {
	transform := GetResponseTransform("https://gateway.example.com/relay?target=api.deepseek.com")
	assert.Nil(t, transform)
}

func TestGetResponseTransformNoMatch(t *testing.T) {
	transform := GetResponseTransform("https://api.openai.com/v1")
	assert.Nil(t, transform)
}

func TestApplyDeepSeekResponseTransformAddsEmptyReasoningContent(t *testing.T) {
	resp := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"message": map[string]interface{}{"content": "hi"}},
		},
	}

	result := ApplyResponseTransforms(resp, "https://api.deepseek.com/v1", "deepseek-v4-flash")

	choices := result["choices"].([]map[string]interface{})
	message := choices[0]["message"].(map[string]interface{})
	assert.Equal(t, "", message["reasoning_content"])
}
