package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// withTempHome points os.UserHomeDir() (via $HOME) at a fresh temp dir with
// a .claude directory already created, restoring the original HOME on
// cleanup. Mirrors the pattern used by TestApplyClaudeSettings_NewFile.
func withTempHome(t *testing.T) (home, claudeDir string) {
	t.Helper()
	home = t.TempDir()
	original := os.Getenv("HOME")
	os.Setenv("HOME", home)
	t.Cleanup(func() { os.Setenv("HOME", original) })

	claudeDir = filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("failed to create .claude dir: %v", err)
	}
	return home, claudeDir
}

func readSettings(t *testing.T, claudeDir string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("unmarshal settings.json: %v", err)
	}
	return settings
}

func TestMergeHookToken(t *testing.T) {
	t.Run("empty token is a no-op", func(t *testing.T) {
		cfg := map[string]interface{}{"env": map[string]interface{}{"FOO": "bar"}}
		mergeHookToken(cfg, "")
		env := cfg["env"].(map[string]interface{})
		if _, ok := env[HookTokenEnvVar]; ok {
			t.Fatalf("expected no %s written for empty token, env = %v", HookTokenEnvVar, env)
		}
		if env["FOO"] != "bar" {
			t.Fatalf("expected existing env entries preserved, got %v", env)
		}
	})

	t.Run("writes token into a fresh env block", func(t *testing.T) {
		cfg := map[string]interface{}{}
		mergeHookToken(cfg, "tb-user-abc123")
		env, ok := cfg["env"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected env block to be created, got %v", cfg)
		}
		if env[HookTokenEnvVar] != "tb-user-abc123" {
			t.Fatalf("token = %v, want tb-user-abc123", env[HookTokenEnvVar])
		}
	})

	t.Run("preserves unrelated env entries", func(t *testing.T) {
		cfg := map[string]interface{}{"env": map[string]interface{}{"ANTHROPIC_MODEL": "tingly/cc"}}
		mergeHookToken(cfg, "tb-user-abc123")
		env := cfg["env"].(map[string]interface{})
		if env["ANTHROPIC_MODEL"] != "tingly/cc" {
			t.Fatalf("expected ANTHROPIC_MODEL preserved, got %v", env)
		}
		if env[HookTokenEnvVar] != "tb-user-abc123" {
			t.Fatalf("token = %v, want tb-user-abc123", env[HookTokenEnvVar])
		}
	})
}

func TestApplyNotifyHooks(t *testing.T) {
	_, claudeDir := withTempHome(t)

	result, err := ApplyNotifyHooks("tb-user-abc123")
	if err != nil {
		t.Fatalf("ApplyNotifyHooks failed: %v", err)
	}
	if !result.Success || !result.Created {
		t.Fatalf("expected a successful new-file result, got %+v", result)
	}

	if _, err := os.Stat(filepath.Join(claudeDir, "tingly-notify.sh")); err != nil {
		t.Fatalf("expected notify script installed: %v", err)
	}

	settings := readSettings(t, claudeDir)
	env, ok := settings["env"].(map[string]interface{})
	if !ok || env[HookTokenEnvVar] != "tb-user-abc123" {
		t.Fatalf("expected %s in settings env, got %v", HookTokenEnvVar, settings["env"])
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected hooks block, got %v", settings)
	}
	if _, ok := hooks["Stop"]; !ok {
		t.Fatalf("expected Stop hook entry, got %v", hooks)
	}
}

func TestApplyImHooks(t *testing.T) {
	_, claudeDir := withTempHome(t)

	result, err := ApplyImHooks("tb-user-abc123")
	if err != nil {
		t.Fatalf("ApplyImHooks failed: %v", err)
	}
	if !result.Success || !result.Created {
		t.Fatalf("expected a successful new-file result, got %+v", result)
	}

	settings := readSettings(t, claudeDir)
	env, ok := settings["env"].(map[string]interface{})
	if !ok || env[HookTokenEnvVar] != "tb-user-abc123" {
		t.Fatalf("expected %s in settings env, got %v", HookTokenEnvVar, settings["env"])
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected hooks block, got %v", settings)
	}
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Fatalf("expected PreToolUse hook entry, got %v", hooks)
	}
}

// TestApplyNotifyThenImHooks verifies the two independent install flows
// compose: running both against the same settings.json keeps both hook sets
// and the token survives both merges.
func TestApplyNotifyThenImHooks(t *testing.T) {
	_, claudeDir := withTempHome(t)

	if _, err := ApplyNotifyHooks("tb-user-abc123"); err != nil {
		t.Fatalf("ApplyNotifyHooks failed: %v", err)
	}
	if _, err := ApplyImHooks("tb-user-abc123"); err != nil {
		t.Fatalf("ApplyImHooks failed: %v", err)
	}

	settings := readSettings(t, claudeDir)
	hooks := settings["hooks"].(map[string]interface{})
	if _, ok := hooks["Stop"]; !ok {
		t.Fatalf("expected notify's Stop hook to survive the im-hook merge, got %v", hooks)
	}
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Fatalf("expected im-hook's PreToolUse hook, got %v", hooks)
	}
	env := settings["env"].(map[string]interface{})
	if env[HookTokenEnvVar] != "tb-user-abc123" {
		t.Fatalf("token = %v, want tb-user-abc123", env[HookTokenEnvVar])
	}
}

func TestApplyNotifyHooks_EmptyTokenLeavesEnvUntouched(t *testing.T) {
	_, claudeDir := withTempHome(t)

	if _, err := ApplyNotifyHooks(""); err != nil {
		t.Fatalf("ApplyNotifyHooks failed: %v", err)
	}

	settings := readSettings(t, claudeDir)
	if env, ok := settings["env"]; ok {
		if m, ok := env.(map[string]interface{}); ok {
			if _, has := m[HookTokenEnvVar]; has {
				t.Fatalf("expected no %s written for empty token, env = %v", HookTokenEnvVar, m)
			}
		}
	}
}
