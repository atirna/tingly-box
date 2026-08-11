package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/tingly-dev/tingly-box/internal/db"
	"github.com/tingly-dev/tingly-box/internal/server/config"
)

// Benchmarks for AuthMiddleware.ModelAuthMiddleware's multi-tenant
// "tb-share-" API-token path, wired against a real db.APITokenStore
// (SQLite-backed, exactly as internal/server/server.go constructs it)
// instead of a mock. Mirrors internal/routing/pipeline_bench_test.go's
// approach — benchmark the production wiring and read pprof, rather than
// reason about the DB-lock cost from first principles — for
// APITokenStore.ValidateToken, which internal/db/provider_store.go's
// GetByUUID turned out to share its lock+query-on-every-read shape with
// (see .design/hot-path-db-access.md).
//
// Run: go test ./internal/middleware/... -bench . -benchmem -run '^$'

func benchAPITokenStore(b *testing.B) (*db.APITokenStore, string) {
	b.Helper()
	store, err := db.NewAPITokenStore(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })

	token := "tb-share-benchtoken0000000000000000000000000000"
	if _, err := store.CreateTokenWithTokenID("bench-user", token, "bench", "bench", nil); err != nil {
		b.Fatal(err)
	}
	return store, token
}

// BenchmarkAPITokenStore_ValidateToken isolates the store call the auth
// middleware makes on every "tb-share-"-prefixed request, the same way
// pipeline_bench_test.go's BenchmarkHealthFilter_Filter isolates a single
// routing stage from the full pipeline benchmark.
func BenchmarkAPITokenStore_ValidateToken(b *testing.B) {
	store, token := benchAPITokenStore(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.ValidateToken(token); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkModelAuthMiddleware_APIToken exercises the full production gin
// middleware chain for a multi-tenant "tb-share-" request. UpdateLastUsed's
// fire-and-forget write (see ModelAuthMiddleware in auth.go) runs on its own
// goroutine exactly as in production, so this also reflects the write-lock
// contention a real deployment would see between concurrent validations and
// last-used updates.
func BenchmarkModelAuthMiddleware_APIToken(b *testing.B) {
	gin.SetMode(gin.TestMode)
	store, token := benchAPITokenStore(b)

	cfg := &config.Config{}
	cfg.MultiTenantConfig.Enabled = true

	am := NewAuthMiddleware(cfg, nil, nil, store)
	r := gin.New()
	r.POST("/v1/chat/completions", am.ModelAuthMiddleware(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("unexpected status %d: %s", w.Code, w.Body.String())
		}
	}
}
