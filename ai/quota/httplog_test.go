package quota

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The logging transport sits in front of every fetcher, so the one thing it
// must never do is eat the response body it logs.
func TestLoggingTransportPreservesErrorBody(t *testing.T) {
	t.Parallel()

	const body = `{"code":"permission_denied","message":"no coding plan"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, body, http.StatusForbidden)
	}))
	defer server.Close()

	resp, err := NewHTTPClient("", 5*time.Second).Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	defer resp.Body.Close()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}
	if strings.TrimSpace(string(got)) != body {
		t.Errorf("body = %q, want %q", strings.TrimSpace(string(got)), body)
	}
}

func TestRedactHeaders(t *testing.T) {
	t.Parallel()

	header := http.Header{}
	header.Set("Authorization", "Bearer secret-token")
	header.Set("X-Msh-Platform", "kimi_cli")

	got := redactHeaders(header)
	if strings.Contains(got, "secret-token") {
		t.Errorf("redactHeaders() leaked the credential: %q", got)
	}
	if !strings.Contains(got, "Authorization=<redacted>") {
		t.Errorf("redactHeaders() = %q, want the header name kept", got)
	}
	if !strings.Contains(got, "X-Msh-Platform=kimi_cli") {
		t.Errorf("redactHeaders() = %q, want non-credential headers verbatim", got)
	}
}
