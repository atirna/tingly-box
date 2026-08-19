# OpenCode（Zen / Go）配额方案

> 适用对象：改 `ai/quota/fetcher/opencode.go` 或排查 OpenCode provider 额度显示的贡献者。
> 结构：调研（§1–§3）→ 我们能读到什么（§4）→ 归一实现（§5）→ 验证（§6）→ 待定（§7）。
> 调研来源：`sst/opencode`（现 `anomalyco/opencode`）`dev` 分支源码 —— 网关实现在
> `packages/console/app/src/routes/zen/**`，客户端错误处理在
> `packages/opencode/src/session/retry.ts`；文档 `opencode.ai/docs/zen`、`/docs/go`。

---

## 1. 一个 host、一把 key、两个产品

OpenCode 的网关（`packages/console/app/src/routes/zen/`）在同一个域名下开两组路由：

| 路由 | 产品 | 计费 |
|---|---|---|
| `/zen/v1/{chat/completions,messages,responses,models}` | Zen（按量付费） | 预充余额 balance |
| `/zen/go/v1/{...,usage}` | Go 订阅（内部代号 `lite`） | 订阅额度，超限拒绝 |

两组路由认的是**同一把 Zen API key**（`Authorization: Bearer <key>`），
key 背后是 workspace + user。所以「这把 key 属于哪个产品」不是配置出来的，
而是**看这个 workspace 有没有订阅**——这一点决定了 §5 的读取策略。

## 2. 四种计费源，网关按固定优先级判定

`validateBilling()`（`zen/util/handler.go` 对应的 `handler.ts`）返回
`anonymous | free | byok | subscription | lite | balance`，判定顺序即优先级：

1. **匿名 / free**：未带 key 或只用免费模型。限额是**按 IP 的每日请求数**
   （`zen/util/ipRateLimiter.ts`：UTC 日切，新 IP 在终身前 `dailyLimit*7` 次内享双倍日限），
   超限抛 `FreeUsageLimitError`。另有按 IP 的试用 token 池（`trialLimiter.ts`，`promoTokens`）。
2. **byok**：workspace 自带上游 key，网关不计费。
3. **subscription（Black，$20/$100/$200 档）**：一个滚动窗口 + 一个固定（周）窗口，
   超限抛 `BlackUsageLimitError`。
4. **lite（即 Go 订阅）**：**周 → 月 → 5 小时滚动**三个窗口依次校验，
   任一打满即抛 `GoUsageLimitError`。
5. **balance（按量付费）**：`balance <= 0` → `CreditsError`；
   workspace 月度上限 → `MonthlyLimitError`；成员个人月度上限 → `UserLimitError`。
   余额低于 `reloadTrigger`（默认 $5）自动充值（默认 $20），可关。

订阅档位开启 `useBalance` 时，订阅超限会**回落到余额**继续跑而不是拒绝
（`catch (e) { if (!...useBalance) throw e }`）。

### 2.1 限额是钱，不是 token

`Subscription.analyze{Rolling,Weekly,Monthly}Usage()`（`console/core/src/subscription.ts`）
把用量和限额都换算成 **microcents** 比较，返回 `{status, resetInSec, usagePercent}`：

- 滚动窗口：窗口长度 `rollingWindow`（小时）；`timeUpdated` 早于窗口起点即视为归零；
  窗口末端 = 最后一次计费时间 + 窗口长度（**不是自然滑动**，是"最后一次用之后再等一个窗口"）。
- 周窗口：自然周边界。
- 月窗口：**从订阅日起算**的账期（`getMonthlyBounds(now, timeSubscribed)`），不是自然月。
- `usagePercent` 向下取整；打满返回 `status: "rate-limited"`。

具体限额数值来自部署期资源 `ZEN_LIMITS`（`free.dailyRequests` / `lite.rollingLimit` /
`lite.rollingWindow` / `lite.weeklyLimit` / `lite.monthlyLimit` / `black.*`），
**源码里没有硬编码值，API 也不返回**——这是 §4 的关键约束。

## 3. 超限时客户端拿到什么

网关统一返回 **429**，body：

```json
{"type":"error","error":{"type":"GoUsageLimitError","message":"..."},
 "metadata":{"workspace":"wrk_...","limitName":"5 hour"}}
```

`retry-after`（秒）来自 `resetInSec`。`limitName` ∈ `"5 hour" | "weekly" | "monthly"`。
`FreeUsageLimitError` / `BlackUsageLimitError` 不带 metadata。
认证与计费类错误（`AuthError` / `CreditsError` / `MonthlyLimitError` / `UserLimitError` /
`ModelError`）走 **401**，`RegionError` / `DataPolicyError` 走 403。

客户端 `session/retry.ts` 据此渲染：`FreeUsageLimitError` → 引导订阅 Go；
`GoUsageLimitError` → "X usage limit reached, resets in ..."，链接到
`opencode.ai/workspace/<id>/go`。

## 4. 我们能主动读到的只有一个端点

```
GET https://opencode.ai/zen/go/v1/usage
Authorization: Bearer <zen api key>

{"usage":{
  "rolling": {"status":"ok","percent":12,"resetsAt":"..."},
  "weekly":  {"status":"ok","percent":40,"resetsAt":"..."},
  "monthly": {"status":"ok","percent":31,"resetsAt":"..."}}}
```

三条约束，直接决定了归一方式：

1. **只有百分比。** 限额（美元）与已用量都不返回，只给 `percent` + `resetsAt`。
   所以窗口只能落在 0-100 标度上（`Unit=percent`，`Limit=100`），
   跟 codex / anthropic 同一路数。
2. **窗口长度不返回。** `resetsAt` 是"这次什么时候回来"，不是周期长度
   （未用过时才等于整窗）。滚动窗口按网关自己在超限文案里给的名字取 **5 小时**；
   月窗口按 30 天记（真实边界以 `resetsAt` 为准）。
3. **余额没有公开端点。** 只有订阅有 `usage`；按量付费的 balance 至今只能看 web 控制台
   （上游 issue #10448 仍开着）。因此**没有 Go 订阅的 key** 会拿到
   `403 EntitlementError: OpenCode Go subscription required.`——
   这是一把好 key，不是错误。

## 5. 归一到 `ai/quota`

`ai/quota/fetcher/opencode.go`（`ProviderTypeOpenCode = "opencode"`，`api_key` 认证）：

| 上游 | 窗口 | 说明 |
|---|---|---|
| `usage.rolling` | `rolling`，`session`，300min | `Kind=limit`；`ResetsAt` 来自上游 |
| `usage.weekly` | `weekly`，`weekly`，7d | 同上 |
| `usage.monthly` | `monthly`，`monthly`，30d | 账期从订阅日起算，长度只是名义值 |

- 三个窗口都是**账户级 gate**（网关任一打满即拒），所以都进 `Windows`，
  由 `Tightest()` 选出真正卡住的那个——不是"取周窗口"或"取最先重置的"。
- `status: "rate-limited"` → `LimitReached=true` / `Allowed=false`。
  百分比是向下取整的，100% 分不出"刚好用尽"和"正在被拒"，只有 `status` 分得出。
- **403 EntitlementError → `unreadableUsage`**（记 `LastError`，不产出窗口）。
  报错会让用户去修一把没坏的 key；报 0% 则是撒谎——按 §5.3 的不变量，
  读不到就得说读不到。其余非 200 才是真错误，并把上游 message 带进错误文本。
- provider 的 `APIBase` 配的是推理端点（`…/zen/v1` 或 `…/zen/go/v1`），
  usage 挂在 host 根上，用 `apiRoot()` 先削前缀（长后缀优先，`/zen/go/v1` 也以 `/v1` 结尾）。
- `manager.go` 的 host 推断：`opencode.ai` → `ProviderTypeOpenCode`，两个产品不按 path 分——
  fetcher 本身就能回答"这把 key 有没有订阅"。

免费额度（按 IP 日限）与按量付费余额**不产出窗口**：前者的口径是 IP 不是 key，
我们无从查询；后者上游没端点。两者都只会以 429 / 401 的形式在推理链路上出现。

## 6. 验证

- `ai/quota/fetcher/opencode_test.go`：三窗口归一（周为最紧）、`rate-limited` 落到
  `LimitReached/Allowed`、403 走 unreadable、401 报错、`/zen/v1` 前缀削除后打到
  `/zen/go/v1/usage`、Bearer 头。
- `taskfile_samples_test.go` + `build/Taskfile.quota.yml`（`task -t build/Taskfile.quota.yml opencode`）
  钉住样本归一结果，并记录真实抓取命令。
- 每个用例都过 `checkInvariants`（`.design/quota-semantics.md` §5.3）。

## 7. 待定

1. **滚动窗口长度是配置项**（`ZEN_LIMITS.lite.rollingWindow`），我们写死 300min。
   若上游改档位，窗口排序会偏——真出问题前不值得为它加配置项。
2. **Black 档（$20/$100/$200）没有 usage 端点**，只有 429 才知道。
   若上游补上 `/zen/black/v1/usage` 之类，按 §5 同款接入即可。
3. **余额**：等上游 issue #10448 落地 `GET /zen/v1/balance` 后，
   按 `WindowTypeBalance` + `WindowKindResource` 补一个资源窗口（对齐 openrouter 的做法）。
4. **`useBalance` 回落**：订阅打满但开了回落时，请求其实仍能跑。
   `usage` 端点不返回这个开关，所以 100% 的窗口可能"看着卡住实际能用"。
   要修得等上游给出该字段。
