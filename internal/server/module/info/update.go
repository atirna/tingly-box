package info

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tingly-dev/tingly-box/internal/shortcut"
	"github.com/tingly-dev/tingly-box/pkg/daemon"
)

// CanOneClickUpdate reports whether the given launch source supports the
// one-click self-update path. Only the npx shapes do: for them "updating" is
// just relaunching with a newer version spec — npm downloads it, the new
// process restarts the daemon, nothing self-modifies. A plain binary install
// (Homebrew, manual download) must be updated the way it was installed, which
// this server can't safely do on the user's behalf yet.
func CanOneClickUpdate(launchSource string) bool {
	return launchSource == shortcut.SourceNpx || launchSource == shortcut.SourceNpxBundle
}

// updateLaunchSpec builds the relaunch command for a one-click update:
// the same command a shortcut would run, pinned to targetVersion instead of
// the running version, plus:
//   - --browser=false: the page that clicked "update" reloads itself, so the
//     relaunch opening another tab would just duplicate it;
//   - --host <host> when the server was bound to an explicit host, so an
//     update never silently widens network exposure (a bare restart would
//     reset --host to the default). The port needs no passthrough — restart
//     preserves the running port via the runtime port file;
//   - --shortcut iff the user already has shortcut artifacts, so the update
//     repins them to the new version without ever creating shortcuts for a
//     user who never asked for any.
func updateLaunchSpec(launchSource, targetVersion, host, shortcutName string) shortcut.LaunchSpec {
	args := append(shortcut.LaunchArgs(), "--browser=false")
	if host != "" {
		args = append(args, "--host", host)
	}
	if shortcut.AnyExists(shortcutName) {
		args = append(args, "--shortcut")
	}
	// exePath is irrelevant for npx sources (the spec runs npx, not the
	// binary), but resolve it anyway so the spec is well-formed.
	exePath, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	return shortcut.ResolveLaunchWith(exePath, launchSource, targetVersion, args)
}

// spawnDetached starts the relaunch command fully detached from this process,
// which the command will stop (it runs `restart`). Returns the human-readable
// command line for display.
func spawnDetached(spec shortcut.LaunchSpec) (string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// spec.WinArgs is "/c npx -y <pkg>@<version> restart --daemon ..." —
		// every token is space-free (package specs, flags), so Fields is a
		// faithful split here.
		cmd = exec.Command(spec.WinTarget, strings.Fields(spec.WinArgs)...)
	} else {
		cmd = exec.Command(spec.Argv[0], spec.Argv[1:]...)
	}
	cmd.Dir = spec.WorkDir
	daemon.DetachAttrs(cmd)

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start update command: %w", err)
	}
	// Not waited on: the child outlives (and replaces) this process.
	display := strings.Join(spec.Argv, " ")
	if runtime.GOOS == "windows" {
		display = spec.WinTarget + " " + spec.WinArgs
	}
	return display, nil
}
