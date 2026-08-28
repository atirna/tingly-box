package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const versionFileName = "tingly-server.version"

// VersionFile records the build version of the running server. Like the port
// file, it is a runtime artifact in the config directory, not configuration:
// the server writes it right after acquiring the file lock and removes it on
// shutdown. Launcher processes (bare `tingly-box` from the npm shims, `start`)
// read it to decide whether the running server matches their own version —
// if it does, there is nothing to update and the launcher must not restart it.
//
// Readers must gate on FileLock.IsLocked(), same as the port file. A missing
// or unreadable file means "version unknown" (e.g. a server started by a
// build predating version recording) and callers should treat that the same
// as a version mismatch.
type VersionFile struct {
	path string
}

// NewVersionFile creates a version file handle for the given config directory.
func NewVersionFile(configDir string) *VersionFile {
	return &VersionFile{path: filepath.Join(configDir, versionFileName)}
}

// Write records the running server's version. The write is atomic (temp file
// + rename) so a concurrent reader never observes a partial value.
func (vf *VersionFile) Write(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("empty version")
	}
	tmp := vf.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(version+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to write version file: %w", err)
	}
	if err := os.Rename(tmp, vf.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to publish version file: %w", err)
	}
	return nil
}

// Read returns the recorded version. Callers should treat any error as
// "version unknown".
func (vf *VersionFile) Read() (string, error) {
	data, err := os.ReadFile(vf.path)
	if err != nil {
		return "", fmt.Errorf("failed to read version file: %w", err)
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return "", fmt.Errorf("empty version in %s", vf.path)
	}
	return s, nil
}

// Remove deletes the version file. A missing file is not an error.
func (vf *VersionFile) Remove() error {
	if err := os.Remove(vf.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove version file: %w", err)
	}
	return nil
}
