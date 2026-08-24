package info

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")

	// No shortcut artifacts in the fresh home: the relaunch must not carry
	// --shortcut (updating never creates shortcuts the user never asked for).
	spec := updateLaunchSpec("npx", "9.9.9", "Tingly Box")
	cmd := strings.Join(spec.Argv, " ")
	if !strings.Contains(cmd, "npx -y tingly-box@9.9.9 restart --daemon") {
		t.Errorf("relaunch not pinned to target version:\n%s", cmd)
	}
	if strings.Contains(cmd, "--shortcut") {
		t.Errorf("no artifacts exist, relaunch must not pass --shortcut:\n%s", cmd)
	}

	bundle := updateLaunchSpec("npx-bundle", "9.9.9", "Tingly Box")
	if !strings.Contains(strings.Join(bundle.Argv, " "), "tingly-box-bundle@9.9.9") {
		t.Errorf("bundle source should relaunch the bundle package: %v", bundle.Argv)
	}

	// With an existing artifact the relaunch repins it via --shortcut.
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "tingly-box.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	repin := updateLaunchSpec("npx", "9.9.9", "Tingly Box")
	if !strings.Contains(strings.Join(repin.Argv, " "), "restart --daemon --shortcut") {
		t.Errorf("existing artifacts should be repinned via --shortcut: %v", repin.Argv)
	}
}
