package ops

// ResponseTransform applies provider-specific transformations to OpenAI responses
type ResponseTransform func(map[string]interface{}, string, string) map[string]interface{}

// responseConfig maps a provider's APIBase host to its response transform.
type responseConfig struct {
	APIBaseHost string
	Transform   ResponseTransform
}

// ResponseConfigs holds all registered provider response configurations
var ResponseConfigs = []responseConfig{
	// DeepSeek - ensure reasoning_content is always present
	{"api.deepseek.com", applyDeepSeekResponseTransform},
}

// GetResponseTransform identifies provider by APIBase URL and returns its
// response transform. Matches on the parsed host (see SplitProviderHostPath)
// rather than searching for APIBaseHost as a substring anywhere in
// providerURL, so a base URL that merely mentions a vendor's hostname in its
// path or query isn't mistaken for that vendor.
func GetResponseTransform(providerURL string) ResponseTransform {
	if providerURL == "" {
		return nil
	}
	host, _ := SplitProviderHostPath(providerURL)
	if host == "" {
		return nil
	}

	for _, config := range ResponseConfigs {
		if host == config.APIBaseHost {
			return config.Transform
		}
	}

	return nil
}

// ApplyResponseTransforms applies provider-specific transformations to responses
func ApplyResponseTransforms(resp map[string]interface{}, providerURL, model string) map[string]interface{} {
	if transform := GetResponseTransform(providerURL); transform != nil {
		return transform(resp, providerURL, model)
	}
	return resp
}

// applyDeepSeekResponseTransform ensures reasoning_content is present for DeepSeek
func applyDeepSeekResponseTransform(resp map[string]interface{}, providerURL, model string) map[string]interface{} {
	if choices, ok := resp["choices"].([]map[string]interface{}); ok && len(choices) > 0 {
		if message, ok := choices[0]["message"].(map[string]interface{}); ok {
			if _, has := message["reasoning_content"]; !has {
				message["reasoning_content"] = ""
			}
		}
	}
	return resp
}
