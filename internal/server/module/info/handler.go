// Package versioncheck exposes the /info/* HTTP endpoints (health, config,
// version, and latest-version check). Version lookup itself is delegated to
// Checker; this file only handles request/response wiring.
package info

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler carries the minimal server state needed to serve /info/* endpoints.
type Handler struct {
	version      string
	configFile   string
	configDir    string
	launchSource string
}

// NewHandler creates a Handler. launchSource is how this server process was
// invoked (see internal/shortcut source constants; empty means plain binary)
// — it decides whether the one-click update path is available.
func NewHandler(version, configFile, configDir, launchSource string) *Handler {
	return &Handler{
		version:      version,
		configFile:   configFile,
		configDir:    configDir,
		launchSource: launchSource,
	}
}

// --- handlers ---------------------------------------------------------------

// GetHealthInfo is a lightweight health check that can be called frequently.
func (h *Handler) GetHealthInfo(c *gin.Context) {
	c.JSON(http.StatusOK, HealthInfoResponse{
		Health:  true,
		Status:  "healthy",
		Service: "tingly-box",
	})
}

// GetInfoConfig returns the runtime configuration paths.
func (h *Handler) GetInfoConfig(c *gin.Context) {
	c.JSON(http.StatusOK, ConfigInfoResponse{
		Success: true,
		Data: ConfigInfo{
			ConfigPath: h.configFile,
			ConfigDir:  h.configDir,
		},
	})
}

// GetInfoVersion returns the current running version.
func (h *Handler) GetInfoVersion(c *gin.Context) {
	c.JSON(http.StatusOK, VersionInfoResponse{
		Success: true,
		Data:    VersionInfo{Version: h.version},
	})
}

// GetLatestVersion checks the npm registry for the latest published version
// and compares it with the running version.
func (h *Handler) GetLatestVersion(c *gin.Context) {
	checker := New()
	latestVersion, releaseURL, err := checker.CheckLatestVersion()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, LatestVersionResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	current := h.version
	hasUpdate := CompareVersions(latestVersion, current) > 0

	c.JSON(http.StatusOK, LatestVersionResponse{
		Success: true,
		Data: LatestVersionInfo{
			CurrentVersion: current,
			LatestVersion:  latestVersion,
			HasUpdate:      hasUpdate,
			ReleaseURL:     releaseURL,
			ShouldNotify:   hasUpdate,
			LaunchSource:   h.launchSource,
			CanOneClick:    CanOneClickUpdate(h.launchSource),
		},
	})
}

// PostUpdate applies a one-click update: it relaunches Tingly Box through npx
// pinned to the latest version, fully detached. The relaunch runs `restart
// --daemon`, so the new version stops this server and takes over; the caller
// should poll /info/version until it reports the new version. Only available
// for npx-based installs (see CanOneClickUpdate).
func (h *Handler) PostUpdate(c *gin.Context) {
	if !CanOneClickUpdate(h.launchSource) {
		c.JSON(http.StatusBadRequest, UpdateApplyResponse{
			Error: "one-click update is only available for npx-based installs; update Tingly Box the way it was installed",
		})
		return
	}

	latestVersion, _, err := New().CheckLatestVersion()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, UpdateApplyResponse{
			Error: fmt.Sprintf("failed to resolve the latest version: %v", err),
		})
		return
	}
	if CompareVersions(latestVersion, h.version) <= 0 {
		c.JSON(http.StatusBadRequest, UpdateApplyResponse{
			Error: fmt.Sprintf("already up to date (running %s, latest %s)", h.version, latestVersion),
		})
		return
	}

	spec := updateLaunchSpec(h.launchSource, latestVersion, "Tingly Box")
	command, err := spawnDetached(spec)
	if err != nil {
		c.JSON(http.StatusInternalServerError, UpdateApplyResponse{
			Error: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, UpdateApplyResponse{
		Success: true,
		Data: UpdateApplyInfo{
			TargetVersion: latestVersion,
			Command:       command,
		},
	})
}
