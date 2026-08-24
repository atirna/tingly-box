package command

import (
	"fmt"

	"github.com/tingly-dev/tingly-box/internal/config"
	"github.com/tingly-dev/tingly-box/internal/server"
	serverconfig "github.com/tingly-dev/tingly-box/internal/server/config"
	"github.com/tingly-dev/tingly-box/pkg/lock"
)

// AppManager is the command process host: it owns AppConfig and server
// lifecycle. Domain behavior belongs in internal/usecase rather than here.
type AppManager struct {
	appConfig *config.AppConfig

	// launchSource is how this process was invoked (global --source flag;
	// "", "npx" or "npx-bundle"). Set once in main, read wherever the launch
	// method matters (shortcut generation, self-update) — in memory only,
	// never persisted (see .design/shortcut.md §3).
	launchSource LaunchSource
}

// SetLaunchSource records how this process was invoked. Called once from
// main before any command runs.
func (am *AppManager) SetLaunchSource(source LaunchSource) { am.launchSource = source }

// LaunchSource returns how this process was invoked.
func (am *AppManager) LaunchSource() LaunchSource { return am.launchSource }

// NewAppManager creates a new AppManager with the given config directory.
func NewAppManager(configDir string) (*AppManager, error) {
	appConfig, err := config.NewAppConfig(config.WithConfigDir(configDir))
	if err != nil {
		return nil, fmt.Errorf("failed to create app config: %w", err)
	}

	return &AppManager{
		appConfig: appConfig,
	}, nil
}

// NewAppManagerWithConfig creates a new AppManager with an existing AppConfig.
func NewAppManagerWithConfig(appConfig *config.AppConfig) *AppManager {
	return &AppManager{
		appConfig: appConfig,
	}
}

// AppConfig returns the underlying AppConfig.
func (am *AppManager) AppConfig() *config.AppConfig {
	return am.appConfig
}

// GetGlobalConfig returns the global configuration manager.
func (am *AppManager) GetGlobalConfig() *serverconfig.Config {
	return am.appConfig.GetGlobalConfig()
}

// ============
// Server Management
// ============

// StartServerAt initializes and starts the in-process server used by the TUI.
func (am *AppManager) StartServerAt(port int) error {
	serverManager := NewServerManager(am.appConfig,
		server.WithLaunchSource(string(am.launchSource)))
	if err := serverManager.Setup(port); err != nil {
		return err
	}
	return serverManager.Start()
}

// ============
// Configuration Accessors
// ============

// GetRuntimeServerPort returns the port the running server is actually
// listening on. The server port is intentionally not persisted in the config
// file, so a server started with --port would be invisible to other CLI
// processes; the server therefore records its port in a runtime port file
// next to the PID lock. When the server is running (lock held) and the port
// file is readable, that port wins; otherwise this falls back to the
// configured port.
func (am *AppManager) GetRuntimeServerPort() int {
	fileLock := lock.NewFileLock(am.appConfig.ConfigDir())
	if fileLock.IsLocked() {
		if port, err := fileLock.ReadPort(); err == nil {
			return port
		}
	}
	return am.appConfig.GetServerPort()
}
