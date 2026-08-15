package db

import (
	"testing"

	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// GetByUUID is resolved once per routed LLM request (ServiceSelector.Select,
// see .design/db.md) and is served entirely from the write-through cache --
// so what it costs is whatever toProvider() does to turn the cached record
// into a typ.Provider. While the JSON columns were `string` fields that meant
// a json.Unmarshal per request; with `serializer:json` the record already
// holds the decoded values and toProvider only has to clone them.
//
// Run: go test ./internal/db/... -bench 'ProviderStore_GetByUUID' -benchmem -run '^$'

func benchmarkGetByUUID(b *testing.B, p *typ.Provider) {
	store, err := NewProviderStore(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	if err := store.Save(p); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.GetByUUID(p.UUID); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProviderStore_GetByUUID_Tagged: the common case, a provider
// carrying a handful of tags.
func BenchmarkProviderStore_GetByUUID_Tagged(b *testing.B) {
	benchmarkGetByUUID(b, &typ.Provider{
		UUID: "bench-uuid", Name: "bench", APIBase: "https://api.example.com",
		APIStyle: protocol.APIStyleOpenAI, AuthType: typ.AuthTypeAPIKey,
		Token: "tok", Enabled: true,
		Tags: []string{"prod", "openai", "us-east", "tier1"},
	})
}

// BenchmarkProviderStore_GetByUUID_OAuth: the heaviest shape, tags plus an
// OAuth ExtraFields map.
func BenchmarkProviderStore_GetByUUID_OAuth(b *testing.B) {
	benchmarkGetByUUID(b, &typ.Provider{
		UUID: "bench-uuid", Name: "bench", APIBase: "https://api.example.com",
		APIStyle: protocol.APIStyleAnthropic, AuthType: typ.AuthTypeOAuth,
		Enabled: true, Tags: []string{"prod"},
		OAuthDetail: &typ.OAuthDetail{
			AccessToken: "access", RefreshToken: "refresh",
			ExpiresAt:   "2030-01-01T00:00:00Z",
			ExtraFields: map[string]any{"id_token": "abc", "organization_id": "org_123"},
		},
	})
}

// BenchmarkProviderStore_GetByUUID_NoJSON is the control: a provider with
// every JSON column empty. It should be unaffected by how those columns are
// encoded, and so pins that any delta in the two above is really the JSON
// work and not measurement drift.
func BenchmarkProviderStore_GetByUUID_NoJSON(b *testing.B) {
	benchmarkGetByUUID(b, &typ.Provider{
		UUID: "bench-uuid", Name: "bench", APIBase: "https://api.example.com",
		APIStyle: protocol.APIStyleOpenAI, AuthType: typ.AuthTypeAPIKey,
		Token: "tok", Enabled: true,
	})
}
