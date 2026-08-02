# OpenAI Endpoint Routing 设计

> 适用对象：tingly-box 后端贡献者，特别是改 `internal/server/protocol_endpoint.go` 或 provider/template 类型时。
> 本文档描述「客户端发请求 → gateway 选 OpenAI 上游 endpoint」的最终设计。
>
> **2026-08-02 修订**：声明模型从单一枚举 `OpenAIEndpointMode`（`""`/`chat`/`responses`/`both`）改为按端点显式声明的 `OpenAIEndpoints []OpenAIEndpoint`（`chat_completions`/`responses`）。`both` 把"上游有哪些端点"这个事实和"两个都有时选哪个"这条路由策略糅进了一个词，本文档 §2.3 曾把这当作最终设计，现已识别为同一类错误的又一次发生(结构事实里混入了运行时决策)。详见 `.design/provider-responses-native-endpoint.spec.md`。本文档下方按新模型改写；声明还从"用户不可编辑"改为一等可编辑字段(provider 编辑 UI 里两个独立 checkbox)，模板负责预填而非静默快照，见 §6。

---

## 1. 问题域

OpenAI 兼容生态里有两种 endpoint 形态：

| Endpoint | 提供方 |
|---|---|
| `/v1/chat/completions` | 几乎所有 OpenAI-compat 厂商（Qwen、Deepseek、Mistral、GLM、MiniMax、xAI、本地 vLLM/llama.cpp 等）+ OpenAI 官方 |
| `/v1/responses` | 仅 OpenAI 官方（gpt-5、o-series 等）+ Codex |

Provider 实际能力组合**只有三种**：

| 类型 | 例子 | Chat | Responses |
|---|---|:---:|:---:|
| Chat-only | Qwen / Deepseek / Mistral / 本地模型 / 绝大多数厂商 | ✅ | ❌ |
| Responses-only | Codex | ❌ | ✅ |
| Both | OpenAI 官方 | ✅ | ✅ |

Gateway 收到客户端的 request 时（无论入站协议是 OpenAI Chat / OpenAI Responses / Anthropic Messages 经转换后等价的 OpenAI 形态），**必须知道**：上游用哪一个？

---

## 2. 历史与教训

### 2.1 Adaptive 时代（已移除，PR #976）

`AdaptiveProbe` 在 cold-start 时同时探两个 endpoint，缓存结果，运行时按缓存路由。

代价：
- 首次请求阻塞 10s
- 每次 probe 烧真实 token
- 单次失败即标"不可用"，不可重试
- 永远不可能 100% 准确（速率限制、临时 5xx 都会污染缓存）
- 整体黑盒，故障难诊断

最早就引入了 deprecated 注释（"use per-request routing decisions instead"）。

### 2.2 失败的过渡：负向声明（dead-end）

PR #976 中间一版试过这个补丁：

```go
// 错误设计
type Provider struct {
    ResponsesOnly bool  // 标记 Codex
    ChatOnly bool       // 补丁：标记常见 Chat-only 厂商
}
```

`ChatOnly` 是补丁，因为默认行为还是错的——**默认 mirror 入站**，本质上沿袭了 Adaptive 的乐观假设「上游也支持客户端发的协议」。这导致：
- Codex 客户端发 `/v1/responses` → 路由到任意非-Codex 上游 → mirror 到 Responses → 404
- 用户必须显式 set 一个否定标志才能避免 bug

根本错误：**正确语义应该是 positive declaration**——provider 显式声明"我支持 Responses"，没声明就默认 Chat。

### 2.3 单一 enum 的尝试与教训（已废弃）

2026-05 版本采用了单一 enum：

```go
type OpenAIEndpointMode string

const (
    EndpointModeUnknown   OpenAIEndpointMode = ""           // 未声明，按 Chat 处理
    EndpointModeChat      OpenAIEndpointMode = "chat"       // 绝大多数厂商
    EndpointModeResponses OpenAIEndpointMode = "responses"  // Codex
    EndpointModeBoth      OpenAIEndpointMode = "both"       // OpenAI 官方
)
```

三种状态 1:1 映射 §1 表格的三类 provider，当时认为是最终设计。

但 `both` 这个值本身不是一条事实——它是"两个端点都支持 + 入站决定选哪个"的合成语义。声明层（"上游有哪些端点"）里悄悄夹带了一条路由策略（"两个都有时怎么选"）。这与 §2.2 "负向声明"翻的是同一个跟头:**结构事实里混入了运行时决策**,只是这次伪装得更隐蔽——`chat`/`responses`两个值确实是纯事实,只有第三个值`both`带了策略。后果:

- UI 上只能做成一个三选一的 mode picker,而不是两个独立 checkbox——`both` 不是"第三种状态",是"前两种状态都为真";
- 厂商文档说"我们支持 X"时,配置里要先理解三个模式词的隐含行为才能填对;
- 不可扩展:未来任何"支持组合 × 选择策略"的新需求都得再造枚举值。

2026-08 改为 §2.4 的按端点显式声明,`both` 的 mirror 行为被 resolver 里的"原生优先"规则**推导**出来,不再是某个值自带的隐含语义。

### 2.4 当前设计:按端点显式声明 + Chat 默认

```go
type OpenAIEndpoint string

const (
    OpenAIEndpointChat      OpenAIEndpoint = "chat_completions"
    OpenAIEndpointResponses OpenAIEndpoint = "responses"
)

// Provider.OpenAIEndpoints []OpenAIEndpoint —— 每个值是一条独立、可验证的事实
```

声明只回答"上游实现了哪个端点",不回答"两个都有时选哪个"——后者完全交给 §4 的 resolver 显式表达。`nil`/空切片 = 未声明,按 Chat 处理,与旧设计的 `""` 语义一致。

---

## 3. 关注点分层（partition）

Endpoint 路由相关的状态/决策被刻意拆成两层，每层承担一个 well-defined 的责任。**不要**把任何一层的事情塞到另一层去做——这是 Adaptive 时代失败的根因（把"结构事实"埋进了运行时缓存）。

```
┌─────────────────────────────────────────────────────────────┐
│  Layer 2  Rule flag (extension)  —  per-rule customization   │
│           openai_endpoint_override = auto/chat/responses    │
│           其他 rule flag：cursor_compat、skip_usage、…       │
├─────────────────────────────────────────────────────────────┤
│  Layer 1  Provider 声明  —  structural fact                  │
│           Provider.OpenAIEndpoints = []{chat_completions,   │
│           responses} 的子集                                 │
│           模板预填 / OAuth 实例化 / 用户在 provider 编辑    │
│           页勾选（见 §6，2026-08 起为可编辑字段）            │
└─────────────────────────────────────────────────────────────┘
```

「Request-shape guard」**不是**第三层。Responses-only 字段（`previous_response_id`、`include`、`reasoning` 等）在路由到 Chat 上游时由转换函数 `ConvertOpenAIResponsesToChat` 静默丢弃，与 Anthropic→Chat 降级时静默丢弃 thinking blocks / vision details 的行为完全一致。用户在 provider 声明上的选择已经承担了这个 trade-off，gateway 不再做二次裁决。

### Layer 1: Provider 声明 —— 结构性事实,但由用户维护

回答的问题：**这个 provider 实际上能听懂什么 endpoint？**

- 来源：template 预填（实例化时,前端从模板的 `openai_endpoints` 抄一份到表单,可见可改)、OAuth issuer 推断(Codex,用户不接触)、或用户在 provider 编辑页手动勾选(自建/自定义 provider 的唯一入口);
- **用户可编辑**——这是 2026-08 相对旧版的修正:端点支持虽是上游的客观事实,但 gateway 不可能替所有自定义上游代答,知道事实的是用户。"结构性事实"描述的是它的性质(不是 per-rule 偏好),不代表用户不能碰;
- 单一来源、单一字段(`Provider.OpenAIEndpoints`),每个端点一个独立的布尔位,无 ambiguity。

判定属于这一层的标志：**改变这个值需要上游 API 本身的能力发生改变**(厂商升级 API、用户换 provider)——而不是"谁能编辑它"。

### Layer 2: Rule flag —— per-rule 定制（extension）

回答的问题：**这条 rule 想要什么特殊行为？**

- 实现：`typ.RuleFlags` 结构 + `internal/typ/flag_registry.go` 的 catalog
- 当前关于 endpoint 的 flag：`openai_endpoint_override`（auto / chat / responses）
- 其他 rule flag 与 endpoint 无关（`cursor_compat`、`skip_usage`、`use_max_completion_tokens` …）但**走相同的 extension 机制**

判定属于这一层的标志：**只对某条 rule / 某类客户端 / 某种调试场景有意义**，做成 provider 字段会污染默认，做成 scenario flag 又过于粗粒度。详见 `.design/rule-flags.md`。

### 两层冲突时的优先级

| 场景 | 谁赢 | 理由 |
|---|---|---|
| Rule flag 指定 `chat`，Provider 声明含 `responses` | **Rule flag 赢** | override 是 per-rule 显式选择，用于调试或特殊客户端适配 |
| Rule flag 指定 `responses`，Provider 未声明 `responses` | **Rule flag 赢** | 同上；调用方显式接受上游不支持时可能产生的错误 |
| Provider 声明两个端点、入站 Chat | 原生服务(Chat) | 入站协议命中声明集合时优先原生,不转换 |
| Provider 声明两个端点、入站 Responses | 原生服务(Responses) | 同上 |

具体决策表见 §4.2。

### 何时应当新增一个 rule flag

如果未来出现某种"针对部分 rule 的 endpoint-routing 微调"需求（例如：某条 rule 强制走 streaming-only 通道、某条 rule 启用 Responses 的特殊参数），**首选** 把它做成新 rule flag：

1. 在 `typ.RuleFlags` 加字段
2. 在 `RuleFlagRegistry()` 注册 metadata（label / description / category）
3. 在相应的 transform / handler 消费

**不应当** 把这种需求塞进 `Provider.OpenAIEndpoints`——那是 structural layer,扩张它会复活 Adaptive 时代的混乱。同理,模型粒度的端点限制(如某个模型暂不支持 responses)也不应做成 provider/template 的数据字段——那是"针对部分 rule"的需求,直接用已有的 rule flag 逃生舱(见 `.design/provider-responses-native-endpoint.spec.md` 关于模型级 `endpoints` 元数据被撤销的记录)。

---

## 4. Resolver 行为

`ResolveOpenAIEndpoint(provider, ruleFlags, incoming) → (protocol.APIType, error)` 定义在 `internal/server/protocol_endpoint.go`。**纯函数**：不读 Server 状态、不发 I/O。

### 4.1 precedence（高 → 低）

1. **Rule flag `OpenAIEndpointOverride`**（Layer 2，用户每条 rule 可设）
   - `""` / `"auto"` / 未知值 → 当作未设置
   - `"chat"` 或 `"responses"` → 显式 override
2. **原生优先**：入站端点若在 `provider.OpenAIEndpoints` 声明集合内，直接用（passthrough，不转换）
3. **转换降级/升级**：否则若 provider 声明了另一个端点，路由到该端点并转换请求
4. **保守默认**：都没声明 → Chat

Rule override 优先于 provider 声明；`OpenAIEndpointOverride` 与声明冲突时不再降级为 provider 声明，也不记录 ignored warn。

### 4.2 决策表

| Override | 声明 | 入站 Chat | 入站 Responses |
|---|---|---|---|
| 无 | 缺省 / `[]` | Chat | Chat（降级转换） |
| 无 | `[chat_completions]` | Chat | Chat（降级转换） |
| 无 | `[responses]` | Responses（升级转换，Codex） | Responses |
| 无 | `[chat_completions, responses]` | **Chat（原生）** | **Responses（原生）** |
| `=chat` | 任意 | Chat | Chat |
| `=responses` | 任意 | Responses | Responses |

旧版 `both` 的 mirror 行为，在这张表里是"声明双端点"那一行——由"原生优先"规则推导出来，不是某个枚举值自带的含义。

### 4.3 为什么默认是 Chat

- 生态现实：绝大多数 OpenAI-compat 厂商只实现 `/chat/completions`
- 未声明时 mirror 入站等于继续相信「上游也支持客户端发的协议」——这就是 Adaptive 时代的乐观假设
- Chat 默认 + 显式声明 Responses 是 safe failure mode：未声明的 provider 永远走通用 endpoint，不会 404

---

## 5. Codex 的处理

Codex 是 OAuth-only 接入路径（Web `oauth/handler.go` 和 CLI `command/oauth.go`），实例化时通过 `ai.OpenAIEndpointsForIssuer(issuer)` 直接填入 Provider 结构体——目前该 helper 只对 `IssuerCodex` 返回 `[]OpenAIEndpoint{OpenAIEndpointResponses}`，其他 issuer 返回 `[]OpenAIEndpoint{OpenAIEndpointChat}`。两条 OAuth 路径共用同一 mapping。

无需用户配置。OAuth 完成即正确。Codex provider 的声明不在 UI 里暴露编辑（走 OAuth 专属路径，不经过 §6 的 provider 编辑表单）。

存量 Codex provider（PR #976 之前已 OAuth 完成的）由 `migrate20260518` backfill。Idempotent。2026-08 声明模型换字段后，该 migration 已跟随改写为写入 `OpenAIEndpoints`（见 `internal/server/config/migration_codex_endpoint_mode.go`），语义不变；新增的 `migrate20260802` 负责把任何仍带旧枚举列的行转换到新字段（纯格式转换，不做能力 backfill，见 §9）。

`ai.Provider.IsCodexProvider()` 方法**保留**——它仍被 client、UA pin、system message 注入等非路由代码消费。本文档讨论的 endpoint 声明只与路由相关。

---

## 6. Template 与 Provider 字段

Template 是用户实例化 provider 的预设入口。Template 里的 `openai_endpoints` 数组在**前端**层面预填进 provider 表单的两个独立 checkbox（Chat Completions / Responses），用户创建前可见、可改；提交时随 `CreateProviderRequest.OpenAIEndpoints` 一并发给后端校验持久化，不是后端悄悄从 template 快照。

`providers.json` 现状：
| Template | `openai_endpoints` |
|---|---|
| `openai-com` | `["chat_completions", "responses"]` |
| `codex` | `["responses"]` |
| `deepseek-com` | `["chat_completions", "responses"]`（DeepSeek 官方 Responses API，2026-08 起声明） |
| 其他（Qwen / GLM / ...）| 未设（= 空，resolver 按 Chat 处理）|

用户**可以在 provider 编辑 UI 里改声明**——两个独立 checkbox，不是 mode picker。模板只负责预填默认值，不锁死。后续若发现某个 Chat-only template 该加 `responses`（如某厂商新上线官方 Responses API），在 `providers.json` 加一个值即可，新建 provider 立即预填；存量 provider **不自动 backfill**（评审决议，见 `.design/provider-responses-native-endpoint.spec.md` §9），用户在编辑页手动勾选即可启用。

---

## 7. 客户端协议 → 上游 endpoint 全链路

完整端到端转换矩阵（仅 OpenAI-API-style provider；Anthropic / Google provider 走各自原生路径）：

| 客户端入站 | Provider 声明 | 上游 | 入站→上游 转换 | 上游→客户端 转换 |
|---|---|---|---|---|
| OpenAI Chat | `[chat_completions]` | Chat | passthrough | passthrough |
| OpenAI Chat | `[responses]` | Responses | `ConvertOpenAIChatToResponses`（建中）* | `buildChatPayloadFromResponses` |
| OpenAI Responses | `[chat_completions]` | Chat | `ConvertOpenAIResponsesToChat` | `buildResponsesPayloadFromChat` |
| OpenAI Responses | `[responses]` | Responses | passthrough | passthrough |
| OpenAI Responses | `[chat_completions, responses]`（原生优先）| Responses | passthrough | passthrough |
| Anthropic Messages | `[chat_completions]` | Chat | `ConvertAnthropicToOpenAIRequest` | `ConvertOpenAIToAnthropicResponse` |
| Anthropic Messages | `[responses]` | Responses | Anthropic→Responses 直转 | `streamResponsesToAnthropic*` |
| Anthropic Messages | `[chat_completions, responses]`（原生优先）| Responses | （同上）| （同上）|

Anthropic 入站在 resolver 里固定被当作 Responses 入站处理（见 `anthropic_message.go` 的调用点）——这是隐藏在调用点的一条策略，本文档记录但不在此展开；`[chat_completions, responses]` 声明下 Anthropic 入站因此总是走 Responses 上游，不受"原生优先"表面语义的字面误导。

(*) Chat-in / Responses-out 路径在 `protocol_dispatch.go:streamOpenAIChatToResponses` / `nonstreamOpenAIChatToResponses`。注意：当前 dispatch 里这两个函数被命名为 `streamResponsesToChat` / `nonstreamResponsesToChat`，方向写反了，pre-existing issue。

---

## 8. 关键文件

- `ai/provider.go` —— `OpenAIEndpoint` 类型 + 常量 + `Provider.OpenAIEndpoints` 字段 + 旧枚举兼容转换（`OpenAIEndpointsFromLegacyMode`）+ 校验（`ParseOpenAIEndpoints`）
- `internal/data/provider_template.go` —— `ProviderTemplate.OpenAIEndpoints`（`[]string`）+ 旧字段兼容读取
- `internal/data/providers.json` —— 出厂 template 的声明
- `internal/server/protocol_endpoint.go` —— `ResolveOpenAIEndpoint` 纯函数 + `EndpointOverride` 枚举与 `ParseEndpointOverride`
- `internal/server/module/provider/types.go`、`handler.go` —— Create/Update API 承载 `openai_endpoints`，词汇表校验
- `frontend/src/components/ProviderFormDialog.tsx` —— 两个独立 checkbox + 模板预填
- `internal/server/openai_responses.go`、`internal/server/anthropic_message.go` —— Responses / Anthropic 入站的路由调用点
- `internal/server/module/oauth/handler.go`、`internal/command/oauth.go` —— Codex OAuth 实例化写声明
- `internal/server/config/migration_codex_endpoint_mode.go` —— 存量 Codex backfill 迁移（`migrate20260518`）
- `internal/server/config/migration_openai_endpoints.go` —— 旧枚举列格式转换（`migrate20260802`，纯格式转换非能力 backfill）
- `internal/data/db/provider_store.go` —— DB 列 `openai_endpoints`（CSV），旧列 `openai_endpoint_mode` 读兼容、写时清空

---

## 9. 升级与兼容性

PR #976 引入 mode-based 设计；2026-08 改为按端点显式声明（`.design/provider-responses-native-endpoint.spec.md`）。涉及行为变更的点：

1. **PR #976：默认从 mirror 变 Chat**：手搓的 OpenAI-proper provider（没用 `openai-com` template）若依赖 `/v1/responses` 直通上游，需手动加声明。彼时 migration 不能自动处理这种情况（provider 没有 template_id 痕迹），文档化警告即可。
2. **PR #976：Codex 存量**：`migrate20260518` 自动 backfill，无感知。
3. **2026-08：声明模型换字段**：`migrate20260802` 把存量 `openai_endpoint_mode` 行转换为 `openai_endpoints`，纯格式转换，行为不变（幂等）。
4. **2026-08：不做能力 backfill**（评审决议）：即使某个 template（如 `deepseek-com`）新增了 `responses` 声明，**存量**由该 template 创建的 provider 不会自动获得这条声明——只有新建 / 重新实例化的 provider，或用户在编辑页手动勾选，才会带上它。这避免了任何存量路由路径被静默翻转（例如 claude-code 场景下 openai-style provider 从 Chat 转到 Responses）。

后续若新增 provider 类型或端点形态，原则不变：默认 Chat，需要的端点就显式声明；模型粒度的差异不进 provider/template 数据，用 rule flag 处理（§3）。

---

## 10. 不在本文档范围

- Anthropic / Google provider 的路由（走各自原生 endpoint，不进 OpenAI resolver）
- Smart routing / load balance 选哪个 service（在 endpoint 选择之前）
- vmodel loopback（独立处理）
- `IsCodexProvider()` 在 client 层的用法（UA pin、system message 注入等 quirk）
