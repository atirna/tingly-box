package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDshConfig_Apply(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)

	cfg := &DshConfig{}
	result, err := cfg.Apply(&DshParams{
		DshBaseURL: "https://tingly.local/tingly/dsh",
		APIKey:     "sk-test",
		Models:     []string{"tingly-dsh"},
		Prefs:      DefaultDshPrefs(),
	})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}
	if len(result.ConfigFiles) != 2 {
		t.Errorf("expected settings.yaml + .credentials.yaml to both be written, got %v", result.ConfigFiles)
	}

	settingsData, err := os.ReadFile(filepath.Join(dshHome, "settings.yaml"))
	if err != nil {
		t.Fatalf("expected settings.yaml to be written: %v", err)
	}
	settings := string(settingsData)
	if !strings.Contains(settings, "tingly.local") {
		t.Errorf("expected settings.yaml to reference the base URL, got %s", settings)
	}
	if !strings.Contains(settings, "tingly-dsh") {
		t.Errorf("expected settings.yaml to reference the model, got %s", settings)
	}
	if !strings.Contains(settings, dshAPIKeyEnvName) {
		t.Errorf("expected settings.yaml to reference apiKeyEnv, got %s", settings)
	}
	if strings.Contains(settings, "sk-test") {
		t.Errorf("expected settings.yaml NOT to contain the literal API key, got %s", settings)
	}

	credsData, err := os.ReadFile(filepath.Join(dshHome, ".credentials.yaml"))
	if err != nil {
		t.Fatalf("expected .credentials.yaml to be written: %v", err)
	}
	if !strings.Contains(string(credsData), "sk-test") {
		t.Errorf("expected .credentials.yaml to contain the API key, got %s", credsData)
	}
}

func TestDshConfig_Apply_PreservesUnrelatedSettings(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)

	if err := os.WriteFile(filepath.Join(dshHome, "settings.yaml"), []byte(
		"llm-pi-ai:\n  providers:\n    other-gateway:\n      apiKeyEnv: OTHER_KEY\n      api: openai-completions\n      baseURL: https://other.example/v1\n      models:\n        - id: other-model\n"),
		0644); err != nil {
		t.Fatalf("failed to seed settings.yaml: %v", err)
	}

	cfg := &DshConfig{}
	result, err := cfg.Apply(&DshParams{
		DshBaseURL: "https://tingly.local/tingly/dsh",
		APIKey:     "sk-test",
		Models:     []string{"tingly-dsh"},
		Prefs:      DefaultDshPrefs(),
	})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}

	settingsData, err := os.ReadFile(filepath.Join(dshHome, "settings.yaml"))
	if err != nil {
		t.Fatalf("expected settings.yaml to be written: %v", err)
	}
	settings := string(settingsData)
	if !strings.Contains(settings, "other-gateway") {
		t.Errorf("expected existing other-gateway provider to be preserved, got %s", settings)
	}
	if !strings.Contains(settings, dshGatewayProviderName) {
		t.Errorf("expected tingly-box provider to be written, got %s", settings)
	}
}

func TestDshConfig_Apply_InvalidParams(t *testing.T) {
	cfg := &DshConfig{}
	if _, err := cfg.Apply("not-the-right-type"); err == nil {
		t.Fatal("expected error for invalid params type")
	}
}

func TestDshConfig_Restore_NoBackupFails(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)

	cfg := &DshConfig{}
	result, err := cfg.Restore()
	if err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}
	if result.Success {
		t.Fatalf("expected restore to fail with no backups, got %+v", result)
	}
}
