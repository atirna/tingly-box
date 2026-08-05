# Web Proxy

> 适用对象：tingly-box 后端 / 前端贡献者。
> 描述 web proxy 的设计与实现。与 `vision-proxy.md` 是姊妹文档——两者
> 作用域模型、配置形态、UI 范式刻意保持一致，差异只在"借的是什么能力"。

---

## 1. 它做什么

下游模型没有联网能力，但请求里带了 `web_search` / `web_fetch`——**借一个
有联网能力的 {provider, model} 来干这件事**。

具体两步：

1. **请求侧**：把请求里的 native web 工具声明剥掉（那是 *server tool*，
   由 provider 侧执行，下游根本跑不了），换成两个**普通 function tool**，
   任何模型都能调。
2. **执行侧**：下游模型一旦调用这两个工具，tingly-box 就拿配置的
   {provider, model} 去真正搜索 / 抓取，把结果作为 tool_result 回填，
   循环继续。**客户端全程看不到这两个工具调用**，只看到最终回答。

和 vision proxy 的对称关系：

| | vision proxy | web proxy |
|---|---|---|
| 下游缺的能力 | 看图 | 联网 |
| 触发时机 | 请求里有 image block | 模型调用注入的 web 工具 |
| 借来做什么 | 把图描述成文字 | 把查询变成带出处的文字 |
| 客户端可见 | 否（图已被替换） | 否（工具调用被服务端吃掉） |

> **为什么不是"整请求改道"？** 把带 web 工具的请求整个路由到联网模型，
> 意味着用户选的下游模型被悄悄换掉——回答风格、上下文、成本全变了。
> 借能力只借能力：主模型仍然是用户选的那个，只是多了一双眼睛。

---

## 2. 两个作用域，同一种效果

| 作用域 | 配置位置 | 谁来用 |
|------|------|------|
| **scenario 级** | `ScenarioConfig.Extensions["web_proxy_service"]` | "这个场景的所有 rule 共用一个联网模型" |
| **rule 级** | `Rule.Flags.WebProxyService` | "这条 rule 单独用别的联网模型（或单独关掉）" |

### 配置矩阵

| rule 设了 | scenario 设了 | 实际用谁 |
|---|---|---|
| ✓ | ✓ | **rule** |
| ✓ | ✗ | rule |
| ✗ | ✓ | scenario |
| ✗ | ✗ | 不启用 |

**rule 优先于 scenario**——更具体的作用域被视为用户意图。半填的
`{provider}` 或 `{model}` **不算配置**（`IsActive()` 为假），所以 rule 只
填了一半时会正常回退到 scenario，而不是把功能卡在中间态。

### 服务形态是 `{provider, model}` 二元，没有独立开关

与 vision proxy 同一条原则：**"是否启用" ≡ "有没有配 service"**。配了就是
开，清空就是关。单一事实源、无飘移；前端也因此把"开关"和"选模型"合并成
一个控件。

### 一个 flag 同时管 search 和 fetch

不拆成 `web_search_proxy_service` / `web_fetch_proxy_service`。两者是同一
个"联网"能力的两面，同一个模型几乎总是同时具备或同时不具备；拆开只会让
用户面对一个他答不上来的问题（"我要给抓取单独选个模型吗？"）。真出现
需求再拆——加 flag 比减 flag 容易。

---

## 3. 数据模型

### 3.1 Scenario 级 —— Extensions 存储

```jsonc
// ScenarioConfig.Extensions
{
  "web_proxy_service": {
    "provider": "<provider-uuid>",
    "model": "claude-sonnet-4-6"
  }
}
```

约定 key：`internal/server/config/extension_keys.go` 的
`ExtensionWebProxyService`。

### 3.2 Rule 级 —— RuleFlags typed 字段

```go
// internal/typ/type.go
type RuleFlags struct {
    // ...
    WebProxyService *WebProxyService `json:"web_proxy_service,omitempty" yaml:"web_proxy_service,omitempty"`
}

type WebProxyService struct {
    Provider string `json:"provider" yaml:"provider"`
    Model    string `json:"model"    yaml:"model"`
}

func (s *WebProxyService) IsActive() bool  // nil-safe：两项齐备才算配置
```

`WebProxyService` 与 `VisionProxyService` 是**两个独立类型**而非共用一个
`ServiceRef`：两个 flag 语义无关，互相不该可赋值；共用类型会让"把 vision
的值塞进 web 字段"变成编译期合法的错误。

### 3.3 Flag registry

```go
{
    Key:      "web_proxy_service",
    Type:     FlagTypeServiceRef,   // 复用 vision proxy 引入的二元类型
    Category: FlagCategoryWeb,      // 新增分组
}
```

`FlagTypeServiceRef` 直接复用——前端的 `service_ref` 分支已经会渲染模型选择
器，新增 flag 不需要前端改渲染逻辑。

---

## 4. 执行流程

### 4.1 作用域归一：一处 merge，一处 stash

不像 vision proxy 有独立的 `Service.Resolve` 在 handler 里调用，web proxy
的作用域合并发生在**已有的唯一 flag merge 点**
`ResolveRuleFlagsWithScenario`（`internal/protocolserver/rule_flags.go`）：

```go
if !flags.WebProxyService.IsActive() {
    if svc := webproxy.ParseScenarioService(scenarioConfig.Extensions); svc != nil {
        flags.WebProxyService = svc     // scenario 兜底
    }
}
// ...
applyWebProxyService(c, flags)          // 挂到 request context
```

合并后**只剩一个字段**，下游（tool transform / dispatch gate / 日志）全部
读同一份，不存在"这里读 rule、那里读 scenario"的分叉。

挂 context 的理由和 `custom_user_agent`、`context_1m` 完全一样（见
`rule_flags.go` 里那几个 `applyXxx`）：真正的消费点在 dispatch 深处，那里
已经拿不到 rule 了。

`webproxy.Resolve(cfg, scenario, rule)` 仍然存在并被单测覆盖，作为该优先级
规则的可执行说明。

### 4.2 请求侧：preVendor 槽的 tool transform

```go
// RulePreVendorTransforms
if flags.WebProxyService.IsActive() {
    preVendor = append(preVendor, webproxy.NewToolTransform(true))
}
```

**为什么是 preVendor 而不是 preBase**：preBase 看到的是客户端原始形态，
preVendor 看到的是**协议转换后、面向上游的形态**。"能注入哪些工具"取决于
目标协议——尤其是 Responses 目标根本没有服务端工具循环（见 §4.4），只有在
目标已知时才能做出正确决定。

transform 做两件事，顺序固定：

1. **剥掉 native web 工具**
   - Anthropic（v1 + beta）：`OfWebSearchTool20250305` /
     `OfWebSearchTool20260209` / `OfWebFetchTool2025091` 等 union 成员
   - OpenAI Chat：按名字匹配 `web_search` / `websearch` /
     `web_search_preview` / `web_fetch` / `webfetch`
   - Responses：`OfWebSearch` / `OfWebSearchPreview`
2. **注入两个 function tool**，名字为
   `tingly_box_mcp__webproxy__web_search` / `…__web_fetch`。

名字冲突时**客户端已有的声明优先**——重复声明同名工具会让请求非法。

### 4.3 执行侧：复用服务端工具循环

注入的工具名走的是 `coretool.NormalizeToolName` 的
`tingly_box_mcp__<source>__<tool>` 命名规范，source 固定为
`coretool.WebProxySourceID = "webproxy"`。

> **web proxy 不是 MCP。** 它没有 MCP server、没有 source config、不在
> runtime 注册。借用命名规范只是为了复用**已有的服务端工具循环**——那条
> 路径已经知道怎么"在服务端执行一个工具调用、把结果续进对话、全程不给
> 客户端看"，重写一遍没有任何收益。

三处衔接：

| 位置 | 改动 |
|---|---|
| `internal/mcpserver/tool_classification.go` | `sourceID == WebProxySourceID` → virtual（与 advisor 的特例并列） |
| `internal/protocolserver/mcp_tool_error.go` | `CallMCPToolWithHooks` 在 MCP executor **之前**分流到 `WebProxyService.Execute`——否则会被 MCP 的 callable guard 拒掉 |
| `internal/protocolserver/protocol_handler.go` | `serverToolLoopEnabled(c)` 取代各 dispatch gate 里的 `mcpEnabled()`：MCP 开着**或** web proxy 在 context 里激活，都要开循环 |

`Execute` 的失败语义：**任何失败路径都返回非空的 error ToolResult**（而不
是只返回 error）。下游模型正卡在工具循环里，给它一个空结果等于让它挂死；
给它一句可读的错误，它至少能换个思路或如实告诉用户。

空结果（搜到了但没内容）不算失败，返回 `"No results."`——明确告诉模型别
用同一个查询再试一次。

### 4.4 借来的调用怎么发出去

`poolWebClient` 按 `provider.APIStyle` 分流（与 `poolVisionClient` 同构）：

- **Anthropic style** —— 挂上 native server tool
  (`web_search_20250305` / `web_fetch_20250910`) 发一轮流式请求，用共享
  assembler 折回单条消息，取文本。流式的理由与 vision proxy 相同：不少
  Anthropic 兼容网关只对 server tool 轮次实现了流式端点。
- **OpenAI style** —— 发一轮**普通 chat completion**。OpenAI Chat 没有可移植
  的 native web 工具，这条路径依赖借来的模型**自身**具备联网能力
  （Perplexity Sonar、`*-search-preview`、带 grounding 的网关模型）。
  没有联网能力的模型会凭记忆回答。
- 其它 style —— 报错，转成模型可见的 tool error。

> **已知取舍**：OpenAI style 目前不走 Responses + `web_search_preview`。
> 那需要按 provider 的 endpoint mode 分流，且在不支持的 provider 上会
> 直接 400；当前先保守。要联网确定性时配 Anthropic style 的服务。

---

## 5. UI

两个作用域两个落点，**外观、交互与 Vision Proxy 完全一致**——同一类功能
不该有两套心智模型。

### 5.1 Scenario 级：场景 plugin 行

落点 `frontend/src/components/PluginFeatures.tsx`。按钮形态：

| 状态 | 按钮 |
|------|------|
| 未配 | `Web Proxy: Off`（灰） |
| 已配 | `Web Proxy: <model>`（高亮，tooltip 显示 `provider / model`） |

点击弹下拉：`Off`（直接清空 = 关闭）/ `On — <model>`（进
`ModelSelectDialog` 选模型，**选模型即启用**）。

持久化读写 `Extensions["web_proxy_service"]`，**不调任何 flag 端点**。

> ⚠️ 沿用 vision proxy 踩过的坑（见 `vision-proxy.md` §6.2）：后端
> `SetScenarioConfig` 是整体替换，任何 `setScenarioConfig` 前必须先
> GET-merge，否则会把 `web_proxy_service` 一并抹掉。

### 5.2 Rule 级：Rule extensions catalog

落点 `FlagCatalogDialog.tsx`。registry 里 `web_proxy_service` 的
type=`service_ref`，catalog 按类型分支渲染，**无需为新 flag 加任何
switch/case**——这正是 registry 驱动的收益。

### 5.3 类型层（camel ↔ snake）

```ts
// RoutingGraphTypes.ts
export interface ServiceRef { provider: string; model: string }

interface RuleFlags     { visionProxyService?: ServiceRef; webProxyService?: ServiceRef }
interface RuleFlagsApi  { vision_proxy_service?: ServiceRef; web_proxy_service?: ServiceRef }
```

`rule-card/flagHelpers.ts` 的 `apiToFlags` / `flagsToApi` 是**纯粹的通用
camel↔snake 转换**（不是逐 flag 的映射表），所以新增 flag 只需要在这两个
interface 里各加一行，转换逻辑无需改动。

原来的 `VisionProxyServiceRef` 改名为 `ServiceRef`——`service_ref` 现在有两
个 flag 用，类型名再绑定在其中一个上会误导。

### 5.4 顺手修掉的：service_ref picker 的写死标题

`FlagCatalogDialog` 里的模型选择弹窗标题原本写死 "Pick Vision Proxy
Model"，未配置时的按钮文案写死 "Select vision model…"。只有一个
`service_ref` flag 时看不出问题；第二个一进来，用户给 Web Proxy 选模型
会看到 "Pick Vision Proxy Model"。标题改为从当前编辑的 `spec.label` 推导。

同理，前端两个控件的重复实现被抽成
`flags/ServiceRefControl.tsx`——`VisionProxyControl` / `WebProxyControl` 只
剩文案差异。两个功能因此天然保持同一套交互，不会各自漂移。

---

## 6. 与已有 MCP webtools 的关系

`internal/mcp/tools`（`webtools` source，Serper + Jina 后端）已经提供
`mcp_web_search` / `mcp_web_fetch`，并有
`NativeWebSearchStripTransform` 在它们可用时剥掉 native web 工具。

**两者平行、互不依赖**：

| | MCP webtools | web proxy |
|---|---|---|
| 后端 | Serper / Jina（需 API key） | 另一个 LLM provider |
| 配置粒度 | 全局 MCP source 开关 | rule / scenario |
| 命名 | `tingly_box_mcp__webtools__mcp_web_*` | `tingly_box_mcp__webproxy__web_*` |
| 剥 native 工具的条件 | MCP 工具就绪 | web proxy 已配置 |

两者同时开启时，下游模型会同时看到两组工具，各自独立可用——名字不冲突，
执行路径也不共享（`CallMCPToolWithHooks` 按 source 分流）。这是刻意的：
web proxy 不该因为 MCP 的配置状态而行为不同。

---

## 7. 关键文件索引

| 功能 | 文件 |
|------|------|
| 包入口 / 说明 | `internal/webproxy/{doc.go,README.md}` |
| 作用域解析 + context stash | `internal/webproxy/resolve.go` |
| 工具定义 / 命名 / 参数解析 | `internal/webproxy/tools.go` |
| 请求侧 transform（剥 + 注入） | `internal/webproxy/transform.go` |
| 执行入口 `Service.Execute` | `internal/webproxy/service.go` |
| 借调上游的客户端 | `internal/webproxy/web_proxy_client.go` |
| 测试替身 | `internal/webproxy/webproxytest/stub.go` |
| `RuleFlags` + `WebProxyService` | `internal/typ/type.go` |
| Flag registry 条目 | `internal/typ/flag_registry.go` |
| Extensions key 常量 | `internal/server/config/extension_keys.go` |
| 命名空间常量 | `internal/tool/name.go` (`WebProxySourceID`) |
| 作用域 merge + ctx stash + transform 装配 | `internal/protocolserver/rule_flags.go` |
| 工具循环开关 | `internal/protocolserver/protocol_handler.go` (`serverToolLoopEnabled`) |
| 工具调用分流 | `internal/protocolserver/mcp_tool_error.go` |
| virtual 分类 | `internal/mcpserver/tool_classification.go` |
| 服务构造 / 依赖注入 | `internal/server/server.go`, `internal/protocolserver/protocol_handler.go` |
| Scenario 级 UI | `frontend/src/components/PluginFeatures.tsx` |
| Rule 级 UI | `frontend/src/components/rule-card/FlagCatalogDialog.tsx` |

---

## 8. 测试

| 层 | 用例 |
|----|------|
| `Resolve` 优先级 | rule+scenario → rule；只 rule；只 scenario；都无 → nil；rule 半填 → 回退 scenario；nil rule + scenario → scenario |
| `ParseScenarioService` | nil / 缺键 / 结构错 / 缺 provider / 缺 model → nil；齐备 → 有值 |
| context 往返 | 半填不激活；齐备可读回；裸 context 不激活 |
| `ToolTransform` | 未激活 = no-op；OpenAI Chat / Anthropic v1 / beta 各自剥 native + 注入两个工具 + 保留无关工具；Responses **只剥不注**；同名不重复注入 |
| `Service.Execute` | search / fetch 参数透传 + 借到的 service 正确；无 service / 无 client / 非本包工具 / 缺参数 / 参数非 JSON / 上游报错 → **一律 error ToolResult 而非空**；空答案 → `"No results."` 且不是错误 |
| `IsVirtualTool` | webproxy 命名空间在空 registry / nil registry 下都是 virtual；别的 source 未注册时不是；裸名字不是；三个 adapter 答案一致 |
| flag merge | scenario-only / rule-wins / 都无三种情况下 `flags.WebProxyService` 与 context stash 一致 |
| preVendor 装配 | 已配 → 出现 `web_proxy_tools`；未配 / 半填 → 零 transform |
| Flag registry 暴露 | `web_proxy_service` type=`service_ref`、category=`web` |
| 类型反序列化 | JSON 圆环保持一致；未设置时字段消失 |
| **端到端（flag 矩阵）** | `internal/protocoltest/flags.go` 的 `web_proxy_service` 用例：一次请求里同时验证请求侧（native `web_search` 未到下游、`keep_me` 保留、两个代理工具已注入）和执行侧（下游模型调用后，配置的联网服务确实被打到） |

---

## 9. 当前不做

- **不缓存**搜索 / 抓取结果，同一查询在不同请求里会重复借调。
- **不覆盖 Responses 目标的执行侧**：该目标没有服务端工具循环，所以只剥
  native 工具、不注入。要让 Responses 目标也能借能力，得先给它补上循环。
- **不处理历史里的 `server_tool_use` / `web_search_tool_result` block**：
  只有"这段对话之前直连过原生联网 provider、现在改道到不认识这些 block
  的下游"才会遇到。走 web proxy 产生的对话不会出现这些 block。
- **不自动挑选联网模型**：用户显式配 `{provider, model}`，不做能力探测。
