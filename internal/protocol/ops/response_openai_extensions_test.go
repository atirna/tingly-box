package ops

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func deepSeekResponseFixture() map[string]interface{} {
	return map[string]interface{}{
		"choices": []map[string]interface{}{
			{"message": map[string]interface{}{"content": "hi"}},
		},
	}
}

func TestApplyResponseTransformsMatchesDeepSeekHost(t *testing.T) {
	result := ApplyResponseTransforms(deepSeekResponseFixture(), "https://api.deepseek.com/v1", "deepseek-v4-flash")

	choices := result["choices"].([]map[string]interface{})
	message := choices[0]["message"].(map[string]interface{})
	assert.Equal(t, "", message["reasoning_content"],
		"DeepSeek response transform should add empty reasoning_content when missing")
}

func TestApplyResponseTransformsMatchesBareHostnameWithoutScheme(t *testing.T) {
	result := ApplyResponseTransforms(deepSeekResponseFixture(), "api.deepseek.com", "deepseek-v4-flash")

	choices := result["choices"].([]map[string]interface{})
	message := choices[0]["message"].(map[string]interface{})
	assert.Equal(t, "", message["reasoning_content"])
}

// TestApplyResponseTransformsDoesNotMatchHostnameMentionedElsewhereInURL
// proves the fix for the old strings.Contains(providerURL, pattern)
// matching: a base URL whose path or query merely mentions a vendor's
// hostname as text (e.g. a proxy relaying to a target named in a query
// parameter) must not be mistaken for that vendor.
func TestApplyResponseTransformsDoesNotMatchHostnameMentionedElsewhereInURL(t *testing.T) {
	resp := deepSeekResponseFixture()

	result := ApplyResponseTransforms(resp, "https://gateway.example.com/relay?target=api.deepseek.com", "some-model")

	choices := result["choices"].([]map[string]interface{})
	message := choices[0]["message"].(map[string]interface{})
	assert.NotContains(t, message, "reasoning_content",
		"a URL that merely mentions a vendor hostname in its path/query must not trigger that vendor's transform")
}

func TestApplyResponseTransformsNoMatchLeavesResponseUnchanged(t *testing.T) {
	resp := deepSeekResponseFixture()

	result := ApplyResponseTransforms(resp, "https://api.openai.com/v1", "gpt-4o")

	choices := result["choices"].([]map[string]interface{})
	message := choices[0]["message"].(map[string]interface{})
	assert.NotContains(t, message, "reasoning_content")
}
