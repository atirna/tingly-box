package shortcut

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupTestRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/shortcut", h.Status)
	router.POST("/shortcut", h.Create)
	return router
}

// withHome points os.UserHomeDir (and XDG_DATA_HOME) at a fresh temp dir so
// the handler never touches the real machine's Desktop/.local — same
// isolation internal/shortcut's own tests use.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	return home
}

func TestHandlerCreate(t *testing.T) {
	home := withHome(t)
	h := NewHandler("binary", "1.4.2")
	router := setupTestRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/shortcut", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ShortcutCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}
	if len(resp.Data.Created) == 0 {
		t.Fatal("expected at least one created path")
	}
	wantScript := filepath.Join(home, ".local", "bin", "tingly-box.sh")
	if resp.Data.ScriptPath != wantScript {
		t.Errorf("expected script path %q, got %q", wantScript, resp.Data.ScriptPath)
	}
	for _, p := range resp.Data.Created {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("Create reported path %q but it does not exist: %v", p, err)
		}
	}
}

func TestHandlerCreateCustomName(t *testing.T) {
	withHome(t)
	h := NewHandler("binary", "1.4.2")
	router := setupTestRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/shortcut", strings.NewReader(`{"name":"My Box"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp ShortcutCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	for _, p := range resp.Data.Created {
		if !strings.Contains(p, "my-box") {
			t.Errorf("expected slugified custom name in path, got %q", p)
		}
	}
}

func TestHandlerStatusBeforeAndAfterCreate(t *testing.T) {
	withHome(t)
	h := NewHandler("binary", "1.4.2")
	router := setupTestRouter(h)

	// Before creating anything, status should report not-exists.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/shortcut", nil))
	var status ShortcutStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if status.Exists {
		t.Fatalf("expected exists=false before any create, got %+v", status)
	}

	// Create, then status should flip to exists=true and report the same paths.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/shortcut", nil))
	var created ShortcutCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/shortcut", nil))
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !status.Exists {
		t.Fatalf("expected exists=true after create, got %+v", status)
	}
	if len(status.Data.Created) != len(created.Data.Created) {
		t.Errorf("expected status to report the same paths create wrote: got %v vs %v", status.Data.Created, created.Data.Created)
	}
}

func TestScriptPathAmong(t *testing.T) {
	if got := scriptPathAmong([]string{"/a/tingly-box.desktop", "/b/tingly-box.sh"}); got != "/b/tingly-box.sh" {
		t.Errorf("unexpected script path: %q", got)
	}
	if got := scriptPathAmong([]string{"/a/tingly-box.desktop"}); got != "" {
		t.Errorf("expected empty script path, got %q", got)
	}
}
