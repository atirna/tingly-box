# Database Layer (`internal/db`)

Design notes for `internal/db`'s stores — SQLite via gorm, one shared
`*gorm.DB` owned by `StoreManager`. Performance is the first topic below;
other sections (schema, migrations, ...) can be added here as they come up.

## Performance

Status: `ProviderStore` and `APITokenStore` (read-cache fixes) and
`StatsStore`/`UsageStore` (transaction-merge fix) shipped as three
independent PRs off this investigation. Batched/debounced stats+usage
writes remain open — tracked below as the holistic follow-up.

### Why this matters

`internal/routing/pipeline_bench_test.go` benchmarks the real production
selection pipeline (`ServiceSelector` + `protocolserver.LoadBalancer`, wired
exactly as `internal/server/server.go` does). Profiling it showed
`ServiceSelector.Select` spending **83% of its own time** in one line:

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
UUID, so this cost was paid **once per LLM request**, unconditionally. The
same shape turned up in `APITokenStore.ValidateToken` (auth middleware) and
in `StatsStore`/`UsageStore`'s per-request writes — see below.

### `ProviderStore`

`internal/db/provider_store.go`'s `ProviderStore` keeps a write-through
in-memory mirror of the `providers` table:

- `cache map[string]*ProviderRecord` + an `order []string` slice (map
  iteration is randomized in Go; `order` keeps `List`/`ListOAuth`/`ListEnabled`
  stable in insertion order, matching what SQLite returned before and what the
  provider table UI depends on).
- `mu` changed from `sync.Mutex` to `sync.RWMutex`.
- All **read** methods serve from the cache under `RLock` — no SQLite call,
  no exclusive lock.
- All **write** methods write to SQLite first (still the source of truth /
  durability boundary), then update the cache entry, under `Lock`. Writes
  mutate a copy and only swap it into the cache once the SQLite write
  succeeds (`writeThroughLocked`), so a failed write can't leave the cache
  holding unpersisted data.
- The cache is loaded once at construction. Both `NewProviderStore` and
  `StoreManager.initProviderStore` funnel through one `newProviderStoreOverDB`
  helper for this, since `StoreManager` builds `ProviderStore` over its own
  already-open connection rather than calling `NewProviderStore` — two
  separate init paths that previously had to be kept in sync by hand (a bug
  once, caught by `TestStoreManager_StoreOperations`).

Single-process, single-DB-connection deployment (`StoreManager` owns the one
`*gorm.DB` shared by every store) makes this store the sole writer of its
table, so the mirror can't drift from concurrent external writers.

**Result** (same benchmark, before → after):

| Benchmark | before | after | allocs before → after |
|---|---|---|---|
| `BenchmarkSelect_Plain` | 59.7 µs/op | 5.8 µs/op (**10.2×**) | 236 → 76 |
| `BenchmarkSelect_SmartMatch` | 61.5 µs/op | 10.5 µs/op (**5.9×**) | 252 → 92 |
| `BenchmarkSelect_SmartNoMatch` | 66.9 µs/op | 9.7 µs/op (**6.9×**) | 251 → 91 |
| `BenchmarkSelect_Affinity` | 65.7 µs/op | 8.3 µs/op (**7.9×**) | 252 → 86 |

Re-profiling after the fix: `GetProviderByUUID` no longer appears in the
profile's top nodes at all. The new dominant cost (~27% cum) is
`loadbalance.ServiceID.String()` / `fmt.Sprintf` inside `HealthFilter.Filter`
(called twice per request — the double active/health filtering noted in
`pipeline_bench_test.go`) — a separate, smaller inefficiency, out of scope here.

### `APITokenStore`

`ValidateToken` is called by `AuthMiddleware.ModelAuthMiddleware` on every
request bearing a `tb-share-`-prefixed multi-tenant API token — the same
"every request, data barely ever changes" shape as `ProviderStore.GetByUUID`.
A benchmark was written first (`internal/middleware/auth_bench_test.go`,
wired against a real `db.APITokenStore`) and profiled before changing anything:

| Benchmark | before | after | change |
|---|---|---|---|
| `BenchmarkAPITokenStore_ValidateToken` (isolated store call) | 30.0 µs/op, 99 allocs | 119 ns/op, 1 alloc | **~251×** |
| `BenchmarkModelAuthMiddleware_APIToken` (full gin middleware chain) | 103.8 µs/op, 182 allocs | 55.4 µs/op, 80 allocs | **1.87×** |

The fix mirrors `ProviderStore`: a write-through `map[string]*APITokenRecord`
cache keyed by `TokenID`, `RWMutex`. Reads (`ValidateToken`, `GetToken`,
`ListTokens`) hit the cache; writes hit SQLite first, then update the cache
entry under the same write lock — so a revoked token stops validating the
instant `RevokeToken` returns. `CleanupExpiredTokens` (a bulk, predicate-based
delete) just reloads the whole cache from SQLite afterward rather than
re-deriving its `WHERE` clause in Go — not hot path, so the O(n) reload cost
doesn't matter.

**Why the full-middleware number only improved 1.87× despite `ValidateToken`
itself improving 251×**: the remaining cost is `APITokenStore.UpdateLastUsed`
— a genuine SQLite **write** on every authenticated request (fire-and-forget,
so it doesn't block the response, but it's real DB load), not a read this
caching pattern touches.

**`UpdateLastUsed`: debounce the write instead of caching a read.**
`last_used_at` only needs display-level freshness (token admin UI), not
per-request precision. `UpdateLastUsed` now persists at most once per
`defaultLastUsedDebounce` (1 minute) per token — checked cheaply under
`RLock` against the cached `LastUsedAt` before ever touching SQLite. No new
cache or flusher: the debounce state *is* the existing cache entry.

| Benchmark | cached only | + debounced `UpdateLastUsed` | change |
|---|---|---|---|
| `BenchmarkModelAuthMiddleware_APIToken` | 55.4 µs/op, 80 allocs | 1.39 µs/op, 10 allocs | **~40×** (≈75× vs. the original baseline) |

### `StatsStore` + `UsageStore`

An audit of every `internal/db/*_store.go` (see below) turned up two stores
on the request hot path despite looking like background bookkeeping:
`StatsStore.UpdateFromService` and `UsageStore.RecordUsage`, called
synchronously once per completed LLM request from
`ProtocolHandler.trackUsageWithTokenUsage` / `trackUsageFromContext`
(`internal/protocolserver/usage_tracking.go`). Unlike `ProviderStore` and
`APITokenStore`, both are genuine per-request **writes** with no prior read —
the read-cache pattern above doesn't apply.

| Benchmark | Result |
|---|---|
| `BenchmarkStatsStore_UpdateFromService` (upsert) | ~80 µs/op, 129 allocs |
| `BenchmarkUsageStore_RecordUsage` (insert) | ~307 µs/op, 117 allocs |
| `BenchmarkStatsAndUsage_Combined` (both, separate stores) | ~440 µs/op, 246 allocs |

pprof showed both dominated by gorm's implicit transaction wrapper
(`BEGIN`/`COMMIT`) around each write; `UsageStore.RecordUsage` costs ~4×
`StatsStore.UpdateFromService` because `usage_records` has 6 indexed columns
to maintain per insert against `service_stats`' single composite primary key.

Two directions were considered:

- **`PRAGMA synchronous=NORMAL`** — investigated, implemented, benchmarked,
  and **reverted**: read back as `1` (NORMAL) both before and after the
  change. The driver (`mattn/go-sqlite3`) already forces `synchronous=NORMAL`
  whenever `_journal_mode=WAL` is set (every store here already sets that),
  so the DSN change was a no-op — confirmed the pragma itself matters
  (isolated raw-driver test: 373 µs/op FULL vs. 26 µs/op NORMAL, 14×) but
  that headroom was never available here. Lesson: read back the actual
  `PRAGMA` value before crediting a DSN change with a win.

- **Merge the two writes into one transaction** (`db.RecordRequestOutcome`,
  `internal/db/record_outcome.go`) — implemented. `UpdateFromService` and
  `RecordUsage` were split into a pure record-builder and the DB write, so
  `RecordRequestOutcome` builds both records (outside any lock) and saves
  them inside one `gorm.DB.Transaction(...)`, assuming both stores share the
  same `*gorm.DB` (always true via `StoreManager`).

  | Benchmark | separate transactions | merged transaction | change |
  |---|---|---|---|
  | `BenchmarkStatsAndUsage_Combined` vs. `_RecordRequestOutcome` | ~440 µs/op, 246 allocs | ~420 µs/op, 235 allocs | **~4–8%**, 11 fewer allocs/op |

  Confirmed via pprof the merge is real (`gorm.(*DB).Commit` appears once
  per call, not twice), but the win is modest: most of the ~440 µs isn't
  transaction-wrapper overhead, it's the two per-row statements themselves
  — removing one `COMMIT` doesn't remove either statement. Kept anyway: the
  win is real, and it adds a correctness improvement — the stats and usage
  rows for one request can no longer partially persist
  (`TestRecordRequestOutcome_AtomicRollback`).

**Still open**: batching/debouncing multiple requests' stats and usage
writes into fewer, larger transactions — the actual lever for the remaining
per-statement cost, at the cost of async durability (a crash loses whatever's
still buffered) and added complexity (buffer, flush trigger, backpressure).

**Fixed in passing**: `NewUsageStore` never created the `db` subdirectory
`constant.GetDBFile` expects (every other `NewXStore` constructor does).
Never hit in production since `StoreManager` builds `UsageStore` directly
over its own already-open `*gorm.DB` — surfaced when
`stats_usage_bench_test.go` became `NewUsageStore`'s first-ever caller.

### Audit: is your store on a hot path?

Every store under `internal/db/` follows the same shape: a GORM-backed
SQLite table, one `sync.Mutex` (not `RWMutex`) per store, every method —
reads included — taking the exclusive lock and issuing a query. Whether
that matters depends on whether the store is read/written on the **request
hot path** vs. an **admin/UI path** or the lower-QPS **IM-bot/remote-control
message path**:

| Store | Hot-path? | Where | Status |
|---|---|---|---|
| `ProviderStore` | Yes (read) | `ServiceSelector.Select` resolves a provider every request | Fixed — read cache |
| `APITokenStore` | Yes (read + write) | `AuthMiddleware.ModelAuthMiddleware` validates every API-token request, updates `last_used_at` | Fixed — read cache + debounced write |
| `StatsStore`, `UsageStore` | Yes (write) | `usage_tracking.go` persists stats + a usage row every completed request | Partially fixed — merged transaction; batching still open |
| `ImBotSettingsStore` | No | Bot lifecycle events, admin REST/CLI only | Not touched — genuinely low frequency |
| `ModelStore` | No | Admin "list/refresh models" endpoints, OAuth completion, CLI | Not touched — already `RWMutex`, admin-paced |
| `TaskStore` | Dead code | Zero callers; the real task subsystem uses `internal/task/store.go` instead | Not touched — separate cleanup ticket |
| `ToolConfigStore` | Effectively unreachable | `Config.GetToolConfig` is a different, in-memory-only implementation that never touches this store | Not touched — separate cleanup ticket |
| `RemoteChatStore`, `RemoteSessionStore`, `BotAccessStore` | IM-bot message path | `remote/control/remoteagent/handler_message.go` et al. | Not touched — real traffic but chat-message-rate, not LLM-API-rate; no app-level mutex today |

### Recommended approach for future stores

1. **Classify first**: is this store read on the per-request hot path? If
   not, don't bother caching — `sync.Mutex` → `sync.RWMutex` is enough, and
   only worth doing if profiling shows contention.
2. **For hot-path reads**: write-through cache keyed by the natural lookup
   key, `RWMutex`, writes go to SQLite first then update the cache under the
   same write lock. Don't invent a TTL/invalidation scheme — these stores
   are single-writer-per-process, so "cache is only ever mutated by this
   store's own write methods" is sufficient.
3. **Watch for bypass construction**: `StoreManager` builds several stores
   via struct literals instead of calling each store's `NewXStore`
   constructor. Any cached store needs a shared `newXStoreOverDB(db, dbPath)`
   helper that both `NewXStore` and `StoreManager.initXStore` call, so the
   cache-init wiring can't drift between two hand-duplicated copies.
4. **Write the benchmark first**, against the real production wiring, and
   profile before touching any code — don't reason about the win from first
   principles; measure it.
