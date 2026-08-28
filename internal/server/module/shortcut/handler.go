// Package shortcut exposes the /api/v1/shortcut HTTP endpoints that let the
// frontend create (or check for) the desktop / start-menu shortcut the CLI's
// `tingly-box shortcut` command already writes. All actual file-writing logic
// lives in internal/shortcut, which has no HTTP or CLI dependency — this
// package only adapts it to gin, mirroring the "future HTTP handler" sketch
// in .design/shortcut.md.
package shortcut

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/tingly-dev/tingly-box/internal/shortcut"
)

// Handler carries the minimal server state needed to serve /api/v1/shortcut.
// launchSource and version are known once, at boot, from how this process
// itself was started (see internal/command's --source flag); they are never
// persisted, matching the CLI's own "no detection, no persistence" design.
type Handler struct {
	launchSource string
	version      string
}

// NewHandler creates a Handler.
func NewHandler(launchSource, version string) *Handler {
	return &Handler{launchSource: launchSource, version: version}
}

func (h *Handler) resolveSpec() (shortcut.LaunchSpec, error) {
	exePath, err := shortcut.ResolveExePath()
	if err != nil {
		return shortcut.LaunchSpec{}, err
	}
	return shortcut.ResolveLaunch(exePath, h.launchSource, h.version), nil
}

// Create handles POST /api/v1/shortcut: (re)writes the desktop / start-menu
// shortcut(s) and the Linux headless launcher script, and returns the real
// paths written. Deliberately zero-config beyond an optional display name —
// no --no-desktop/--no-menu equivalent — and idempotent, so the frontend can
// let the user click it again any time (after an upgrade, a source change,
// or just to recover a deleted shortcut) without needing to track state.
func (h *Handler) Create(c *gin.Context) {
	var req ShortcutCreateRequest
	_ = c.ShouldBindJSON(&req)
	if req.Name == "" {
		req.Name = "Tingly Box"
	}

	spec, err := h.resolveSpec()
	if err != nil {
		c.JSON(http.StatusInternalServerError, ShortcutCreateResponse{Error: err.Error()})
		return
	}

	created, err := shortcut.Create(shortcut.Options{Name: req.Name}, spec)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ShortcutCreateResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, ShortcutCreateResponse{
		Success: true,
		Data:    ShortcutInfo{Created: created, ScriptPath: scriptPathAmong(created)},
	})
}

// Status handles GET /api/v1/shortcut: reports whether every artifact Create
// would write already exists, without writing anything itself. Best-effort —
// see shortcut.ExpectedPaths for the Windows OneDrive-redirection caveat.
func (h *Handler) Status(c *gin.Context) {
	opts := shortcut.Options{Name: "Tingly Box"}
	paths := shortcut.ExpectedPaths(opts)

	exists := len(paths) > 0
	var present []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			present = append(present, p)
		} else {
			exists = false
		}
	}

	c.JSON(http.StatusOK, ShortcutStatusResponse{
		Success: true,
		Exists:  exists,
		Data:    ShortcutInfo{Created: present, ScriptPath: scriptPathAmong(present)},
	})
}

// scriptPathAmong returns the one path in paths that is the Linux headless
// launcher script (identified by its .sh suffix, same detection the CLI's
// ShortcutCmdKong.Run already uses), or "" if none is present.
func scriptPathAmong(paths []string) string {
	for _, p := range paths {
		if strings.HasSuffix(p, ".sh") {
			return p
		}
	}
	return ""
}
