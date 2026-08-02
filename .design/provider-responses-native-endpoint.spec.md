# Spec: Provider 定义声明官方 Responses API 支持(DeepSeek first)

> Status: Draft for review
> Date: 2026-08-02
> Related: `.design/openai-endpoint-routing.md`, `.design/rule-flags.md`, PR #976

---

## 1. Motivation

DeepSeek 官方已上线 OpenAI Responses API([create-response](https://api-docs.deepseek.com/api/create-response)、[responses_api 指南](https://api-docs.deepseek.com/zh-cn/guides/responses_api))。当前 tingly-box 把 DeepSeek 当作 Chat-only 厂商:codex 场景(入站即 Responses 协议)路由到 DeepSeek 时走 `ConvertOpenAIResponsesToChat` 降级,丢弃 `reasoning`、`include` 等 Responses-only 语义。

既然厂商官方支持了,gateway 应通过 **provider 配置预定义**识别这一能力并**优先使用原生 `/responses`**:passthrough 保真(reasoning items、web_search tool、语义化 SSE 全部原样透传),消除有损转换。

现有架构(`OpenAIEndpointMode` + `ResolveOpenAIEndpoint`)已为此设计好——本 spec 主要是**数据声明 + 快照接线补全 + 转换链路确认**,不改路由决策模型。

## 2. 现状与差距

### 2.1 已就绪的部分

| 机制 | 位置 | 状态 |
|---|---|---|
| `OpenAIEndpointMode` enum(`""`/`chat`/`responses`/`both`) | `ai/provider.go:216` | ✅ |
| `ResolveOpenAIEndpoint` 纯函数(rule flag > provider mode,`both` 时 mirror 入站) | `internal/server/protocol_endpoint.go:49` | ✅ |
| 模板字段 `openai_endpoint_mode` | `internal/data/provider_template.go:123` | ✅ |
| Rule flag `openai_endpoint_override`(逃生舱) | `internal/typ/flag_registry.go:117` | ✅ |
| DB 持久化 + 导入导出 | `internal/data/db/provider_store.go:47` | ✅ |
| Responses passthrough 转发 | `internal/server/forwarding/openai.go` | ✅ |

codex 场景入站协议就是 Responses,因此 provider 声明 `both` 后,mirror 语义**自动**实现"codex 场景优先原生 responses",无需任何 scenario 特判。

### 2.2 差距

**Gap A — 数据未声明。** `internal/data/providers.json` 中仅 `openai-com`(`both`)与 `codex`(`responses`)声明了 mode;`deepseek-com` 未声明 → 按 Chat 处理。

**Gap B — 模板快照未接线(本 spec 发现的实现缺口)。** `.design/openai-endpoint-routing.md` §6 声称"模板 `openai_endpoint_mode` 在实例化时快照到 Provider 同名字段",但:

- `CreateProviderRequest`(`internal/server/module/provider/types.go:35`)没有 `openai_endpoint_mode`,也没有 `template_id`;
- `CreateProvider` handler(`handler.go:110`)构造 Provider 时不设置该字段;
- 前端代码中无任何 `endpoint_mode` 引用。

即:**只有 OAuth 路径**(`oauth/handler.go:1140`、`command/oauth.go:390`,经 `OpenAIEndpointModeForIssuer`)会写入 mode。用户经 UI 从 `openai-com` 模板添加 API-key provider,拿到的也是 `mode=""`(Chat)。改 Gap A 的数据而不修 Gap B,声明到不了路由层。

**Gap C — 模型级差异无表达。** DeepSeek `/responses` 当前仅支持 `deepseek-v4-flash`(`deepseek-v4-pro` 官方宣布 2026-08 支持);provider 级 mode 无法表达这一粒度。

**Gap D — 转换链路未确认。** 见 §5:
- `VendorTransform.applyResponses`(`internal/protocol/transform/vendor.go:58`)对所有厂商 no-op;
- `ConvertAnthropicV1ToResponsesRequest` / Beta 版(`internal/protocol/request/anthropic_{v1,beta}_to_responses.go`)**没有** thinking → `reasoning` 参数映射(grep 无 `reasoning` 命中);
- 响应侧 `streamResponsesToAnthropic*` 的 reasoning item → thinking block 映射需对 DeepSeek 输出验证。

### 2.3 已验证的 DeepSeek 事实(2026-08-02)

来源:官方文档两篇 + 无 key 探测。

| 事实 | 影响 |
|---|---|
| `POST https://api.deepseek.com/responses` 与 `/v1/responses` 均返回 401(路径存在、需鉴权;404 才是不存在) | 现有 `base_url_openai: https://api.deepseek.com/v1` 可直接复用,openai-go SDK 拼 `{base}/responses` 即可,**无需新增 base URL 字段** |
| 仅 `deepseek-v4-flash` 支持;`deepseek-v4-pro` 2026-08 到来 | Gap C;需模型级元数据或文档化 |
| Stateless:`previous_response_id`、`store`、`conversation` 不支持,客户端自维护历史 | codex 默认 `store:false` + 全量历史重发,天然兼容 |
| 不支持的参数**静默忽略,不报错**(含 `metadata`、`prompt_cache_key`) | vendor transform 无需强制剥离字段(§5.1) |
| 流式为语义化 SSE(`response.completed/incomplete/failed` 结束,无 `[DONE]`) | 与 OpenAI Responses SSE 一致,openai-go 解析无需改动 |
| `reasoning.effort` 全支持(minimal→max);`summary` 接受但不生成 | Anthropic thinking 映射目标(§5.2) |
| tools 支持 `function` 与内建 `web_search`;function arguments 可能是非法 JSON | 响应侧 passthrough,由客户端容错;记入测试项 |
| 不支持图片/文件输入 | 与 DeepSeek Chat 一致,无回归 |

## 3. 设计总览

```
┌────────────────────────────────────────────────────────────┐
│ D1 数据:providers.json                                     │
│    deepseek-com + openai_endpoint_mode:"both"              │
│    模型级 endpoints 元数据(informational)                  │
├────────────────────────────────────────────────────────────┤
│ D2 接线:模板快照落地                                        │
│    CreateProviderRequest + template_id → snapshot mode     │
│    migration backfill(域名精确匹配)                        │
├────────────────────────────────────────────────────────────┤
│ D3 路由:零改动(复用 mirror 语义)                           │
├────────────────────────────────────────────────────────────┤
│ D4 转换:Anthropic→Responses thinking 映射 + 验证清单        │
└────────────────────────────────────────────────────────────┘
```

## 4. 详细设计

### 4.1 D1 — providers.json 数据变更

`deepseek-com` 条目:

```jsonc
{
  "deepseek-com": {
    // ... 现有字段不变 ...
    "openai_endpoint_mode": "both",          // 新增:chat + responses 双端点
    "models": [
      // deepseek-chat / deepseek-reasoner 已标 deprecated,不加 endpoints
      { "id": "deepseek-v4-flash", "context": 1000000, "max_output": 384000,
        "endpoints": ["chat", "responses"] },              // 新增
      { "id": "deepseek-v4-pro",   "context": 1000000, "max_output": 384000,
        "endpoints": ["chat"] }   // v4-pro responses 官宣 2026-08;上线后实测通过再补
    ],
    "last_updated": "2026-08-02",
    "sources": [
      "https://api-docs.deepseek.com/api/create-response",
      "https://api-docs.deepseek.com/zh-cn/guides/responses_api"
    ]
  }
}
```

模型级 `endpoints` 语义(写入 `_naming_rules.models_schema` 说明):

- **缺省 = 不限制**(跟随 provider 级 mode)。存量所有模型不受影响;
- 显式声明 = 该模型经验证的端点集合,**Phase 1 仅作 informational 元数据**:供 UI 展示、smart-guide/agent 规则创建时选默认模型(codex 场景优先挑带 `responses` 的模型)。**不参与路由**——`ResolveOpenAIEndpoint` 保持 provider 级纯函数,不扩坑(遵守 `.design/openai-endpoint-routing.md` §3 的分层纪律);
- 若真实用户反馈出现"mirror 到 responses 但模型不支持"的错误,再评估 Phase 3 的模型级降级 guard(§7)。

同时:注册表 `version` bump 至 `2.2.0`;`_naming_rules` 增补 `openai_endpoint_mode` 与 `endpoints` 字段说明。

**其他厂商**:本次只改 DeepSeek。后续厂商官宣 responses 支持时,复用同一模式:实测(401/404 探测 + keyed smoke)→ 改 json → 走 D2 已建好的链路,不需要再改代码。

### 4.2 D2 — 模板快照接线 + backfill

**(1) 创建链路快照。** `CreateProviderRequest` 增加可选字段:

```go
// internal/server/module/provider/types.go
TemplateID string `json:"template_id,omitempty" description:"Provider template id this provider is instantiated from (e.g. deepseek-com); backend snapshots structural facts (openai_endpoint_mode) from it"`
```

`CreateProvider` handler:`TemplateID` 非空时经 `templateManager` 解析模板,快照 `OpenAIEndpointMode`(cast 到 `ai.OpenAIEndpointMode`)。选 `template_id` 而非直传 `openai_endpoint_mode`:mode 是上游 API 的结构性事实、非用户偏好(设计文档 Layer 1 语义),客户端不应能任意指定;且未来模板新增结构性字段时无需再改 API。

前端:从模板添加 provider 的表单提交时带上 `template_id`(现有模板列表 API 已返回 id)。需要 `task codegen` 重新生成 openapi.json 与前端 client SDK。

**(2) 存量 backfill migration。** 仿照 `migrate20260518`(Codex backfill)新增 `migrate2026XXXX`:

- 条件:`provider.OpenAIEndpointMode == ""` **且** `APIBase` host 精确匹配模板 `canonical_domain` **且**该模板声明了非空 mode;
- 当前命中:`api.openai.com` → `both`,`api.deepseek.com` → `both`;
- 幂等;每条写 log。

Backfill 是行为变更(见 §6 风险),需要 release note;逃生舱为 rule flag `openai_endpoint_override: "chat"`(逐条 rule 粒度,已存在)。

### 4.3 D3 — 路由:零改动

`ResolveOpenAIEndpoint` 决策表不变。变更后行为(deepseek-com,openai-style provider):

| 场景 | 入站协议 | 之前(mode="") | 之后(mode="both") |
|---|---|---|---|
| codex | Responses | **降级 Chat**(`ConvertOpenAIResponsesToChat`,丢 reasoning/include) | **原生 `/responses` passthrough** ← 本 spec 目标 |
| openai 兼容客户端 | Chat | Chat passthrough | Chat passthrough(mirror,不变) |
| claude-code(provider 为 openai-style 时) | Anthropic→(视为 Responses 入站,`anthropic_message.go:258`) | Anthropic→Chat(+ `reasoning_content` vendor patch) | **Anthropic→Responses 直转** ⚠ 行为翻转,见 §5.2 / §6 |

注:claude-code 场景若 provider 走 anthropic-style(deepseek 有 `base_url_anthropic`,模板默认双模式),不进 OpenAI resolver,不受影响。

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

`ConvertAnthropicV1ToResponsesRequest` / Beta 版当前**完全丢弃** `thinking` 配置与 assistant thinking blocks。openai-com `both` 路径同样受此缺口影响,DeepSeek 接入使其影响面扩大。修复:

1. 请求参数:`thinking.type=="enabled"` → `reasoning.effort`,按 `budget_tokens` 分档映射(建议:<4k→`low`,<16k→`medium`,否则→`high`;`disabled`→ 不设 reasoning)。DeepSeek 对 effort 全档支持,OpenAI 同形,一份映射两家通用;
2. 历史消息:assistant 消息中的 thinking block → Responses `reasoning` input item(DeepSeek stateless 依赖客户端回传推理上下文);
3. 响应侧:确认 `streamResponsesToAnthropic*` 将 reasoning output item 映射回 anthropic thinking block(含流式增量),对 DeepSeek 实际输出(reasoning item 在 message 之前)验证顺序假设。

### 5.3 Chat→Responses 方向

`both` 的 mirror 语义下 Chat 入站不会撞上 Responses 上游,仅 rule flag 强制 `responses` 时触达(`ConvertOpenAIChatToResponses`,建设中,函数命名方向反了的 pre-existing issue 见 `.design/openai-endpoint-routing.md` §7 注)。不在本 spec 范围,不阻塞。

## 6. 兼容性与风险

| 风险 | 等级 | 缓解 |
|---|---|---|
| backfill 后 claude-code + openai-style deepseek 从 Chat 翻转到 Responses(§4.3 第三行) | 中 | §5.2 修复先行合入(同 PR 或先行 PR);release note;rule flag `openai_endpoint_override:"chat"` 逃生;若评审认为过险,降级方案:backfill 只处理 `api.openai.com`,DeepSeek 仅新实例生效 |
| codex 场景 rule 配了 `deepseek-v4-pro` 等暂不支持 responses 的模型 → 上游报错 | 中 | 模型级 `endpoints` 元数据引导 UI/agent 默认选 v4-flash;DeepSeek 报错信息清晰;v4-pro 官宣本月支持,窗口极短 |
| `/v1/responses` 路径仅经 401 探测验证,未 keyed 实测 | 低 | §8 smoke test 项;若意外 404,fallback 方案为 client 层对 deepseek 剥 `/v1` 前缀(不新增数据字段) |
| 模板注册表从 GitHub 同步,老版本 app 读到新字段 | 低 | `openai_endpoint_mode`/`endpoints` 均为增量可选字段,旧代码反序列化忽略 |
| `frontend/src/services/service_providers.json`(独立的旧前端数据集,含 `deepseek` 键)与注册表漂移 | 低 | 本 spec 不动它;开放问题 §9 |

## 7. 实施阶段

**Phase 1 — 数据 + 接线(核心交付)**
1. `internal/data/providers.json`:§4.1 变更;
2. `CreateProviderRequest.TemplateID` + handler 快照 + `task codegen` + 前端传参;
3. backfill migration + 单测;
4. 文档:`.design/openai-endpoint-routing.md` 更新 §1 表格("仅 OpenAI 官方 + Codex"已不成立)、§6 模板表、并修正"实例化时快照"的描述为实际接线;`_naming_rules` 增补。

**Phase 2 — 转换保真(与 Phase 1 并行开工,合入顺序在 backfill 之前或同批)**
5. §5.2 Anthropic→Responses thinking 映射(请求 + 历史回传 + 响应侧验证)+ e2e 测试;
6. §5.1 / §8 smoke 验证,按需补 `applyResponses` deepseek 分支。

**Phase 3 — 观察后决定(默认不做)**
7. 模型级 endpoints 路由 guard(mirror→responses 且模型显式不含 responses 时降级 Chat)。仅在真实错误反馈出现后立项,且需重新评审是否违反路由分层纪律。

## 8. 测试策略

- **单元**:resolver 已有覆盖不动;`CreateProvider` 带/不带 `template_id` 的快照行为;migration 幂等 + 域名精确匹配(子域名/自建代理不误伤);
- **protocoltest**:复用 `duo_serve.go`(chat+responses 双端点虚拟 provider)加 deepseek 风味用例:responses passthrough 含 `store/include/previous_response_id` 字段、reasoning item 流式回放(在 message 之前)、`response.completed` 结束无 `[DONE]`;
- **e2e transform**:anthropic thinking 开启 → responses 请求含 `reasoning.effort`;thinking block 历史 → reasoning input item;响应 reasoning item → thinking block 往返;
- **Live smoke(手动,需真实 key,不进 CI)**:`deepseek-v4-flash` 走 tingly-box codex 场景端到端:非流/流式、function call 一轮往返、web_search tool、`/v1/responses` 路径确认。脚本落 `tests/` 并文档化。

## 9. 开放问题

1. backfill 是否包含 `api.deepseek.com`(§6 风险一的两案),还是首发仅 `api.openai.com` + DeepSeek 新实例?→ 评审定;
2. `deepseek-v4-pro` responses 支持официально落地后,`endpoints` 数据更新的节奏(注册表热更 vs 随版本);
3. `service_providers.json`(前端旧数据集)是否已死代码,可否清理(独立小任务);
4. anthropic-style 的 deepseek provider(`/anthropic` base)是否某天也该优先 Responses?——目前判断:不。anthropic-style 走原生 anthropic 端点,DeepSeek 官方自己维护协议映射,gateway 不应二次聪明。

---

### Appendix: 关键文件索引

| 文件 | 角色 |
|---|---|
| `internal/data/providers.json` | 模板数据(D1) |
| `internal/data/provider_template.go` | 模板 schema |
| `internal/server/module/provider/types.go` / `handler.go` | 创建链路快照(D2) |
| `internal/server/config/migration.go` + 新 migration | backfill(D2) |
| `internal/server/protocol_endpoint.go` | 路由 resolver(零改动) |
| `internal/server/anthropic_message.go:258` | anthropic 入站进 resolver 的调用点 |
| `internal/protocol/request/anthropic_{v1,beta}_to_responses.go` | thinking 映射修复点(D4) |
| `internal/protocol/transform/vendor.go` | vendor hook(观察项) |
| `ai/provider.go` | mode enum |
| `.design/openai-endpoint-routing.md` | 需同步更新的设计文档 |
