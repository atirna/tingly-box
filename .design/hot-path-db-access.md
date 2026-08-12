# Hot-Path DB Access: Cache Reads, Don't Lock+Query Them

Status: `ProviderStore` and `APITokenStore` (read-cache fixes) and
`StatsStore`/`UsageStore` (transaction-merge fix) shipped on branch
`claude/routing-pipeline-benchmark-e6aq73`. Batched/debounced stats+usage
writes remain open — tracked here as the holistic follow-up.

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
still serialized against other cache writes under `s.mu.Lock()`.

### `UpdateLastUsed`: debounce the write instead of caching a read

`last_used_at` is only ever consumed for "when was this token last used"
display in the token admin UI — nothing in the codebase reads it at
per-request precision. So instead of caching (there's no read to cache; this
*is* the write), `UpdateLastUsed` now coalesces: it persists at most once per
`defaultLastUsedDebounce` (1 minute) per token, checked cheaply under
`RLock` against the cached `LastUsedAt` before ever taking the write lock or
touching SQLite; a call inside the window is a no-op. A token idle for over
a minute still gets a fresh write on its next use, same as before — only the
common case (bursty/sustained traffic on the same token) collapses from "one
SQLite UPDATE per request" to "one per debounce window."

| Benchmark | cached only | + debounced `UpdateLastUsed` | additional change |
|---|---|---|---|
| `BenchmarkModelAuthMiddleware_APIToken` | 55.4 µs/op, 80 allocs | 1.39 µs/op, 10 allocs | **~40×** (≈75× vs. the original 103.8 µs/op baseline) |

No TTL cache or background flusher was introduced — the debounce state
*is* the existing per-token cache entry's `LastUsedAt` field, so there's
nothing extra to keep in sync or expire. `internal/db/api_token_store_test.go`
covers all three cases: first call persists, a second call inside the window
is a no-op (checked against the DB row, not just the cache, so a caching bug
that skipped the write but claimed success can't hide), and a call after the
window (simulated by rewriting the cached `LastUsedAt` into the past — no
`time.Sleep` in the test) persists again.

## `StatsStore` + `UsageStore`: write-heavy hot path, a different fix shape

A follow-up audit of every `internal/db/*_store.go` (below) turned up two
stores that ARE on the request hot path despite looking like background
bookkeeping: `StatsStore.UpdateFromService` and `UsageStore.RecordUsage`.
Both are called synchronously, once per completed LLM request (streaming and
non-streaming, success and error paths), from
`ProtocolHandler.trackUsageWithTokenUsage` / `trackUsageFromContext`
(`internal/protocolserver/usage_tracking.go`) — and, unlike `ProviderStore`
and `APITokenStore`, both are genuine per-request **writes** with no prior
read: there's a real row to persist every time, so the read-cache pattern
above doesn't apply. Benchmarked in `internal/db/stats_usage_bench_test.go`:

| Benchmark | Result |
|---|---|
| `BenchmarkStatsStore_UpdateFromService` (upsert) | ~80 µs/op, 129 allocs |
| `BenchmarkUsageStore_RecordUsage` (insert) | ~307 µs/op, 117 allocs |
| `BenchmarkStatsAndUsage_Combined` (both, back to back, separate stores) | ~440 µs/op, 246 allocs |

pprof showed both dominated by gorm's implicit transaction wrapper
(`BEGIN`/`COMMIT`) around each write, with `UsageStore.RecordUsage` costing
~4× `StatsStore.UpdateFromService` because `usage_records` has 6 indexed
columns to maintain per insert against `service_stats`' single composite
primary key.

**Two directions were considered and one was rejected on evidence:**

- **`PRAGMA synchronous=NORMAL`** (skip the fsync SQLite issues after each
  `COMMIT` under the default `FULL`) — investigated, implemented, benchmarked,
  and **reverted**: `PRAGMA synchronous` read back as `1` (NORMAL) both
  before and after the change. The driver, `mattn/go-sqlite3`, is compiled
  with `-DSQLITE_DEFAULT_WAL_SYNCHRONOUS=1` and additionally forces
  `synchronous=NORMAL` itself whenever `_journal_mode=WAL` is requested
  (every store here already sets that) — see the driver's `sqlite3.go`
  around `case "WAL": ... synchronousMode = "NORMAL"`. So every store was
  already running at NORMAL; explicitly setting it was a no-op. (Confirmed
  the pragma itself is real and matters when it isn't already the effective
  default: an isolated raw-driver benchmark forcing `FULL` vs `NORMAL`
  explicitly showed 373 µs/op vs 26 µs/op — 14× — but that headroom was
  never available here.) Lesson: always read back the actual `PRAGMA` value
  before crediting a DSN change with a win, and don't trust a driver's
  surface-level defaults without checking what it does internally.

- **Merge the two writes into one transaction** (`db.RecordRequestOutcome`,
  `internal/db/record_outcome.go`) — implemented. `StatsStore.UpdateFromService`
  and `UsageStore.RecordUsage` were split into a pure record-builder
  (`buildStatsRecordFromService`, `prepareUsageRecord`) and the DB write, so
  `RecordRequestOutcome` can build both records and `tx.Save`/`tx.Create`
  them inside one `gorm.DB.Transaction(...)` call when both stores share the
  same underlying `*gorm.DB` (always true in production — `StoreManager`
  wires every store to one shared connection; a `db != db` fallback exists
  for the never-exercised-in-production case of independently constructed
  stores). `usage_tracking.go`'s `updateServiceStats` and
  `recordDetailedUsage[WithTokenUsage]` now build-and-return instead of
  persisting inline; a new `persistRequestOutcome` calls
  `db.RecordRequestOutcome` once per request instead of two independent
  store calls.

  | Benchmark | separate transactions | merged transaction | change |
  |---|---|---|---|
  | `BenchmarkStatsAndUsage_Combined` vs. `_RecordRequestOutcome` | ~440 µs/op, 246 allocs | ~420 µs/op, 235 allocs | **~4–8%** (noisy across runs), 11 fewer allocs/op |

  Confirmed via pprof that the merge is real (`gorm.(*DB).Commit` appears
  once per call, not twice), but the win is modest: most of the ~440 µs
  isn't transaction-wrapper overhead, it's the two per-row statements
  themselves (`INSERT` + `UPDATE`, each its own cgo call into SQLite plus
  gorm's reflection/scan machinery) — removing one `COMMIT` doesn't remove
  either statement. Don't expect "halve the commits" to mean "halve the
  latency" when per-statement cost dominates per-transaction cost, as it
  does here. Kept anyway: the win is real (not regression, ~4–8% lower
  latency on the client-visible path, fewer allocations) and it adds a
  genuine correctness improvement beyond performance —
  `TestRecordRequestOutcome_AtomicRollback` (`internal/db/record_outcome_test.go`)
  proves the stats and usage rows for one request can no longer partially
  persist (previously two independent best-effort writes; a stats-store
  failure and a usage-store success, or vice versa, could silently
  disagree — now they're all-or-nothing).

**Still open** (option 1 from the original discussion, not attempted here):
batching/debouncing multiple requests' stats and usage writes into fewer,
larger transactions (e.g. an in-memory buffer flushed by a ticker or a
bounded channel + background writer) — the actual lever for the remaining
per-statement cost, since it amortizes the cgo/gorm overhead across N rows
instead of paying it per request. Marked complex/deferred in the original
discussion; the numbers above suggest it's the only remaining direction with
real headroom on this path, at the cost of async durability (a crash loses
whatever's still buffered) and added complexity (buffer, flush trigger,
backpressure if the writer falls behind).

**Also fixed in passing**: `NewUsageStore` never created the `db`
subdirectory `constant.GetDBFile` expects (every other `NewXStore`
constructor in this package does). Never hit in production because
`StoreManager` builds `UsageStore` directly over its own already-open
`*gorm.DB` — `NewUsageStore` had zero callers anywhere in the codebase until
`stats_usage_bench_test.go` became its first, which is how this surfaced.

## The pattern is systemic — audit of `internal/db/*_store.go`

Every store under `internal/db/` follows the same shape: a GORM-backed SQLite
table, one `sync.Mutex` (not `RWMutex`) per store, and every method —
including pure reads — takes the exclusive lock and issues a query. Only
`provider_model.go` (`ModelStore`) and `usage_record.go`/`store_manager.go`
already used `RWMutex`, and none of them cache. Whether this matters for a
given store depends entirely on whether it's read (or written) on the
**request hot path** (every inbound request) versus an **admin/UI path**
(occasional, human-paced) or the lower-QPS **IM-bot/remote-control message
path**:

| Store | Hot-path? | Where | Status |
|---|---|---|---|
| `ProviderStore` | **Yes (read)** | `ServiceSelector.Select` resolves a provider every request | **Fixed** — read cache |
| `APITokenStore` | **Yes (read + write)** | `AuthMiddleware.ModelAuthMiddleware` validates every API-token request and updates `last_used_at` | **Fixed** — read cache + debounced write |
| `StatsStore`, `UsageStore` | **Yes (write)** | `usage_tracking.go` persists stats + a usage row every completed request | **Partially fixed** — merged transaction; batching still open |
| `ImBotSettingsStore` | No | Bot lifecycle events (start/stop) and admin REST/CLI only | Not touched — genuinely low frequency |
| `ModelStore` | No | Admin "list/refresh models" REST endpoints, OAuth completion, CLI | Not touched — already `RWMutex`, admin-paced |
| `TaskStore` | **Dead code** | Zero callers anywhere in the repo; the real task subsystem uses `internal/task/store.go` instead | Not touched — flag for a separate cleanup ticket |
| `ToolConfigStore` | **Effectively unreachable** | The one method on the request-adjacent MCP-tool-config path (`Config.GetToolConfig`) is a different, in-memory-only implementation on `Config.ToolConfigs` that never touches this store | Not touched — flag for a separate cleanup ticket |
| `RemoteChatStore`, `RemoteSessionStore`, `BotAccessStore` | IM-bot message path (several round trips per inbound chat message) | `remote/control/remoteagent/handler_message.go` et al. | Not touched — real per-message traffic, but QPS is chat-message-rate, not LLM-API-rate, and none has an app-level mutex at all today (rely on gorm/SQLite's own WAL concurrency) |

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
