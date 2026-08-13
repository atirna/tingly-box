# Database Layer (`internal/db`)

Design notes for `internal/db`'s stores — SQLite via gorm, one shared
`*gorm.DB` owned by `StoreManager`.

## Architecture (rules established by the holistic 2026-08 pass)

A whole-layer audit (all of `internal/db`, every SQLite open outside it,
and every store call site) found the layer had grown by copy-paste: three
independent GORM connection pools against one `tingly.db`, the store set
hand-enumerated in four places, six of eleven stores constructed by
struct-literal bypass, four dead subsystems still migrated on every boot,
and one migration step that only ran on the test path. The pass distilled
into rules; hold new code to them:

1. **One connection per database file per process.** `StoreManager` owns
   the only production connection to `tingly.db` and exposes it via
   `StoreManager.DB()` for subsystems that keep their own record types on
   the shared file (`ai/quota` borrows it via `quota.NewGormStoreOverDB`;
   the model-list subsystem wraps `StoreManager.Model()` via
   `data.NewProviderModelManagerWithStore`). A standalone
   `NewXStore(baseDir)` opening its own connection is for CLI commands and
   tests only. `guardrails.db` is a separate file by design (separate
   security domain), but follows the same rules: canonical DSN options and
   one memoized store per path (`config.CredentialStore`).
2. **All connection opening goes through `internal/db/open.go`.**
   `sqliteDSN`/`openSQLite`/`openTinglyDB` are the only places the DSN
   option set (`_busy_timeout`, `_journal_mode=WAL`, `_foreign_keys=1`)
   and directory creation are spelled out. Never hand-type the DSN again.
3. **Every store embeds `storeConn`** (`db *gorm.DB` + `ownsDB bool`) and
   has a `newXStore(conn storeConn)` seam that does migrate + init.
   `NewXStore(baseDir)` = `openTinglyDB` + `ownedConn`;
   `StoreManager.initXStore` = the same seam + `borrowedConn`, so the two
   init paths cannot drift (the `ensureUsageRecordSchema` drift was exactly
   this failure mode). `Close` on a borrowed connection is a no-op — a
   store can never tear the shared handle down.
4. **The store set is spelled out in two places only**: the `storeSet`
   struct and `storeSet.initialized()`. `Close` resets stores with one
   zero-value assignment; `HealthCheck` iterates `initialized()` and pings
   the shared connection once.
5. **Migrations run from the shared constructor seam**, never only from
   `NewXStore`. Dead tables get dropped in
   `StoreManager.dropDeprecatedTables` (currently: `model_capabilities`,
   `tasks`, `tool_configs`, `usage_monthly`).
6. **Hot-path writes go through `StoreManager.RecordOutcome`** (batched;
   see below), not per-store write methods.

## Performance

Status: `ProviderStore` and `APITokenStore` (read-cache fixes),
`StatsStore`/`UsageStore` (transaction-merge fix), and batched outcome
writes (`StoreManager.RecordOutcome` + `outcomeWriter`) are all shipped.

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

**Batching (shipped)**: `StoreManager.RecordOutcome`
(`internal/db/outcome_writer.go`) is now the production entry point.
Records are built on the request goroutine (the stats record snapshots
live counters; the usage timestamp is completion time), then enqueued to a
flusher that commits up to 128 outcomes per transaction at most 1s apart.
Stats rows dedupe within a batch to the newest snapshot per
provider:model; usage rows are all kept. Bounded queue (1024), no drops:
full-or-closed degrades to the synchronous `RecordRequestOutcome`.
`StoreManager.Close` drains and flushes before closing the connection, so
the crash-loss window is the flush interval (1s) — the accepted trade.

| Benchmark | Result |
|---|---|
| `BenchmarkStatsAndUsage_Combined` (pre-merge baseline) | ~332 µs/op, 246 allocs |
| `BenchmarkStatsAndUsage_RecordRequestOutcome` (merged txn) | ~300 µs/op, 235 allocs |
| `BenchmarkStatsAndUsage_RecordOutcomeBatched` (shipped) | **~25 µs/op, 33 allocs** |

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
| `StatsStore`, `UsageStore` | Yes (write) | `usage_tracking.go` persists stats + a usage row every completed request | Fixed — merged transaction, then batched writer (`RecordOutcome`) |
| `ImBotSettingsStore` | No | Bot lifecycle events, admin REST/CLI only | Not touched — genuinely low frequency |
| `ModelStore` | No | Admin "list/refresh models" endpoints, OAuth completion, CLI | Now shared via `StoreManager.Model()` (was a second connection) |
| `TaskStore` | Dead code | Zero callers; the `internal/task` subsystem it implemented is itself never constructed | **Removed** (table dropped) |
| `ToolConfigStore` | Effectively unreachable | `Config.GetToolConfig` is a different, config-file-backed implementation that never touched this store | **Removed** (table dropped; `ToolTypeMCPRuntime` moved to `internal/server/config`) |
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

## Remaining backlog (found by the 2026-08 audit, not yet done)

Ranked by expected value; each is independent.

1. **IM path: per-message chat-row caching.** One inbound bot message
   issues ~6–10 identical `GetChat` SELECTs across the dispatch chain
   (`DisabledChatGate`, `AuthorizationGate`, `HandleMessage`, then
   `GetProjectPath`/`GetBashCwd`/... each re-fetching the same row —
   `remote/control/bot`, `remote/control/remoteagent`). A `*bot.Chat`
   fetched once into the handler context collapses them; pure call-site
   change, `RemoteChatStore` needs no cache. Related N+1s:
   `BotAccessStore.ListGroupActors` (2 queries per actor) and
   `ResolveRoute` (a `First()` per candidate route).
2. **`GetPerformanceSummary` computes percentiles in Go**
   (`usage_record.go`): loads every matching success row into memory and
   sorts — unbounded on a dashboard endpoint, and the only usage read with
   no `usage_daily` fast path.
3. **Daily aggregation runs lazily inside dashboard queries**
   (`ensureDailyAggregates` under the write lock, one transaction per
   missing day) — a cold-start query spanning many days aggregates them
   serially in-request. A scheduled job calling the already-written
   `AggregateToDaily` moves it off the read path. Same job should own
   retention: nothing calls `DeleteOlderThan` except the manual REST
   endpoint, so `usage_records` grows unbounded; and
   `APITokenStore.CleanupExpiredTokens` has no caller either.
4. **`config.json` has two writers** (`internal/server/config.Config.Save`,
   non-atomic, 0644; `internal/config/app_config.go`, atomic, 0600) with
   divergent formatting and no locking — consolidate on one atomic writer.
   Related: guardrails history (`utils/history.go`) rewrites its whole
   capped JSON array on every guardrails evaluation — per-LLM-request file
   I/O that belongs next to `usage_records` in SQLite.
5. **Error-dialect inconsistency across stores**: `fmt.Errorf` strings
   (provider/token/imbot), sentinel errors (`remote_chat_store`,
   `bot_access_store`), and silent zero-value returns (`provider_model`,
   parts of `service_stat`). Callers can only distinguish "not found" for
   the sentinel stores. Converge on sentinels when a store is next touched.
6. **The usage column list exists in five SQL strings**
   (`usage_record.go` ×2, `usage_daily.go` ×3): adding a token dimension
   means editing five queries plus the scan structs. Extract a shared
   column-list builder when the next dimension lands.
7. **Server shutdown order**: `StoreManager.Close` runs before
   `httpServer.Shutdown`, so in-flight requests can hit closed stores
   (clean errors, but still backwards).
8. **Two known consistency windows from outcome batching** (accepted for
   the ~12× hot-path win; close them if they ever start to matter):
   - *Config hot-reload can regress in-memory stats by ≤1 flush interval.*
     `HydrateRules` re-seeds `service.Stats` from `service_stats` on every
     config reload; outcomes still buffered in the writer aren't in the
     table yet, so the rebuilt counters can be up to 1s behind the traffic
     the old rule objects had already counted. Self-correcting window
     counters, no user-visible effect today. Fix if needed: flush the
     writer before `RefreshStatsFromStore` (a `StoreManager` method that
     calls `outcomeWriter`'s flush synchronously).
   - *Admin "clear stats" can be resurrected by a pending flush.*
     `ClearAllStats`/`ClearServiceStats` deletes rows, but a snapshot
     buffered before the clear re-Saves the old cumulative counts up to 1s
     later. Same fix shape: flush (or drop matching buffered outcomes)
     before the delete. Note any future reader that needs
     read-your-writes on `usage_records`/`service_stats` (e.g. real-time
     quota enforcement) must NOT read the tables directly — add an
     explicit writer flush or read the in-memory stats instead.
