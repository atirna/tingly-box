package command

import (
	"os"
	"testing"

	"github.com/tingly-dev/tingly-box/pkg/lock"
)

// TestServerManagerStopWithoutStart tests stopping a server that was never started
func TestServerManagerStopWithoutStart(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test-no-start-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	appManager, err := NewAppManager(tempDir)
	if err != nil {
		t.Fatalf("Failed to create app manager: %v", err)
	}

	serverManager := NewServerManager(appManager.AppConfig())

	// Stop without starting should not fail
	err = serverManager.Stop()
	if err != nil {
		t.Errorf("Stop without start should not fail, got: %v", err)
	}
}

// TestFileLockIntegration tests that file locks work correctly with server lifecycle
func TestFileLockIntegration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test-filelock-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	_, err = NewAppManager(tempDir)
	if err != nil {
		t.Fatalf("Failed to create app manager: %v", err)
	}

	fileLock := lock.NewFileLock(tempDir)

	t.Run("File lock not acquired initially", func(t *testing.T) {
		if fileLock.IsLocked() {
			t.Error("File lock should not be locked initially")
		}
	})

	t.Run("Acquire and release file lock", func(t *testing.T) {
		err := fileLock.TryLock()
		if err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}

		if !fileLock.IsLocked() {
			t.Error("File lock should be locked after TryLock")
		}

		err = fileLock.Unlock()
		if err != nil {
			t.Fatalf("Failed to release lock: %v", err)
		}

		if fileLock.IsLocked() {
			t.Error("File lock should not be locked after Unlock")
		}
	})

	t.Run("Acquire lock twice fails", func(t *testing.T) {
		err := fileLock.TryLock()
		if err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}
		defer fileLock.Unlock()

		// Try to acquire again - should fail
		err = fileLock.TryLock()
		if err == nil {
			t.Error("Expected error when acquiring lock twice")
		}
	})
}

// TestServerPortConfiguration tests port configuration persistence
func TestServerPortConfiguration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test-port-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	t.Run("Set and get server port", func(t *testing.T) {
		appManager, err := NewAppManager(tempDir)
		if err != nil {
			t.Fatalf("Failed to create app manager: %v", err)
		}

		testPort := 12580
		err = appManager.AppConfig().SetServerPort(testPort)
		if err != nil {
			t.Fatalf("Failed to set server port: %v", err)
		}

		if appManager.GetGlobalConfig().GetServerPort() != testPort {
			t.Errorf("Expected port %d, got %d", testPort, appManager.GetGlobalConfig().GetServerPort())
		}
	})

	t.Run("Runtime port prefers port file while server is running", func(t *testing.T) {
		appManager, err := NewAppManager(tempDir)
		if err != nil {
			t.Fatalf("Failed to create app manager: %v", err)
		}

		configPort := appManager.GetGlobalConfig().GetServerPort()

		// No server running: falls back to the configured port even if a
		// stale port file exists.
		portFile := lock.NewPortFile(tempDir)
		if err := portFile.Write(23456); err != nil {
			t.Fatalf("Failed to write port file: %v", err)
		}
		if got := appManager.GetRuntimeServerPort(); got != configPort {
			t.Errorf("Expected configured port %d when server is not running, got %d", configPort, got)
		}

		// Server running (lock held): the port file wins.
		fileLock := lock.NewFileLock(tempDir)
		if err := fileLock.TryLock(); err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}
		defer fileLock.Unlock()

		if got := appManager.GetRuntimeServerPort(); got != 23456 {
			t.Errorf("Expected runtime port 23456, got %d", got)
		}

		// Port file gone while running: falls back to configured port.
		if err := portFile.Remove(); err != nil {
			t.Fatalf("Failed to remove port file: %v", err)
		}
		if got := appManager.GetRuntimeServerPort(); got != configPort {
			t.Errorf("Expected configured port %d after port file removal, got %d", configPort, got)
		}
	})

	t.Run("Port persists when explicitly saved", func(t *testing.T) {
		// First instance: set port
		appManager1, err := NewAppManager(tempDir)
		if err != nil {
			t.Fatalf("Failed to create first app manager: %v", err)
		}

		testPort := 12582
		err = appManager1.AppConfig().SetServerPort(testPort)
		if err != nil {
			t.Fatalf("Failed to set server port: %v", err)
		}

		// Verify it was set
		if appManager1.GetGlobalConfig().GetServerPort() != testPort {
			t.Errorf("Expected port %d, got %d", testPort, appManager1.GetGlobalConfig().GetServerPort())
		}

		// Note: Port persistence is handled by config file, not by SaveConfig
		// The test just verifies that SetServerPort works within the same instance
	})
}

// TestResolveAlreadyRunningAction covers the guard that keeps a casual
// `tingly-box` (the npm/npx bare entrypoint, mapped to `start --daemon`)
// from restarting a running server and killing in-flight AI requests.
func TestResolveAlreadyRunningAction(t *testing.T) {
	tests := []struct {
		name           string
		runningVersion string
		promptRestart  bool
		tty            bool
		want           alreadyRunningAction
	}{
		{"same version shows info, no restart", "1.2.3", false, true, alreadyRunningShowInfo},
		{"same version non-tty shows info", "1.2.3", false, false, alreadyRunningShowInfo},
		{"different version prompts on tty", "1.2.2", false, true, alreadyRunningPrompt},
		{"different version hints without tty", "1.2.2", false, false, alreadyRunningHint},
		{"unknown version treated as mismatch: prompt on tty", "", false, true, alreadyRunningPrompt},
		{"unknown version treated as mismatch: hint without tty", "", false, false, alreadyRunningHint},
		{"explicit --prompt-restart always prompts on tty", "1.2.3", true, true, alreadyRunningPrompt},
		{"explicit --prompt-restart without tty degrades to hint", "1.2.3", true, false, alreadyRunningHint},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveAlreadyRunningAction(tt.runningVersion, "1.2.3", tt.promptRestart, tt.tty)
			if got != tt.want {
				t.Errorf("resolveAlreadyRunningAction(%q, %q, %v, %v) = %v, want %v",
					tt.runningVersion, "1.2.3", tt.promptRestart, tt.tty, got, tt.want)
			}
		})
	}
}
