# Recording 梳理:意图、现状与整合方向

> 适用对象:tingly-box 后端 / 前端贡献者。
> 状态:**梳理文档(as-is 盘点 + to-be 方向)**。本文档只梳理、不改行为;
> 分阶段落地见 §6。obs 包内部的 pipeline 化重构规划见
> `internal/obs/PLANNING.md`(Phase 2),本文与其互补:PLANNING 管
> "record 怎么被采集与导出",本文管 "record 由谁启用、在哪些层出现、
> 与 rule flag 体系怎么融合"。

---

## 1. 意图(统一口径)

历史上 recording 的意图一直比较混乱(全局 CLI 开关、scenario 开关、
client 层独立 RoundTripper 三者并存),导致了当前零碎的局面。统一后的
意图只有三句话:

1. **Record 实体长期存在并沿请求链传播,只是不一定启用。**
   每个请求都可以有一个 record 实体(recorder),从 handler 入口创建,
   经 transform chain 传播到 client transport;"是否启用/录多深"是它
   身上的状态,不是"是否存在"的条件。这与 obs PLANNING Phase 2 的
   "hot path 总是构建完整 record,裁剪放在 exporter 出口" 是同一思想。

2. **启用来源走 flag 体系:scenario 可以启用,rule flag 也可以启用。**
   这正是 rule flag 系统的既有设计(shared flag + 继承,参照
   `thinking_effort` / `skip_usage`,见 `.design/rule-flags.md` §12)。
   recording 满足 rule flag 的全部定义特征——局部、可选、可叠加、
   per-rule 语义成立("只录打到某条 rule 的流量"是最典型的 debug 诉求)。

3. **Client 层不做判定,只负责用最终的 transport 录制出站请求。**
   到了 client 层,"是否需要录 request"已经在上游(flag 解析)决定;
   而且只要 record 启用,出站 wire 请求总是要录的——所以 client 层
   反而简单:在**最贴近 wire 的 transport** 上无条件录制(recorder
   在 ctx 里就录),不再有自己的 mode 判定逻辑。

---

## 2. 现状全景(as-is)

### 2.1 三个互相独立的开关来源

| 来源 | 粒度 | 位置 | 状态 |
|------|------|------|------|
| ~~CLI `--record-mode` / `--record-dir`~~ | 全局 | — | **已移除(Phase 1)**:启用与否是 flag 关注点,不是 CLI 参数。落盘目录固定为 `<configDir>/record`(`StartServerOptions.RecordDir` 内部解析,无用户 flag) |
| `ScenarioFlags.RecordingV2` | scenario | `internal/typ/type.go`(json `recording_v2`) | 生效,scenario-only,**不在 `RuleFlagRegistry()`** |
| rule 级 | 单条 rule | — | **不存在** |

`Server.GetScenarioRecordMode(scenario)`(`server_options.go`)现在只读
scenario 的 `recording_v2`,不再有全局 fallback。

### 2.2 两套录制机制

**A. Chain 级(v2,现役)** — `recording.ProtocolRecorder` +
`TransformRecorder`(StagePre / StagePost):

```
handler 入口 (EnsureProtocolRecorder, 读 GetScenarioRecordMode)
  │  recorder 存 gin ctx;sink 按 scenario 懒建 (scenarioRecordSinks)
  ▼
BuildTransformChain: StagePre 录原始请求 → … → Vendor → StagePost 录最终(SDK 形态)请求
  ▼
流式 hooks / RecordResponse / RecordError → sink.Emit
```

- 只接了 **Anthropic 入站** handler(`anthropic_message.go` V1 + Beta)。
- **OpenAI Chat / Responses handler 传 `nil` recorder**
  (`openai_chat.go`、`openai_responses.go` 里 `TransformOpenAIChat/
  Responses(..., nil, ...)`)——OpenAI 入站流量即使开了 `recording_v2`
  也不录。
- mode 语义:`request` = 只录 transformed request;`request_response`
  再加最终响应;`staged_request_response` 再加原始(客户端)请求。

**B. Client 级(legacy,已死)** — `client.RecordRoundTripper`
(`internal/client/record_roundtripper.go`),见 §3。

### 2.3 Client transport 链的层次(通用链)

```
[RecordRoundTripper]   ← 仅 SetRecordSink 后挂载;最外层
  loggingRoundTripper  ← wrapWithLogging(仅日志)
    ruleFlagTransport  ← wrapWithRuleFlags(header 改写:UA / extra_headers)
      base (wire)      ← transport pool / vendor round-tripper
```

vendor 链(Claude OAuth / Codex / Kimi / Gemini / Antigravity)自建
transport,不挂 `ruleFlagTransport`(不变式,见 rule-flags.md §8),
也同样可能被 `SetRecordSink` 包上 `RecordRoundTripper`(advisor 路径)。

### 2.4 Sink 生命周期

- `scenarioRecordSinks map[RuleScenario]*obs.Sink`:root `*Server` 持有,
  按 scenario 懒建(`GetOrCreateScenarioSink`),chain 级录制用它。
- `ClientPool.recordSink`:生产路径 `server.clientPool =
  client.NewClientPool()` **从不设置 sink**(`WithRecordSink` 只有测试
  在用;`server.recordSink` 字段声明后无人读写)。
- 唯一在运行时给 client 塞 sink 的是 advisor 路径:
  `servertool/hook.go::applyHooks` 把 scenario sink 放进 ctx →
  `mcp/runtime/advisor_call.go` 对 advisor wrapper client 调
  `SetRecordSink(sink)`。

---

## 3. 问题清单(逐条,可验证)

**P1 — `recording_v2` 游离在 flag registry 之外。**
scenario-only、无 FlagSpec、前端 `RecordingV2Control.tsx` 硬编码
(rule-flags.md §13 已把"scenario flag registry 化"列为未做项)。

**P2 — rule 级 recording 不存在。** 意图 §1.2 要求的"rule flag 可以
启用"没有对应实现。

**P3 — `RecordRoundTripper` 的录制路径是死代码。✅ 已清除(Phase 1)。**
原论证链:
1. `obs.NewSink` 只接受三种 v2 mode(`request` / `request_response` /
   `staged_request_response`),其余返回 nil(`sink.go`);
2. `RecordRoundTripper.RoundTrip` 开头对这三种 mode **直接透传**
   (early return,意图是"v2 由 chain 级负责,client 层不重复录");
3. 挂载只发生在 sink 非空时(`SetRecordSink` → `applyRecordMode`);
4. 生产 pool 无 sink(§2.4),唯一挂载点是 advisor wrapper——挂上即
   early-return。
   ⇒ 该文件 ~450 行的录制 / SSE 重组逻辑全部不可达。
Phase 1 已删除:`record_roundtripper.go` 整个文件、各 client 的
`recordSink` 字段 / `SetRecordSink` / `applyRecordMode`(含接口方法与
vmodel no-op 实现)、pool 的 sink 字段与 builder、advisor_call.go 中
两处对 `SetRecordSink` 的死调用。

**P4 — advisor 防递归 header 缺失(真 bug)。✅ 已修复(Phase 1.5)。**
原状:`X-Tingly-Advisor-Depth: 1` 的唯一设置点在 `RecordRoundTripper.
RoundTrip` 的 early-return **之后**——即从未真正发出;服务端却靠这个
header 跳过 MCP tool 注入 / 标记 loopback(`protocol_transform.go`、
`transform_mcp_tool_injection.go`)。
修复方式:advisor 调用侧(`mcp/runtime/advisor_call.go`)在 SDK 调用前
`client.WithAdvisorLoopback(ctx)` 标记 ctx;通用 pass-through 链挂载只读
的 `advisorLoopbackTransport`(`internal/client/advisor_loopback.go`,
挂载点:`NewOpenAIClient` 与 `anthropicTransport`)按标记盖 header。
vendor 链不挂——它们固定指向真实 vendor 端点,不可能 loopback。
同批清理了从未生效的 advisor sink 注入:`WithAdvisorRecordSink` /
`GetAdvisorRecordSink`(tool/context.go)、`HookDeps.GetScenarioSink`
及其注入点(servertool/hook.go)与实现(mcp_tool_error.go)全部删除;
advisor 调用的录制将来随统一录制路径(Phase 3)回归。

**P5 — 录制覆盖不对称。** OpenAI 入站 handler 不接 recorder(§2.2 A)。

**P6 — 即使 P3 修活,挂载位置也录不到真实出站请求。**
`RecordRoundTripper` 挂在**最外层**(§2.3),看到的是
`ruleFlagTransport` / vendor round-tripper 改写 header **之前**的请求;
chain 级 StagePost 录的则是 SDK 参数形态(拿不到 wire header)。
"真正发出去的请求"目前没有任何一层能完整录到——这正是意图 §1.3
"用最终的 transport 录制"要解决的。

**P7 — `ScenarioContextKey` 定义在死文件里。✅ 已解决(Phase 1):**
迁至 `internal/client/context.go`,引用方
(`routes_middleware.go`、`servertool/hook.go`)不变。

---

## 4. 目标架构(to-be)

```
                     启用判定(一次,handler 入口)
   scenario flag (recording_v2, 场景默认) ──┐
   rule flag (recording, override 继承) ────┤→ resolveRuleFlagsWithScenario
   CLI --record-mode (全局兜底,去留待定) ──┘        │
                                                     ▼
              record 实体(per-request recorder)创建/禁用
                     │ 挂 gin ctx + request ctx,全链传播
                     ▼
   transform chain: StagePre(原始) … StagePost(transformed)   ← 现有
                     ▼
   client: 最终 wire transport 无条件录制出站请求(+响应流)   ← 新增
                     │  recorder 在 ctx 就录;无 mode 判定
                     ▼
              sink(per-scenario)/ ModeFilterExporter 出口裁剪  ← obs Phase 2
```

要点:

1. **Flag 建模**:`RuleFlags.Recording`(enum:`""` / `request` /
   `request_response` / `staged_request_response`),`Shared: true`,
   `InheritanceMode: "override"`(rule 显式设置 > scenario `recording_v2`
   默认 > 全局 CLI 兜底)。走 rule-flags.md §10 的标准操作手册,前端
   零 UI 代码(registry-driven)。类别可新增 `FlagCategoryObservability`。
   注入类型上它是 **Type 2 变体**:handler 入口读解析后的 flags 决定
   recorder 创建与 mode——不改请求体,故不进 transform slot。

2. **Record 实体传播**:recorder(或 obs Phase 2 的 `RecordCtx`)由
   handler 创建后,除 gin ctx 外同时进入 `c.Request.Context()`
   (SDK 调用共享该 ctx),client transport 用 ctx 取用——与
   `typ.GetRuleFlags` 同一手法。"存在但未启用"时为 nil / disabled,
   零成本。

3. **Client 层收敛**:新的 `recordTransport` 直接包在 **wire transport**
   上(`ruleFlagTransport` / vendor round-tripper **之内**,所有 header
   改写之后),从 ctx 取 recorder,有则录出站 wire 请求与响应流。
   只读不写,因此 vendor 链也可以挂——不违反"vendor 链不挂
   `ruleFlagTransport`"的不变式(那条不变式挡的是**改写**)。
   `RecordRoundTripper` 整体删除;advisor-depth header 移到确定执行的
   位置(advisor client 构造处或独立小 transport),修复 P4。

4. **覆盖补齐**:OpenAI Chat / Responses handler 接上 recorder(P5)。

5. **Mode 语义收敛**:recorder 总是尽量收集(client 请求 / transformed
   请求 / wire 请求 / 响应),录多少由出口裁剪(obs Phase 2 的
   `ModeFilterExporter`)。wire 请求进入 record 模型后,`staged` 语义
   自然升级为"原始 + transformed + wire + 响应"。

---

## 5. 开放问题(落地前需拍板)

- **CLI `--record-mode` 的去留**:统一到 flag 体系后,全局开关是保留为
  "所有 scenario 的默认值"还是废弃?倾向保留为兜底(部署级 debug),
  但文档与 UI 都以 flag 为主入口。
- **Sink 归属**:rule 级启用时是否仍写 scenario sink?倾向是——rule 只
  决定"录不录/录多深",落盘仍按 scenario 组织(目录结构不变,record
  里已带 provider/model/scenario 字段可过滤)。
- **`recording_v2` 字段名**:scenario 级 json key 保持 `recording_v2`
  (兼容存量配置),rule 级新字段用干净的 `recording`;还是两级同名?
  倾向前者,迁移成本最低。
- **响应流录制在 client 层还是 chain 层**:chain 级已有流式 hooks,
  client 层再录 wire 响应会重复;倾向 client 层只录 wire **请求**,
  响应仍归 chain 级 hooks,直到 obs Phase 2 的 EventTap 统一。

---

## 6. 分阶段落地(防止一次改动过大)

| 阶段 | 内容 | 涉及 | 风险 |
|------|------|------|------|
| **Phase 0 ✅** | 本梳理文档 | `.design/recording.md` | 无 |
| **Phase 1 清障 ✅(收窄范围)** | 已做:删 `RecordRoundTripper` 死代码与全部 `SetRecordSink` 机制;`ScenarioContextKey` 迁出(P7);删 `ClientPool.recordSink` / `server.recordSink` / `server.recordMode` / `WithRecordMode` / `WithRecording`;去除 CLI `--record-mode` / `--record-dir`(目录固定默认)。**刻意未动**:advisor/MCP 侧接线(`WithAdvisorRecordSink`、`HookDeps.GetScenarioSink`)与 P4 header 修复——单独小步处理 | `internal/client`、`internal/server`、`internal/command`、`gui/wails3`、`vmodel` | 低(删死代码,行为不变) |
| **Phase 1.5 advisor 小步 ✅** | 修 P4(advisor ctx 标记 + 通用链只读 header transport,附单测);清 `WithAdvisorRecordSink` / `GetScenarioSink` 死数据注入 | `internal/client`、`mcp/runtime`、`servertool` | 低 |
| **Phase 2 flag 融入** | `RuleFlags.Recording` 进 registry(Shared/override);`resolveRuleFlagsWithScenario` 继承;handler 用解析结果替代 `getScenarioRecordMode`;OpenAI 两个 handler 接 recorder(修 P5);前端类型 + codegen | `typ`、`server`、`protocolserver`、frontend 类型 | 中 |
| **Phase 3 wire 录制** | 新 `recordTransport` 挂 wire transport(含 vendor 链);recorder 经 request ctx 传播;record 模型加 wire 请求字段(修 P6) | `internal/client`、`obs` | 中 |
| **Phase 4 obs 汇合** | 与 `internal/obs/PLANNING.md` Phase 2 合流(RecordCtx / EventTap / ModeFilterExporter);scenario 前端控件 registry 化 | `obs`、`transform`、frontend | 按其自身计划 |

Phase 1 与 Phase 2 互不依赖,可并行;Phase 3 依赖 Phase 2(recorder 的
启用判定先统一)。每阶段独立 vet / test 绿。

---

## 7. 与现有文档的关系

- `.design/rule-flags.md`:§12 的 scenario-only 表里 `recording_v2` 在
  Phase 2 后升级为 shared flag,需同步更新该表与 §4 主表。
- `internal/obs/PLANNING.md`:record 采集/导出侧的权威规划;本文的
  Phase 4 即与其合流点。两文档口径一致:record 实体总是构建,裁剪在
  出口。
- `.design/user-agent.md` / rule-flags.md §8:vendor 链不变式只约束
  **改写型** transport;只读的 recordTransport 挂 vendor 链不在禁区,
  但新增时仍须逐链核对(Gemini 清空 header 的链要确认挂载点在清空
  之后)。
