package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yamlpkg "gopkg.in/yaml.v3"
)

// ============================================================================
// ApplyDshSettings / ApplyDshCredentials tests
//
// Contract: writing $DSH_HOME/settings.yaml and .credentials.yaml must MERGE —
// only the tingly-box provider stanza / managed key is overwritten. Unrelated
// providers, top-level keys, and credential entries must survive.
// ============================================================================

func loadDshYAMLForTest(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	cfg := map[string]interface{}{}
	require.NoError(t, yamlpkg.Unmarshal(data, &cfg))
	return cfg
}

func TestApplyDshSettings_CreatesFreshFile(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)

	result, err := ApplyDshSettings("https://tingly.local/tingly/dsh", []string{"tingly-dsh"}, DefaultDshPrefs())
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.True(t, result.Created)
	assert.Empty(t, result.BackupPath)

	cfg := loadDshYAMLForTest(t, filepath.Join(dshHome, "settings.yaml"))
	stanza, ok := dshProviderStanza(cfg)
	require.True(t, ok, "expected llm-pi-ai.providers.tingly-box stanza")
	assert.Equal(t, "https://tingly.local/tingly/dsh", stanza["baseURL"])
	assert.Equal(t, dshAPIKeyEnvName, stanza["apiKeyEnv"])
	assert.Equal(t, "openai-completions", stanza["api"])
	assert.NotContains(t, stanza, "defaultInput", "unset prefs must not write defaultInput")
}

func TestApplyDshSettings_PreservesUnrelatedProvidersAndKeys(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)

	seed := "otherTopLevelKey: keepme\n" +
		"llm-pi-ai:\n" +
		"  providers:\n" +
		"    other-gateway:\n" +
		"      apiKeyEnv: OTHER_KEY\n" +
		"      api: openai-completions\n" +
		"      baseURL: https://other.example/v1\n" +
		"      models:\n" +
		"        - id: other-model\n"
	require.NoError(t, os.WriteFile(filepath.Join(dshHome, "settings.yaml"), []byte(seed), 0644))

	result, err := ApplyDshSettings("https://tingly.local/tingly/dsh", []string{"tingly-dsh"}, DefaultDshPrefs())
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.True(t, result.Updated)
	assert.NotEmpty(t, result.BackupPath)

	cfg := loadDshYAMLForTest(t, filepath.Join(dshHome, "settings.yaml"))
	assert.Equal(t, "keepme", cfg["otherTopLevelKey"])

	root := cfg["llm-pi-ai"].(map[string]interface{})
	providers := root["providers"].(map[string]interface{})
	other, ok := providers["other-gateway"].(map[string]interface{})
	require.True(t, ok, "expected other-gateway provider to survive the merge")
	assert.Equal(t, "https://other.example/v1", other["baseURL"])

	_, ok = providers[dshGatewayProviderName]
	assert.True(t, ok, "expected tingly-box provider stanza to be written")
}

func TestApplyDshSettings_DefaultInputEnum(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)

	result, err := ApplyDshSettings("https://tingly.local/tingly/dsh", []string{"vision-model"}, &DshPrefs{DefaultInput: "text_image"})
	require.NoError(t, err)
	assert.True(t, result.Success)

	cfg := loadDshYAMLForTest(t, filepath.Join(dshHome, "settings.yaml"))
	stanza, ok := dshProviderStanza(cfg)
	require.True(t, ok)
	assert.Equal(t, []interface{}{"text", "image"}, stanza["defaultInput"])
}

func TestReadDshSettings_RoundTrip(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)

	// No file yet — not tingly-managed, defaults returned.
	prefs, exists, err := ReadDshSettings()
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Equal(t, DefaultDshPrefs(), prefs)

	_, err = ApplyDshSettings("https://tingly.local/tingly/dsh", []string{"m1"}, &DshPrefs{DefaultInput: "text_image"})
	require.NoError(t, err)

	prefs, exists, err = ReadDshSettings()
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, "text_image", prefs.DefaultInput)
}

func TestApplyDshCredentials_PreservesOtherKeys(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)

	seed := "OTHER_KEY: other-secret\n"
	require.NoError(t, os.WriteFile(filepath.Join(dshHome, ".credentials.yaml"), []byte(seed), 0600))

	result, err := ApplyDshCredentials("sk-test")
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.True(t, result.Updated)
	assert.NotEmpty(t, result.BackupPath)

	cfg := loadDshYAMLForTest(t, filepath.Join(dshHome, ".credentials.yaml"))
	assert.Equal(t, "other-secret", cfg["OTHER_KEY"])
	assert.Equal(t, "sk-test", cfg[dshAPIKeyEnvName])

	info, err := os.Stat(filepath.Join(dshHome, ".credentials.yaml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestRenderDshSettingsYAML_MatchesApply(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)

	rendered, err := RenderDshSettingsYAML("https://tingly.local/tingly/dsh", []string{"tingly-dsh"}, DefaultDshPrefs())
	require.NoError(t, err)

	_, err = ApplyDshSettings("https://tingly.local/tingly/dsh", []string{"tingly-dsh"}, DefaultDshPrefs())
	require.NoError(t, err)
	written, err := os.ReadFile(filepath.Join(dshHome, "settings.yaml"))
	require.NoError(t, err)

	assert.YAMLEq(t, string(rendered), string(written))
}
