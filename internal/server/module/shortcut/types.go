package shortcut

// ShortcutCreateRequest is the JSON body for POST /api/v1/shortcut. Empty is
// a valid request — Name defaults to "Tingly Box" and both desktop/menu
// artifacts are always written (no --no-desktop/--no-menu equivalent exposed
// here: the CLI flags exist for scripting, but the UI has one zero-config
// action). Named with the module's own prefix, not the generic "CreateRequest"
// — swagger's model registry keys schemas by Go type name, and several other
// modules (team, imbot) already have their own CreateRequest, so a bare name
// here would silently collide with one of theirs in the generated schema.
type ShortcutCreateRequest struct {
	Name string `json:"name,omitempty" example:"Tingly Box"`
}

// ShortcutInfo describes the outcome of a create (or status) call.
type ShortcutInfo struct {
	// Created lists every path written (or, for status, that already exists),
	// e.g. ["/home/user/Desktop/tingly-box.desktop", "/home/user/.local/bin/tingly-box.sh"].
	Created []string `json:"created"`
	// ScriptPath is the headless launcher script's path (Linux-only), or ""
	// when none was written/found — lets the frontend swap "double-click" for
	// "run this command" without re-deriving platform logic.
	ScriptPath string `json:"script_path,omitempty" example:"/home/user/.local/bin/tingly-box.sh"`
}

// ShortcutCreateResponse is the JSON envelope for POST /api/v1/shortcut.
type ShortcutCreateResponse struct {
	Success bool         `json:"success" example:"true"`
	Error   string       `json:"error,omitempty"`
	Data    ShortcutInfo `json:"data,omitempty"`
}

// ShortcutStatusResponse is the JSON envelope for GET /api/v1/shortcut.
// Exists is true only when every artifact this platform would write is
// already present on disk (a partial/stale set still reads as "not created"
// — the Create action rewrites everything in one pass anyway). Named with
// the module's own prefix for the same reason as ShortcutCreateRequest — a
// bare "StatusResponse" already collides with internal/server's own type.
type ShortcutStatusResponse struct {
	Success bool         `json:"success" example:"true"`
	Data    ShortcutInfo `json:"data"`
	Exists  bool         `json:"exists" example:"false"`
}
