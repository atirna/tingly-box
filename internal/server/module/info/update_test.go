package info

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setTestHomeDir isolates os.UserHomeDir() to dir for the test. HOME is what
// os.UserHomeDir reads on Unix and macOS; on Windows it reads USERPROFILE
// instead (HOME is ignored there), so both must be set or a Windows test run
// falls through to the real user profile — reading/writing outside the
// sandboxed dir the test thinks it's using.
func setTestHomeDir(t *testing.T, dir string) string {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

func TestCanOneClickUpdate(t *testing.T) {
	for source, want := range map[string]bool{
		"npx":        true,
		"npx-bundle": true,
		"":           false,
		"binary":     false,
	} {
		if got := CanOneClickUpdate(source); got != want {
			t.Errorf("CanOneClickUpdate(%q) = %v, want %v", source, got, want)
		}
	}
}

func TestUpdateLaunchSpecPinsTargetVersion(t *testing.T) {
	home := setTestHomeDir(t, t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")

	// No shortcut artifacts in the fresh home: the relaunch must not carry
	// --shortcut (updating never creates shortcuts the user never asked for).
	spec := updateLaunchSpec("npx", "9.9.9", "", "Tingly Box")
	cmd := strings.Join(spec.Argv, " ")
	if !strings.Contains(cmd, "npx -y tingly-box@9.9.9 restart --daemon --browser=false") {
		t.Errorf("relaunch not pinned to target version:\n%s", cmd)
	}
	if strings.Contains(cmd, "--shortcut") {
		t.Errorf("no artifacts exist, relaunch must not pass --shortcut:\n%s", cmd)
	}

	bundle := updateLaunchSpec("npx-bundle", "9.9.9", "", "Tingly Box")
	if !strings.Contains(strings.Join(bundle.Argv, " "), "tingly-box-bundle@9.9.9") {
		t.Errorf("bundle source should relaunch the bundle package: %v", bundle.Argv)
	}

	// An explicit --host must survive the relaunch — a bare `restart` would
	// otherwise reset it to the default and widen network exposure.
	hosted := updateLaunchSpec("npx", "9.9.9", "127.0.0.1", "Tingly Box")
	if !strings.Contains(strings.Join(hosted.Argv, " "), "restart --daemon --browser=false --host 127.0.0.1") {
		t.Errorf("relaunch must preserve an explicit host: %v", hosted.Argv)
	}

	// With an existing artifact the relaunch repins it via --shortcut.
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "tingly-box.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	repin := updateLaunchSpec("npx", "9.9.9", "", "Tingly Box")
	if !strings.Contains(strings.Join(repin.Argv, " "), "--shortcut") {
		t.Errorf("existing artifacts should be repinned via --shortcut: %v", repin.Argv)
	}
}
