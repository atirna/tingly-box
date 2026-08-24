package daemon

import (
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestBuildDaemonArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		override []string
		want     []string
	}{
		{
			name:     "no override passes through unchanged",
			args:     []string{"restart", "--daemon"},
			override: nil,
			want:     []string{"restart", "--daemon"},
		},
		{
			name:     "appends pinned port (restart preserve case)",
			args:     []string{"restart", "--daemon"},
			override: []string{"--port", "9000"},
			want:     []string{"restart", "--daemon", "--port", "9000"},
		},
		{
			// An earlier --port is left in place; the CLI parser takes the last
			// occurrence, so the appended value wins without stripping.
			name:     "appends after an existing --port (last wins)",
			args:     []string{"start", "--port", "8080", "--daemon"},
			override: []string{"--port", "9000"},
			want:     []string{"start", "--port", "8080", "--daemon", "--port", "9000"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDaemonArgs(tt.args, tt.override)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildDaemonArgs(%v, %v) = %v, want %v", tt.args, tt.override, got, tt.want)
			}
		})
	}
}

// TestDetachAttrsScrubsDaemonMarker guards the specific bug the self-update
// path hit: a server started via `restart --daemon` runs with
// _TINGLY_BOX_DAEMON=1 in its own environment (set by Daemonize below). Any
// command it spawns via DetachAttrs — e.g. self-update's relaunch — must not
// inherit that marker, or the spawned command's own `--daemon` would see
// IsDaemonProcess() == true and skip Daemonize's re-exec entirely.
func TestDetachAttrsScrubsDaemonMarker(t *testing.T) {
	t.Setenv("_TINGLY_BOX_DAEMON", "1")
	t.Setenv("SOME_OTHER_VAR", "kept")

	cmd := exec.Command("true")
	DetachAttrs(cmd)

	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "_TINGLY_BOX_DAEMON=") {
			t.Fatalf("DetachAttrs must scrub the inherited daemon marker, got env entry %q", kv)
		}
	}
	if !slices.Contains(cmd.Env, "SOME_OTHER_VAR=kept") {
		t.Fatalf("DetachAttrs must preserve other environment variables, got %v", cmd.Env)
	}
}
