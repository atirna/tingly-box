# Hot-Path DB Access: Cache Reads, Don't Lock+Query Them

Status: `ProviderStore` and `APITokenStore` fixed on branch
`claude/routing-pipeline-benchmark-e6aq73`. Rest of the pattern below is
diagnosed but **not yet fixed** — tracked here as the holistic follow-up.

## Why

`internal/routing/pipeline_bench_test.go` benchmarks the real production selection
pipeline (`ServiceSelector` + `protocolserver.LoadBalancer`, wired exactly as
`internal/server/server.go` does). Profiling it (`go test ./internal/routing/...
-bench . -cpuprofile=cpu.prof`) showed `ServiceSelector.Select` spending **83% of
its own time** in one line:

```go
provider, err := s.config.GetProviderByUUID(result.Service.Provider)
```

That call chain — `config.Config.GetProviderByUUID` → `db.ProviderStore.GetByUUID`
→ `gorm.DB.First` → SQLite via cgo — accounted for **47.5% of the entire
benchmark's CPU time** (`cgocall` alone was 20.6%; `database/sql` locking/scan
machinery another ~25%). `ProviderStore` additionally guarded every method,
reads included, with a plain `sync.Mutex` — so even *read* traffic across
concurrent requests fully serialized on one lock, on top of paying for a disk
round-trip that returns data changing only on admin CRUD.

This isn't a one-off: every routed request resolves exactly one provider by
UUID, so this cost was paid **once per LLM request**, unconditionally.

## What changed

`internal/db/provider_store.go`'s `ProviderStore` now keeps a write-through
in-memory mirror of the `providers` table:

- `cache map[string]*ProviderRecord` + an `order []string` slice (map
  iteration is randomized in Go; `order` keeps `List`/`ListOAuth`/`ListEnabled`
  stable in insertion order, matching what SQLite returned before and what the
  provider table UI depends on).
- `mu` changed from `sync.Mutex` to `sync.RWMutex`.
- All **read** methods (`GetByUUID`, `GetByName`, `List`, `ListOAuth`,
  `ListEnabled`, `Exists`, `Count`, `GetAccessToken`, `IsOAuthExpired`) serve
  from the cache under `RLock` — no SQLite call, no exclusive lock.
- All **write** methods (`Save`, `Delete`, `UpdateCredential`,
  `UpdateCredentialBundle`, `UpdateOAuthAccessToken`) write to SQLite first
  (still the source of truth / durability boundary), then update the cache
  entry, under `Lock`.
- The cache is loaded once at construction (`loadCache`, called from both
  `NewProviderStore` and `StoreManager.initProviderStore` — the latter builds
  `ProviderStore` via a struct literal rather than the constructor, so it
  needed the same `cache: make(...)` + `loadCache()` wiring or the cache would
  stay a nil map in the shipped binary; this bit a first pass and was caught
  by `TestStoreManager_StoreOperations` panicking on a nil-map write).

Single-process, single-DB-connection deployment (see `StoreManager`, which
owns the one `*gorm.DB` shared by every store) makes this store the sole
writer of its table, so the mirror can't drift from concurrent external
writers.

### Result (same benchmark, before → after)

| Benchmark | before | after | allocs before → after |
|---|---|---|---|
| `BenchmarkSelect_Plain` | 59.7 µs/op | 5.8 µs/op (**10.2×**) | 236 → 76 |
| `BenchmarkSelect_SmartMatch` | 61.5 µs/op | 10.5 µs/op (**5.9×**) | 252 → 92 |
| `BenchmarkSelect_SmartNoMatch` | 66.9 µs/op | 9.7 µs/op (**6.9×**) | 251 → 91 |
| `BenchmarkSelect_Affinity` | 65.7 µs/op | 8.3 µs/op (**7.9×**) | 252 → 86 |

Re-profiling after the fix: `GetProviderByUUID` no longer appears in the
profile's top nodes at all. The new dominant cost (~27% cum) is
`loadbalance.ServiceID.String()` / `fmt.Sprintf` inside `HealthFilter.Filter`
(itself called twice per request — see the double active/health filtering
note in `pipeline_bench_test.go`) — a separate, much smaller inefficiency,
noted here as a candidate for a future pass but out of scope of this fix.

## `APITokenStore`: same shape, confirmed by benchmark before the fix

`ValidateToken` is called by `AuthMiddleware.ModelAuthMiddleware` (see
`internal/middleware/auth.go`) on every request bearing a `tb-share-`-prefixed
multi-tenant API token — the exact same "every request, data barely ever
changes" shape as `ProviderStore.GetByUUID`. Per the follow-up plan above, a
benchmark was written first (`internal/middleware/auth_bench_test.go`,
wired against a real `db.APITokenStore`, mirroring how
`pipeline_bench_test.go` wires the real production routing pipeline) and
profiled *before* changing anything:

| Benchmark | before | after | change |
|---|---|---|---|
| `BenchmarkAPITokenStore_ValidateToken` (isolated store call) | 30.0 µs/op, 99 allocs | 119 ns/op, 1 alloc | **~251×** |
| `BenchmarkModelAuthMiddleware_APIToken` (full gin middleware chain) | 103.8 µs/op, 182 allocs | 55.4 µs/op, 80 allocs | **1.87×** |

The fix mirrors `ProviderStore` exactly: `APITokenStore` now keeps a
write-through `map[string]*APITokenRecord` cache keyed by `TokenID`, guarded
by `RWMutex`. `ValidateToken`, `GetToken`, and the now-`RLock`-only
`ListTokens` read the cache; `CreateTokenWithTokenID`, `RevokeToken`,
`UpdateLastUsed`, `SetTokenEnabled`, `UpdateTokenString`, and `DeleteToken`
write SQLite first, then update (or remove) the cache entry under the same
write lock — so a revoked token stops validating the instant `RevokeToken`
returns, with no window where SQLite says revoked but the cache still says
enabled. `CleanupExpiredTokens` (a bulk, predicate-based delete) just
reloads the whole cache from SQLite afterward rather than re-deriving its
`WHERE` clause in Go — it's a maintenance job, not hot path, so the O(n)
reload cost doesn't matter.

**Why the full-middleware number only improved 1.87× despite `ValidateToken`
itself improving 251×**: profiling `BenchmarkModelAuthMiddleware_APIToken`
after the fix shows the remaining cost is almost entirely
`APITokenStore.UpdateLastUsed` — the fire-and-forget `go
am.apiTokenStore.UpdateLastUsed(...)` call in `ModelAuthMiddleware` — which
is a genuine SQLite **write** on every authenticated request (updating
`last_used_at`), not a read this caching pattern touches. It doesn't block
the response (it's backgrounded), but it's real DB load per request and
still serializes against other cache writes under `s.mu.Lock()`. That's a
separate finding, noted here as a candidate for a future pass (e.g.
debouncing/batching last-used-at writes so it's not one SQLite transaction
per request) but out of scope for this "cache the hot read path" fix.

## The pattern is systemic — audit of `internal/db/*_store.go`

Every store under `internal/db/` follows the same shape: a GORM-backed SQLite
table, one `sync.Mutex` (not `RWMutex`) per store, and every method —
including pure reads — takes the exclusive lock and issues a query. Only
`provider_model.go` (`ModelStore`) and `usage_record.go`/`store_manager.go`
already used `RWMutex`, and none of them cache. Whether this matters for a
given store depends entirely on whether it's read on the **request hot path**
(every inbound request) versus an **admin/UI path** (occasional, human-paced):

| Store | Hot-path read? | Where | Risk |
|---|---|---|---|
| `ProviderStore` | **Yes — fixed** | `ServiceSelector.Select` resolves a provider every request | was the #1 cost, now cached |
| `APITokenStore` | **Yes — fixed** | `internal/middleware/auth.go`'s `AuthMiddleware` calls `ValidateToken` on every API-token-authenticated request | see benchmark section above — `ValidateToken` itself dropped ~251×; `UpdateLastUsed`'s per-request write remains a separate, smaller follow-up |
| `ModelStore`, `ToolConfigStore`, `ImBotSettingsStore`, `TaskStore`, `ServiceStatStore` | No (or infrequent/admin-path) | config/UI/background jobs | low priority — `RWMutex` alone (no caching) is probably sufficient if ever contended |
| `RemoteChatStore`, `RemoteSessionStore`, `BotAccessStore`, `UsageStore` | No (IM/remote-control and usage-recording paths, not the LLM proxy hot path) | — | low priority |

## Recommended approach for future stores

1. **Classify first**: is this store read on the per-request hot path? If not,
   don't bother caching — `sync.Mutex` → `sync.RWMutex` is enough, and only
   worth doing if profiling shows contention.
2. **For hot-path reads**: mirror the pattern here — write-through cache keyed
   by the natural lookup key, `RWMutex`, writes go to SQLite first then update
   the cache under the same write lock. Don't invent a TTL/invalidation
   scheme; these stores are single-writer-per-process, so "cache is only ever
   mutated by this store's own write methods" is sufficient and avoids
   staleness bugs.
3. **Watch for bypass construction**: `StoreManager` builds several stores via
   struct literals (`&XStore{db: ..., dbPath: ...}`) instead of calling each
   store's `NewXStore` constructor. Any future cached store must get its cache
   initialized (and loaded) in *both* places (`internal/db/store_manager.go`'s
   `initXStore` and the store's own `NewXStore`), or add a
   `StoreManager`-only constructor path and delete the literal. This is
   exactly what broke `TestStoreManager_StoreOperations` on the first pass on
   `ProviderStore`, and would have broken `APITokenStore` the same way had
   `initAPITokenStore` not been fixed alongside it.
4. **Write the benchmark first, against the real production wiring, and
   profile before touching any code** — `internal/routing/pipeline_bench_test.go`
   for the routing pipeline, `internal/middleware/auth_bench_test.go` for the
   auth middleware path. Both fixes here were validated this way (see the
   before/after tables above); don't reason about the win from first
   principles.
