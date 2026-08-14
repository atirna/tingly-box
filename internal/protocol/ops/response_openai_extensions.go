package ops

// ApplyResponseTransforms applies provider-specific transformations to a
// response, dispatched by the parsed host of providerURL (see
// SplitProviderHostPath) — not by searching for a vendor hostname as a
// substring anywhere in providerURL, so a base URL that merely mentions a
// vendor's hostname in its path or query isn't mistaken for that vendor.
func ApplyResponseTransforms(resp map[string]interface{}, providerURL, model string) map[string]interface{} {
	host, _ := SplitProviderHostPath(providerURL)
	switch host {
	case "api.deepseek.com":
		return applyDeepSeekResponseTransform(resp, providerURL, model)
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
