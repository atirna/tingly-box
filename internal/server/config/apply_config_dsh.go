package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	yamlpkg "gopkg.in/yaml.v3"
)

// DeepSeek Harness (dsh) config/credentials application: render and merge the
// tingly-managed provider stanza of $DSH_HOME/settings.yaml, and the
// tingly-managed key in $DSH_HOME/.credentials.yaml.
//
// dsh splits its config the same way Codex does: a settings file that
// references credentials indirectly (apiKeyEnv names an env var rather than
// embedding the secret) and a separate write-only credentials file. See
// https://deepseek-harness.github.io/deepseek-harness/en/guide/providers.

// dshGatewayProviderName is the tingly-box provider key written into
// settings.yaml's `llm-pi-ai.providers` map by mergeDshSettings.
const dshGatewayProviderName = "tingly-box"

// dshAPIKeyEnvName is the env var name settings.yaml's apiKeyEnv points at,
// and the key written into .credentials.yaml. Fixed rather than
// user-configurable: it's an implementation detail of dsh's credential
// indirection, not a user-tunable knob.
const dshAPIKeyEnvName = "TINGLY_BOX_API_KEY"

// dshHomeDir resolves $DSH_HOME, falling back to ~/.dsh when unset (dsh's
// docs do not state a default; ~/.dsh follows the common single-dotdir
// convention used by similar CLIs).
func dshHomeDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("DSH_HOME")); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	if home == "" {
		return "", fmt.Errorf("home directory resolved to empty path")
	}
	return filepath.Join(home, ".dsh"), nil
}

// DshPrefs is the typed, user-tunable surface of settings.yaml's tingly-box
// provider entry. DefaultInput mirrors the provider-level `defaultInput` key
// ("text" or "text_image" -> [text] / [text, image]); empty omits the key so
// dsh treats the provider as text-only — the conservative default for models
// tingly-box cannot verify support vision (same stance as Codex's third-party
// defaults, see .design/codex-config.md).
type DshPrefs struct {
	DefaultInput string `json:"default_input,omitempty"`
}

// DefaultDshPrefs returns the defaults for the CLI path and no-prefs
// fallback: DefaultInput unset (text-only).
func DefaultDshPrefs() *DshPrefs {
	return &DshPrefs{}
}

// dshDefaultInputList converts the enum value into the YAML modality list,
// or nil for an unset/invalid value (dropped, forward-compatible).
func dshDefaultInputList(val string) []string {
	switch val {
	case "text":
		return []string{"text"}
	case "text_image":
		return []string{"text", "image"}
	default:
		return nil
	}
}

// toConfig converts prefs into the provider-stanza fields it controls.
func (p *DshPrefs) toConfig() map[string]interface{} {
	out := map[string]interface{}{}
	if p == nil {
		return out
	}
	if list := dshDefaultInputList(strings.TrimSpace(p.DefaultInput)); list != nil {
		out["defaultInput"] = list
	}
	return out
}

// DshPrefsFromConfig is the inverse of toConfig: it extracts DefaultInput
// from a parsed tingly-box provider stanza.
func DshPrefsFromConfig(providerStanza map[string]interface{}) *DshPrefs {
	prefs := &DshPrefs{}
	list, ok := providerStanza["defaultInput"].([]interface{})
	if !ok || len(list) == 0 {
		return prefs
	}
	hasImage := false
	for _, v := range list {
		if s, ok := v.(string); ok && s == "image" {
			hasImage = true
		}
	}
	if hasImage {
		prefs.DefaultInput = "text_image"
	} else {
		prefs.DefaultInput = "text"
	}
	return prefs
}

// dshProviderStanza drills into cfg["llm-pi-ai"]["providers"]["tingly-box"]
// and returns it if present.
func dshProviderStanza(cfg map[string]interface{}) (map[string]interface{}, bool) {
	root, ok := cfg["llm-pi-ai"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	providers, ok := root["providers"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	stanza, ok := providers[dshGatewayProviderName].(map[string]interface{})
	if !ok {
		return nil, false
	}
	return stanza, true
}

// ReadDshSettings reads $DSH_HOME/settings.yaml and returns the typed prefs
// and whether a tingly-managed provider stanza exists. A missing or
// unparseable file yields empty prefs, exists=false, so the form falls back
// to defaults rather than erroring on first-time setup.
func ReadDshSettings() (prefs *DshPrefs, exists bool, err error) {
	dshHome, err := dshHomeDir()
	if err != nil {
		return nil, false, err
	}
	targetPath := filepath.Join(dshHome, "settings.yaml")

	data, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		return DefaultDshPrefs(), false, nil
	}

	cfg := map[string]interface{}{}
	if err := yamlpkg.Unmarshal(data, &cfg); err != nil {
		return DefaultDshPrefs(), false, nil
	}

	stanza, ok := dshProviderStanza(cfg)
	if !ok {
		return DefaultDshPrefs(), false, nil
	}
	return DshPrefsFromConfig(stanza), true, nil
}

// ApplyDshSettings merges tingly-box's provider entry into
// $DSH_HOME/settings.yaml.
//
// MERGE semantics: only the `llm-pi-ai.providers.tingly-box` stanza is
// overwritten; everything else the user has in settings.yaml — other
// providers, unrelated top-level keys — is left alone. The previous file (if
// any) is backed up before rewrite.
func ApplyDshSettings(baseURL string, models []string, prefs *DshPrefs) (*ApplyResult, error) {
	dshHome, err := dshHomeDir()
	if err != nil {
		return nil, err
	}
	targetPath := filepath.Join(dshHome, "settings.yaml")
	result := &ApplyResult{}

	if err := os.MkdirAll(dshHome, 0755); err != nil {
		result.Message = fmt.Sprintf("Failed to create directory: %v", err)
		return result, nil
	}

	existing := map[string]interface{}{}
	if data, err := os.ReadFile(targetPath); err == nil {
		if err := yamlpkg.Unmarshal(data, &existing); err != nil {
			result.Message = fmt.Sprintf("Failed to parse existing settings.yaml: %v", err)
			return result, nil
		}
		backupPath, err := backupFile(targetPath)
		if err != nil {
			result.Message = fmt.Sprintf("Failed to create backup: %v", err)
			return result, nil
		}
		result.BackupPath = backupPath
		result.Updated = true
	} else {
		result.Created = true
	}

	mergeDshSettings(existing, baseURL, models, prefs)

	out, err := yamlpkg.Marshal(existing)
	if err != nil {
		result.Message = fmt.Sprintf("Failed to marshal settings.yaml: %v", err)
		return result, nil
	}
	if err := os.WriteFile(targetPath, out, 0644); err != nil {
		result.Message = fmt.Sprintf("Failed to write file: %v", err)
		return result, nil
	}

	result.Success = true
	if result.Created {
		result.Message = fmt.Sprintf("Created %s", targetPath)
	} else if result.BackupPath != "" {
		result.Message = fmt.Sprintf("Updated %s (backup: %s)", targetPath, result.BackupPath)
	} else {
		result.Message = fmt.Sprintf("Updated %s", targetPath)
	}
	return result, nil
}

// RenderDshSettingsYAML returns the YAML that would be written to a fresh
// settings.yaml — i.e. the merge applied to an empty starting point. Used by
// the preview endpoint so the UI can show exactly what's pending.
func RenderDshSettingsYAML(baseURL string, models []string, prefs *DshPrefs) ([]byte, error) {
	cfg := map[string]interface{}{}
	mergeDshSettings(cfg, baseURL, models, prefs)
	return yamlpkg.Marshal(cfg)
}

// mergeDshSettings mutates cfg in place, applying the tingly-box provider
// stanza while preserving everything else. See ApplyDshSettings for the
// contract.
func mergeDshSettings(cfg map[string]interface{}, baseURL string, models []string, prefs *DshPrefs) {
	root, _ := cfg["llm-pi-ai"].(map[string]interface{})
	if root == nil {
		root = map[string]interface{}{}
	}
	providers, _ := root["providers"].(map[string]interface{})
	if providers == nil {
		providers = map[string]interface{}{}
	}

	modelEntries := make([]map[string]interface{}, 0, len(models))
	for _, model := range models {
		modelEntries = append(modelEntries, map[string]interface{}{"id": model})
	}

	stanza := map[string]interface{}{
		"apiKeyEnv": dshAPIKeyEnvName,
		"api":       "openai-completions",
		"baseURL":   baseURL,
		"models":    modelEntries,
	}
	for k, v := range prefs.toConfig() {
		stanza[k] = v
	}

	providers[dshGatewayProviderName] = stanza
	root["providers"] = providers
	cfg["llm-pi-ai"] = root
}

// RenderDshCredentialsYAML returns the YAML that would be written to a fresh
// .credentials.yaml — i.e. just the tingly-managed key. Used by the preview
// endpoint; the real ApplyDshCredentials merges into any existing file.
func RenderDshCredentialsYAML(apiKey string) ([]byte, error) {
	return yamlpkg.Marshal(map[string]string{dshAPIKeyEnvName: apiKey})
}

// ApplyDshCredentials writes $DSH_HOME/.credentials.yaml, setting only the
// tingly-box managed env var key; other keys already in the file (other
// providers' credentials) are preserved. The previous file (if any) is
// backed up before modification.
func ApplyDshCredentials(apiKey string) (*ApplyResult, error) {
	dshHome, err := dshHomeDir()
	if err != nil {
		return nil, err
	}
	targetPath := filepath.Join(dshHome, ".credentials.yaml")
	result := &ApplyResult{}

	if err := os.MkdirAll(dshHome, 0755); err != nil {
		result.Message = fmt.Sprintf("Failed to create directory: %v", err)
		return result, nil
	}

	existing := map[string]interface{}{}
	if data, err := os.ReadFile(targetPath); err == nil {
		if err := yamlpkg.Unmarshal(data, &existing); err != nil {
			result.Message = fmt.Sprintf("Failed to parse existing .credentials.yaml: %v", err)
			return result, nil
		}
		backupPath, err := backupFile(targetPath)
		if err != nil {
			result.Message = fmt.Sprintf("Failed to create backup: %v", err)
			return result, nil
		}
		result.BackupPath = backupPath
		result.Updated = true
	} else {
		result.Created = true
	}

	existing[dshAPIKeyEnvName] = apiKey

	out, err := yamlpkg.Marshal(existing)
	if err != nil {
		result.Message = fmt.Sprintf("Failed to marshal .credentials.yaml: %v", err)
		return result, nil
	}
	if err := os.WriteFile(targetPath, out, 0600); err != nil {
		result.Message = fmt.Sprintf("Failed to write file: %v", err)
		return result, nil
	}

	result.Success = true
	if result.Created {
		result.Message = fmt.Sprintf("Created %s", targetPath)
	} else if result.BackupPath != "" {
		result.Message = fmt.Sprintf("Updated %s (backup: %s)", targetPath, result.BackupPath)
	} else {
		result.Message = fmt.Sprintf("Updated %s", targetPath)
	}
	return result, nil
}
