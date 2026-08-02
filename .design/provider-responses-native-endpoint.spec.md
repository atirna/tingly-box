# Spec: Provider 定义按端点显式声明 OpenAI 支持(DeepSeek first)

> Status: Accepted(2026-08-02 评审定稿)
> Date: 2026-08-02
> 定稿决议:开放问题 1 → **不做域名 backfill**。存量 provider 只做旧枚举 → 新字段的等价格式转换(行为不变);responses 能力仅对新建实例生效(模板预填 / 用户勾选)。存量用户想启用,在 provider 编辑页勾选即可。
> Related: `.design/openai-endpoint-routing.md`, `.design/rule-flags.md`, `.design/ux-principles.md`, PR #976
> v2 变更:① 废弃 `openai_endpoint_mode` 枚举(`both` 语义混浊),改为按端点显式声明 `openai_endpoints`;② 声明升级为一等可编辑字段,前端独立勾选,模板实例化自动预填。

---

## 1. Motivation

DeepSeek 官方已上线 OpenAI Responses API([create-response](https://api-docs.deepseek.com/api/create-response)、[responses_api 指南](https://api-docs.deepseek.com/zh-cn/guides/responses_api))。当前 tingly-box 把 DeepSeek 当作 Chat-only 厂商:codex 场景(入站即 Responses 协议)路由到 DeepSeek 时走 `ConvertOpenAIResponsesToChat` 降级,丢弃 `reasoning`、`include` 等 Responses-only 语义。

既然厂商官方支持了,gateway 应通过 **provider 配置预定义**识别这一能力并**优先使用原生 `/responses`**:passthrough 保真(reasoning items、web_search tool、语义化 SSE 全部原样透传),消除有损转换。

落地过程中暴露了现有声明模型的两个缺陷,一并修正:

- `openai_endpoint_mode` 的 `both` 是把能力事实和路由策略混在一起的模式词(§2.2 Gap 0);
- 声明被定义为"用户不可编辑、仅模板快照",但快照链路实际从未接线,且自定义 provider 用户根本无法表达"我的上游支持 responses"(§2.2 Gap B)。

## 2. 现状与差距

### 2.1 现有机制

| 机制 | 位置 | 处置 |
|---|---|---|
| `OpenAIEndpointMode` enum(`""`/`chat`/`responses`/`both`) | `ai/provider.go:216` | **废弃重造**(Gap 0) |
| `ResolveOpenAIEndpoint` 纯函数(rule flag > provider mode,`both` 时 mirror 入站) | `internal/server/protocol_endpoint.go:49` | 保留框架,判定改读端点集合 |
| 模板字段 `openai_endpoint_mode` | `internal/data/provider_template.go:123` | 替换为 `openai_endpoints` |
| Rule flag `openai_endpoint_override`(逃生舱) | `internal/typ/flag_registry.go:117` | 不变 |
| DB 持久化 + 导入导出 | `internal/data/db/provider_store.go:47` | 随字段替换迁移 |
| Responses passthrough 转发 | `internal/server/forwarding/openai.go` | 不变 |

### 2.2 差距

**Gap 0 — 声明模型不合理:`both` 是什么意思?**

`OpenAIEndpointMode` 用一个枚举同时回答两个不同的问题:

1. **能力事实**:上游有哪些端点?(`/chat/completions` 有没有、`/responses` 有没有)
2. **路由策略**:两个都有时选哪个?(`both` ⇒ mirror 入站)

`both` 这个词本身不陈述事实,它是"两个都支持 + 按入站镜像"的合成语义——能力声明里藏了一条路由猜测。后果:

- 声明不直接:厂商文档写的是"我们支持 X 端点",映射到配置却要理解 `chat`/`responses`/`both` 三个模式词的隐含行为;
- 无法独立勾选:UI 上呈现为一个 mode picker(`.design/ux-principles.md` 明确要消除的形态),而不是两个正交的能力开关;
- 不可扩展:未来任何"支持组合 × 选择策略"的新组合都得造新枚举值(Adaptive 时代混乱的温床);
- 语义脆弱:`""`(未知)与 `chat` 行为相同但含义不同,`both` 与"支持两个"看似相同却多了一层 mirror 策略。

**修正原则:声明层只放事实,每个端点一个独立、明确的支持位;策略全部收进 resolver 显式表达。** 见 §4.0。

**Gap A — 数据未声明。** `internal/data/providers.json` 中仅 `openai-com`、`codex` 声明了端点信息;`deepseek-com` 未声明 → 按 Chat 处理。

**Gap B — 声明到不了 provider,用户也无法表达。** `.design/openai-endpoint-routing.md` §6 声称"模板端点声明在实例化时快照到 Provider",但:

- `CreateProviderRequest`(`internal/server/module/provider/types.go:35`)没有端点声明字段;
- `CreateProvider` handler(`handler.go:110`)构造 Provider 时不设置;
- 前端无任何 `endpoint_mode` 引用——快照链路从未接线,**只有 OAuth 路径**(`oauth/handler.go:1140`、`command/oauth.go:390`)会写入。

并且旧设计把声明定义为"用户不可编辑",导致自定义/自建 provider(vLLM、代理网关、模板未及更新的新厂商)即使上游支持 responses 也**没有任何入口声明它**,只能手改 config.json。这与 UX 原则冲突("done ≠ locked"、"smart defaults over toggles" 的前提是 toggle 存在)。

**Gap C — 模型级差异无表达。** DeepSeek `/responses` 当前仅支持 `deepseek-v4-flash`(`deepseek-v4-pro` 官方宣布 2026-08 支持);provider 级声明无法表达这一粒度。

**Gap D — 转换链路未确认。** 见 §5:
- `VendorTransform.applyResponses`(`internal/protocol/transform/vendor.go:58`)对所有厂商 no-op;
- `ConvertAnthropicV1ToResponsesRequest` / Beta 版(`internal/protocol/request/anthropic_{v1,beta}_to_responses.go`)**没有** thinking → `reasoning` 参数映射(grep 无 `reasoning` 命中);
- 响应侧 `streamResponsesToAnthropic*` 的 reasoning item → thinking block 映射需对 DeepSeek 输出验证。

### 2.3 已验证的 DeepSeek 事实(2026-08-02)

来源:官方文档两篇 + 无 key 探测。

| 事实 | 影响 |
|---|---|
| `POST https://api.deepseek.com/responses` 与 `/v1/responses` 均返回 401(路径存在、需鉴权;404 才是不存在) | 现有 `base_url_openai: https://api.deepseek.com/v1` 可直接复用,openai-go SDK 拼 `{base}/responses` 即可,**无需新增 base URL 字段** |
| 仅 `deepseek-v4-flash` 支持;`deepseek-v4-pro` 2026-08 到来 | Gap C;模型级元数据 |
| Stateless:`previous_response_id`、`store`、`conversation` 不支持,客户端自维护历史 | codex 默认 `store:false` + 全量历史重发,天然兼容 |
| 不支持的参数**静默忽略,不报错**(含 `metadata`、`prompt_cache_key`) | vendor transform 无需强制剥离字段(§5.1) |
| 流式为语义化 SSE(`response.completed/incomplete/failed` 结束,无 `[DONE]`) | 与 OpenAI Responses SSE 一致,openai-go 解析无需改动 |
| `reasoning.effort` 全支持(minimal→max);`summary` 接受但不生成 | Anthropic thinking 映射目标(§5.2) |
| tools 支持 `function` 与内建 `web_search`;function arguments 可能是非法 JSON | 响应侧 passthrough,由客户端容错;记入测试项 |
| 不支持图片/文件输入 | 与 DeepSeek Chat 一致,无回归 |

## 3. 设计总览

```
┌────────────────────────────────────────────────────────────┐
│ D0 声明模型重造:openai_endpoint_mode → openai_endpoints    │
│    每端点一个独立支持位;策略全部收进 resolver               │
├────────────────────────────────────────────────────────────┤
│ D1 数据:providers.json                                     │
│    deepseek-com 声明 ["chat_completions","responses"]      │
│    模型级 endpoints 元数据(informational)                  │
├────────────────────────────────────────────────────────────┤
│ D2 接线 + UI:一等可编辑字段                                 │
│    Create/Update API 承载;前端独立勾选;模板预填免手动;     │
│    存量 backfill migration                                  │
├────────────────────────────────────────────────────────────┤
│ D3 路由:resolver 改读端点集合,策略显式(原生优先/降级)      │
├────────────────────────────────────────────────────────────┤
│ D4 转换:Anthropic→Responses thinking 映射 + 验证清单        │
└────────────────────────────────────────────────────────────┘
```

## 4. 详细设计

### 4.0 D0 — 声明模型:按端点显式声明

**数据形态**(模板与 Provider 同形):

```jsonc
// providers.json 模板 / config.json Provider / DB
"openai_endpoints": ["chat_completions", "responses"]
```

- 词汇表:`"chat_completions"`、`"responses"`(与上游 URL path 直接对应,不用缩写模式词);
- **缺省(字段不存在或空数组)= 未声明**,resolver 按生态保守默认处理(仅 Chat)——与今天 `""` 的行为一致;
- 每个值都是一条**可独立验证、可独立勾选的事实陈述**:"该 provider 实现了这个端点"。厂商官宣一个端点,配置就加一个值;UI 上就是一个 checkbox,直接、无猜测;
- 未来若出现第三种 OpenAI 端点形态,加词汇即可,不产生组合爆炸。

**Go 类型**(`ai/provider.go`):

```go
// OpenAIEndpoint 是上游实现的 OpenAI 端点的事实声明。
type OpenAIEndpoint string

const (
    OpenAIEndpointChat      OpenAIEndpoint = "chat_completions"
    OpenAIEndpointResponses OpenAIEndpoint = "responses"
)

// Provider 字段:替换 OpenAIEndpointMode
OpenAIEndpoints []OpenAIEndpoint `json:"openai_endpoints,omitempty"`

func (p *Provider) SupportsOpenAIEndpoint(e OpenAIEndpoint) bool
```

`OpenAIEndpointMode` 类型、四个常量、`OpenAIEndpointModeForIssuer` **删除**;issuer helper 改为:

```go
func OpenAIEndpointsForIssuer(issuer Issuer) []OpenAIEndpoint  // Codex → [responses],其他 → nil
```

**兼容读取**:旧字段仍可能出现在三处外部数据中——用户 config.json、dataio 导入包、GitHub 模板注册表(旧版)。在各自反序列化边界做一次性转换,不在核心类型上保留旧字段:

| 旧值 | 转换为 |
|---|---|
| `""` | 缺省(nil) |
| `"chat"` | `["chat_completions"]` |
| `"responses"` | `["responses"]` |
| `"both"` | `["chat_completions", "responses"]` |

**波及面**(实现清单):`ai/provider.go`、`internal/data/provider_template.go`、`internal/data/db/provider_store.go`(新列 `openai_endpoints`,CSV 或 JSON 编码;旧列迁移后弃用)、`internal/server/module/oauth/handler.go`、`internal/command/oauth.go`、`internal/server/protocol_endpoint.go` + 测试、`internal/protocoltest/*`(测试基建里 ~10 处构造点)、`internal/dataio`、`internal/typ/flag_registry.go` 文案、`vmodel/benchmark/capture.go` 注释、`.design/openai-endpoint-routing.md` 重写相关章节。

### 4.1 D1 — providers.json 数据变更

`deepseek-com` 条目:

```jsonc
{
  "deepseek-com": {
    // ... 现有字段不变 ...
    "openai_endpoints": ["chat_completions", "responses"],   // 新增:两条独立事实
    "models": [
      // deepseek-chat / deepseek-reasoner 已标 deprecated,不加 endpoints
      { "id": "deepseek-v4-flash", "context": 1000000, "max_output": 384000,
        "endpoints": ["chat_completions", "responses"] },     // 新增
      { "id": "deepseek-v4-pro",   "context": 1000000, "max_output": 384000,
        "endpoints": ["chat_completions"] }  // v4-pro responses 官宣 2026-08;上线后实测通过再补
    ],
    "last_updated": "2026-08-02",
    "sources": [
      "https://api-docs.deepseek.com/api/create-response",
      "https://api-docs.deepseek.com/zh-cn/guides/responses_api"
    ]
  }
}
```

存量条目同步换词:`openai-com` → `["chat_completions","responses"]`,`codex` → `["responses"]`(删除两处 `openai_endpoint_mode`)。

模型级 `endpoints`(与 provider 级同词汇表,写入 `_naming_rules.models_schema` 说明):

- **缺省 = 不限制**(跟随 provider 级声明)。存量所有模型不受影响;
- 显式声明 = 该模型经验证的端点集合,**Phase 1 仅作 informational 元数据**:供 UI 展示、smart-guide/agent 规则创建时选默认模型(codex 场景优先挑带 `responses` 的模型)。**不参与路由**——resolver 保持 provider 级纯函数(遵守 `.design/openai-endpoint-routing.md` §3 的分层纪律);
- 若真实用户反馈出现"路由到 responses 但模型不支持"的错误,再评估 Phase 3 的模型级降级 guard(§7)。

同时:注册表 `version` bump 至 `3.0.0`(schema 换字段,major);`_schema_version` bump 并在 `_naming_rules` 增补 `openai_endpoints`/`endpoints` 字段说明。

**其他厂商**:本次只改 DeepSeek。后续厂商官宣 responses 支持时,复用同一模式:实测(401/404 探测 + keyed smoke)→ json 加一个值 → 走 D2 已建好的链路,不需要再改代码。

### 4.2 D2 — 接线 + UI:一等可编辑字段

旧设计("用户不可编辑、仅模板快照")被**显式推翻**:端点支持虽然是上游的客观事实,但 gateway 无法替所有自定义上游代答这个事实——**知道事实的人是用户**。声明改为一等 provider 字段,模板负责预填默认值,用户保留最终编辑权(UX 原则:smart defaults over toggles、done ≠ locked、separate orthogonal axes)。

**(1) API。** `CreateProviderRequest` / `UpdateProviderRequest` 增加:

```go
// internal/server/module/provider/types.go
OpenAIEndpoints []string `json:"openai_endpoints,omitempty" description:"Declared upstream OpenAI endpoints: chat_completions, responses. Empty = undeclared (routes as chat-only)."`
```

后端校验词汇表(非法值 400);`openai` 之外的 APIStyle 忽略该字段。需要 `task codegen` 重新生成 openapi.json 与前端 client SDK(未跑 codegen 前,前端按 CLAUDE.md 惯例留 placeholder)。

**(2) 前端。** openai-style provider 的创建/编辑表单增加两个**独立 checkbox**(不是 mode 下拉):

- ☑ `Chat Completions(/chat/completions)` — 自定义 provider 默认勾选(生态默认);
- ☐ `Responses(/responses)` — 默认不勾选,用户知道上游支持时手动勾上。

**从模板实例化时,前端用模板的 `openai_endpoints` 预填两个 checkbox**——deepseek-com/openai-com 双勾、codex 只勾 responses——用户无需手动操作,但看得见勾了什么(show concrete values,而不是把事实藏在后端快照里)。编辑已有 provider 时同样可见、可改。

两个都不勾 = 未声明,行为等同今天(Chat 保守默认),不阻塞保存。

**(3) 存量 migration(定稿:仅格式转换,不做域名 backfill)。** 新增 `migrate2026XXXX`,幂等、每条写 log:

- **旧枚举转换**:存量 provider 的 `openai_endpoint_mode` 按 §4.0 表转换为 `openai_endpoints`(覆盖 Codex OAuth 存量;`migrate20260518` 保留不动,新迁移在其后运行)。这是等价格式转换,**行为零变化**;
- ~~域名 backfill~~ **不做**(评审决议):存量 deepseek/openai provider 不自动获得 responses 声明,避免任何存量路径翻转(§4.3 第三行的 claude-code 风险随之消除)。存量用户在 provider 编辑页勾选 Responses 即可启用——可编辑字段本身就是启用入口。

**替代方案(已否)**:`template_id` 传后端、由后端解析模板快照。否因:声明既已是可编辑字段,前端预填具体值更直接(用户看得见),后端不需要模板解析逻辑;非 UI 客户端(脚本/CLI)也能直接声明。

### 4.3 D3 — 路由:resolver 改读端点集合,策略显式

`ResolveOpenAIEndpoint(provider, flags, incoming)` 签名与调用点不变,内部判定改为——**策略在此一处显式写出,不再依赖模式词**:

```
1. rule flag override(chat / responses)→ 直接生效(现状不变)
2. incoming ∈ provider.OpenAIEndpoints        → 原生优先:用入站端点
3. 否则,若 provider 声明了另一端点            → 转换降级:用声明的端点
4. 否则(未声明 / 空)                         → 保守默认:Chat
```

决策表(等价重写,行为覆盖旧表):

| Override | 声明 | 入站 Chat | 入站 Responses |
|---|---|---|---|
| 无 | 缺省 / `[]` | Chat | Chat(降级转换) |
| 无 | `[chat_completions]` | Chat | Chat(降级转换) |
| 无 | `[responses]` | Responses(升级转换) | Responses |
| 无 | `[chat_completions, responses]` | **Chat(原生)** | **Responses(原生)** |
| `=chat` / `=responses` | 任意 | 按 override | 按 override |

旧 `both` 的 mirror 行为被"原生优先"规则**推导**出来,而不再是枚举值的隐含语义——同样的运行时行为,但声明与策略各归其位。

变更后 deepseek-com(openai-style provider)行为:

| 场景 | 入站协议 | 之前(未声明) | 之后(声明双端点) |
|---|---|---|---|
| codex | Responses | **降级 Chat**(丢 reasoning/include) | **原生 `/responses` passthrough** ← 本 spec 目标 |
| openai 兼容客户端 | Chat | Chat passthrough | Chat passthrough(原生,不变) |
| claude-code(provider 为 openai-style 时) | Anthropic→(resolver 视为 Responses 入站,`anthropic_message.go:258`) | Anthropic→Chat(+ `reasoning_content` vendor patch) | **Anthropic→Responses 直转** ⚠ 行为翻转,见 §5.2 / §6 |

注:claude-code 场景若 provider 走 anthropic-style(deepseek 有 `base_url_anthropic`,模板默认双模式),不进 OpenAI resolver,不受影响。另,"anthropic 入站视为 Responses 入站"本身也是一条隐藏在调用点的策略,本次不动,但在 `.design/openai-endpoint-routing.md` 重写时应显式记档(§9 开放问题 5)。

### 4.4 D4 — vendor transform 与转换链路

见 §5。

## 5. Vendor Transform 确认清单

### 5.1 Responses 形状的 vendor 处理(`vendor.go applyResponses`)

结论:**Phase 1 不加 DeepSeek case**。依据:官方文档明确"不支持的参数静默忽略、不报错",codex 发出的 `store:false`、`include:["reasoning.encrypted_content"]`、`prompt_cache_key` 等直接透传无害。保留现有 dispatch 结构(per-shape `strings.Contains` 链),若 keyed smoke test(§8)发现实际报错,再在 `applyResponses` 加 `api.deepseek.com` 分支做最小剥离——hook 位置已在,半行改动。

需人工确认项(smoke test 覆盖):
- [ ] codex 全量历史重发中,前轮 DeepSeek reasoning item 回传是否被接受(DeepSeek Chat 侧有 `reasoning_content` 必须回传的要求,见 `ops/request_openai_deepseek.go:26`;Responses 侧 stateless 全量重发理论等价,需实测);
- [ ] DeepSeek 不返回 `reasoning.encrypted_content` 时 codex 客户端行为(预期:stateless 模式不依赖);
- [ ] function_call arguments 非法 JSON 时 passthrough 链路不崩(gateway 不解析 arguments 即可)。

### 5.2 Anthropic→Responses 转换缺 thinking 映射(需修,独立于 DeepSeek 的既有缺口)

`ConvertAnthropicV1ToResponsesRequest` / Beta 版当前**完全丢弃** `thinking` 配置与 assistant thinking blocks。openai-com 双端点路径同样受此缺口影响,DeepSeek 接入使其影响面扩大。修复:

1. 请求参数:`thinking.type=="enabled"` → `reasoning.effort`,按 `budget_tokens` 分档映射(建议:<4k→`low`,<16k→`medium`,否则→`high`;`disabled`→ 不设 reasoning)。DeepSeek 对 effort 全档支持,OpenAI 同形,一份映射两家通用;
2. 历史消息:assistant 消息中的 thinking block → Responses `reasoning` input item(DeepSeek stateless 依赖客户端回传推理上下文);
3. 响应侧:确认 `streamResponsesToAnthropic*` 将 reasoning output item 映射回 anthropic thinking block(含流式增量),对 DeepSeek 实际输出(reasoning item 在 message 之前)验证顺序假设。

### 5.3 Chat→Responses 方向

"原生优先"下 Chat 入站遇双端点 provider 仍走 Chat,不会撞上 Responses 上游;仅声明 `[responses]` 的 provider(Codex)或 rule flag 强制时触达(`ConvertOpenAIChatToResponses`,建设中,函数命名方向反了的 pre-existing issue 见 `.design/openai-endpoint-routing.md` §7 注)。现状即如此,不在本 spec 范围,不阻塞。

## 6. 兼容性与风险

| 风险 | 等级 | 缓解 |
|---|---|---|
| 声明模型换字段,波及 ~12 个文件 + DB 列 + 三个外部数据边界(config.json / dataio / GitHub 注册表旧版) | 中 | §4.0 兼容读取表;migration 幂等;resolver 与 store 已有测试全部改写跟随;一次 PR 内完成硬切,不留双字段长期共存 |
| 用户误勾 responses(上游实际不支持)→ 上游 404 | 中 | 诊断走真实路径(UX 原则):provider 连通性探测按已勾选端点分别测并展示每端点结果;404 错误透传清晰;取消勾选即恢复 |
| claude-code + openai-style deepseek 声明后从 Chat 翻转到 Responses(§4.3 第三行) | 低(定稿后仅新实例/用户主动勾选触发,存量零影响) | §5.2 修复先行合入;UI 取消勾选或 rule flag 逃生 |
| codex 场景 rule 配了 `deepseek-v4-pro` 等暂不支持 responses 的模型 → 上游报错 | 中 | 模型级 `endpoints` 元数据引导 UI/agent 默认选 v4-flash;DeepSeek 报错信息清晰;v4-pro 官宣本月支持,窗口极短 |
| `/v1/responses` 路径仅经 401 探测验证,未 keyed 实测 | 低 | §8 smoke test 项;若意外 404,fallback 方案为 client 层对 deepseek 剥 `/v1` 前缀(不新增数据字段) |
| 模板注册表从 GitHub 同步,新旧版本 app × 新旧注册表交叉 | 低 | 新字段增量可选,旧 app 反序列化忽略;新 app 对旧注册表走 §4.0 兼容转换 |
| `frontend/src/services/service_providers.json`(独立的旧前端数据集,含 `deepseek` 键)与注册表漂移 | 低 | 本 spec 不动它;开放问题 §9 |

## 7. 实施阶段

**Phase 0 — 声明模型重造(先行独立 PR,行为等价重构)**
1. §4.0 全部:类型、resolver 内部改写、DB 列、OAuth helper、兼容读取、旧枚举转换 migration、测试基建跟随;此 PR 结束时运行时行为与现状**逐字节等价**(openai-com/codex 语义不变),只是声明形态换了。

**Phase 1 — 数据 + API + UI(核心交付)**
2. `internal/data/providers.json`:§4.1 变更;
3. Create/Update API 增加 `openai_endpoints` + 校验 + `task codegen`;
4. 前端:独立 checkbox × 2、模板预填、编辑可见可改;连通性探测按端点分别测(§6 风险二);
5. 文档:重写 `.design/openai-endpoint-routing.md` 受影响章节(§1 表格、§2.3 枚举史加一笔 `both` 的教训、§3 Layer 1 "不可编辑"表述、§6 模板表);`_naming_rules` 增补。

**Phase 2 — 转换保真(与 Phase 1 并行开工,合入顺序在 backfill 之前或同批)**
7. §5.2 Anthropic→Responses thinking 映射(请求 + 历史回传 + 响应侧验证)+ e2e 测试;
8. §5.1 / §8 smoke 验证,按需补 `applyResponses` deepseek 分支。

**Phase 3 — 观察后决定(默认不做)**
9. 模型级 endpoints 路由 guard(入站命中 responses 但模型显式不含时降级 Chat)。仅在真实错误反馈出现后立项,且需重新评审是否违反路由分层纪律。

## 8. 测试策略

- **单元**:resolver 新决策表全覆盖(含空声明/单端点/双端点 × 两种入站 × override);Create/Update 携带 `openai_endpoints` 的校验与持久化;migration 两步幂等 + 旧枚举转换全值表 + 域名精确匹配(子域名/自建代理不误伤);
- **protocoltest**:复用 `duo_serve.go`(chat+responses 双端点虚拟 provider)加 deepseek 风味用例:responses passthrough 含 `store/include/previous_response_id` 字段、reasoning item 流式回放(在 message 之前)、`response.completed` 结束无 `[DONE]`;
- **e2e transform**:anthropic thinking 开启 → responses 请求含 `reasoning.effort`;thinking block 历史 → reasoning input item;响应 reasoning item → thinking block 往返;
- **前端**:模板实例化预填正确(deepseek-com 双勾/codex 单勾 responses);自定义 provider 默认仅勾 chat;`ui-preview` skill 截图验收表单布局;
- **Live smoke(手动,需真实 key,不进 CI)**:`deepseek-v4-flash` 走 tingly-box codex 场景端到端:非流/流式、function call 一轮往返、web_search tool、`/v1/responses` 路径确认。脚本落 `tests/` 并文档化。

## 9. 开放问题

1. ~~backfill 范围~~ **已决议(2026-08-02)**:不做域名 backfill,仅新实例(见文首定稿决议);
2. `deepseek-v4-pro` responses 支持正式落地后,`endpoints` 数据更新的节奏(注册表热更 vs 随版本);
3. `service_providers.json`(前端旧数据集)是否已死代码,可否清理(独立小任务);
4. anthropic-style 的 deepseek provider(`/anthropic` base)是否某天也该优先 Responses?——目前判断:不。anthropic-style 走原生 anthropic 端点,DeepSeek 官方自己维护协议映射,gateway 不应二次聪明;
5. "Anthropic 入站在 resolver 中视为 Responses 入站"(`anthropic_message.go:258/382`)是隐藏在调用点的策略,本次不动;重写路由设计文档时显式记档,未来若要改(如按 thinking 开关选择)另立 spec。

---

### Appendix: 关键文件索引

| 文件 | 角色 |
|---|---|
| `ai/provider.go` | 端点声明类型重造(D0) |
| `internal/data/providers.json` | 模板数据(D1) |
| `internal/data/provider_template.go` | 模板 schema(D0) |
| `internal/data/db/provider_store.go` | DB 列迁移(D0) |
| `internal/server/module/provider/types.go` / `handler.go` | Create/Update API(D2) |
| `frontend/`(provider 表单) | 独立 checkbox + 模板预填(D2) |
| `internal/server/config/migration.go` + 新 migration | 旧枚举转换 + backfill(D0/D2) |
| `internal/server/protocol_endpoint.go` | resolver 判定改写(D3) |
| `internal/server/anthropic_message.go:258` | anthropic 入站进 resolver 的调用点(开放问题 5) |
| `internal/server/module/oauth/handler.go` / `internal/command/oauth.go` | OAuth 实例化写声明(D0) |
| `internal/protocol/request/anthropic_{v1,beta}_to_responses.go` | thinking 映射修复点(D4) |
| `internal/protocol/transform/vendor.go` | vendor hook(观察项) |
| `.design/openai-endpoint-routing.md` | 需重写的设计文档 |
