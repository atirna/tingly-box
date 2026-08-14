package ops

// ResponseTransform applies provider-specific transformations to OpenAI responses
type ResponseTransform func(map[string]interface{}, string, string) map[string]interface{}

// GetResponseTransform identifies the provider by the parsed host of
// providerURL (see SplitProviderHostPath) — not by searching for a vendor
// hostname as a substring anywhere in providerURL, so a base URL that merely
// mentions a vendor's hostname in its path or query isn't mistaken for that
// vendor — and returns its response transform, or nil if none applies.
func GetResponseTransform(providerURL string) ResponseTransform {
	host, _ := SplitProviderHostPath(providerURL)
	switch host {
	case "api.deepseek.com":
		return applyDeepSeekResponseTransform
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
