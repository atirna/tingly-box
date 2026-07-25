# Research: 抹平 `internal/remote_control` 残留的平台兼容逻辑

**类型**: 架构研究 + 重构提案（未实施；方向已拍板，见 §10）
**日期**: 2026-07-25（rev.3 — 补入 §5.5-5.7 平台专有能力的三层模型与逃生舱）
**范围**: `internal/remote_control/**`、`remote/channel/imchannel/**`、`imbot/**`
**前置文档**: `.design/bot-arch.md`（三层模型）、`.design/ux-principles.md`

---

## 0. 摘要

imbot 模块化确实抹平了**连接层**（认证、收发、重连、分片）的平台差异，但没有抹平
**交互层**（键盘/卡片、消息编辑、回复上下文、能力判定）。结果是 `remote_control`
仍然在扮演"渲染器 + 能力判官"这两个本该属于 imbot 的角色。

一句话根因：

> imbot 抽象了**入站**（`core.Message` 归一化）和**发送动作**（`SendMessage`），
> 但没有抽象**出站载荷**。出站载荷走的是无类型的 `Metadata map[string]interface{}`，
> 于是调用方必须知道"我在跟谁说话"，才能往这个口袋里塞对的东西。

这不是"再抽几个 helper"能解决的，而是缺了一条 **seam（接缝）**：
`SendMessageOptions` 里没有平台中立的交互载荷字段。

**代价已经实际发生**：Feishu/Lark 上 remote_control 的所有内联按钮（Clear / CD /
Project / 目录浏览 / `/resume` 选择器）**当前是不显示的**——不是降级成文字，是静默
丢失。详见 §3.1。这不是猜测，是类型开关不匹配导致的确定性行为，可复现。

**提案**: 引入 4 条 seam（出站交互载荷、回复上下文、能力表、消息重述），把 5 类
平台逻辑从 `remote_control` 推回 imbot。分 5 个阶段，每阶段独立可发布、可回滚。
预计净减少 `remote_control` 约 400 行、消除 21 处平台条件分支，并顺带修掉 Feishu
按钮丢失这个真实缺陷。

**但真正的难点不在这 5 类，而在 Telegram 的内联键盘模型本身** —— 它是所有目标平台
里约束最强的一个（64 字节 callback_data、消息级绑定、ACK 义务），而且它**已经悄悄
成为整个应用的最小公分母**：`telegram_dir_browser.go` 的索引式导航和
`BindFlowState.Dirs` 快照，纯粹是为了绕开 64 字节限制而发明的架构，却强加给了
Feishu（button value 是任意 JSON，根本没有这个限制）。单独立为 §5 讨论。

---

## 1. 现状盘点：残留的平台逻辑到底是什么

`internal/remote_control` 共 67 个 `.go` 文件 / 15,014 行。平台字面量出现在 19 个
非测试文件中。按**性质**（而不是按文件）归类，只有 6 类：

| # | 类别 | 站点数 | 代表位置 | 性质 |
|---|------|--------|----------|------|
| A | 出站键盘/卡片按平台预渲染 | 13 | `handler_send.go:91`、`bot_agent.go:167`、`command.go:355,777`、`telegram_callback.go:234,265`、`bot_command.go:221`、`agent_claude_code.go:248`、`feature/telegram_dir_browser.go:386,396`、`remote/channel/imchannel/imprompter.go:176` | **抽象缺失** |
| B | Weixin `context_token` 手工透传 | 19 | `handler_send.go:22,65,93`、`bot_agent.go:171`、`bot_file_send.go:56`、`prompt_reply.go:36` | **抽象缺失** |
| C | Telegram-only 的消息编辑 / 撤键盘 | 8 | `telegram_callback.go:288,302`、`bot_command.go:279,295`、`feature/telegram_dir_browser.go:383` | **抽象缺失** |
| D | 平台能力/默认值的 switch | 5 | `manager.go:170-176,201-228,403-417`、`chat_store.go:91-95`、`handler_verbose.go:7`（已注释） | **表格缺失** |
| E | 平台专有实现寄居在 remote_control | 3 个文件 / 819 行 | `feature/feishu_card_renderer.go`(161)、`feature/weixin_qr_client.go`(244)、`file_store.go:114-190` | **归属错误** |
| F | 命名误导（实际中立却叫平台名） | 3 个文件 | `feature/telegram_keyboard.go`（内容完全中立）、`feature/telegram_dir_browser.go`、`telegram_callback.go` | **命名债** |

注意 A 类里有一条在 `remote/channel/imchannel/imprompter.go` —— 说明泄漏**已经越过
了 remote_control 的边界**，`notify` consumer 走的共享 prompter 也在做同样的事。
只修 `remote_control` 是修不干净的。

### 1.1 A 类的典型形态

```go
// internal/remote_control/bot/handler_send.go:90-91
kb := feature.BuildActionKeyboard()                       // ① 中立构建
tgKeyboard := imbot.BuildTelegramActionKeyboard(kb.Build()) // ② 渲染成 Telegram 结构
...
opts.Metadata = h.buildTrackedActionMenuMetadata(hCtx, tgKeyboard, actionCard) // ③ 塞口袋
```

第 ③ 步展开（`card_metadata.go:29-41`）：

```go
metadata["replyMarkup"] = tgKeyboard          // Telegram 形状
metadata["card"]        = card                // 中立形状
if hCtx.Platform == PlatformFeishu || PlatformLark {
    metadata["card_json"] = feishuRenderer.Render(card)  // Feishu 形状
}
```

**同一个调用点，为三个平台各渲染一份，指望正确的那份被捡起来。** 这就是"抹平"没做完
的确证：调用方仍然需要一张"谁认哪个 key"的心智地图。

而 imbot 自己也被这个模式反噬了 —— `imbot/platform/tingly/adapter.go:13-20`
的注释是自白：

```go
// 生产调用方往 metadata["replyMarkup"] 里塞两种形状之一：
//   - imbot.InlineKeyboardMarkup（通用 interaction 包），或
//   - models.InlineKeyboardMarkup（Telegram 专有，由 imbot.BuildTelegramActionKeyboard
//     产生，被 remote_control/bot 广泛使用）。
// 我们两种都接，这样 tingly 无论调用方用哪个 API 都能忠实捕获键盘。
```

一个平台适配器为了兼容**调用方的历史习惯**而做双形状解码 —— 抽象的方向反了。

---

## 2. imbot 侧：抽象不是没有，是够不着 / 没人用

这是本次调研最重要的发现。以下四件东西**已经存在于 imbot**，但 `remote_control`
一个都没用上：

| 已有抽象 | 位置 | 现状 |
|----------|------|------|
| `menu.Adapter`（`ConvertMenu`/`ShowMenu`/`HideMenu`/`UpdateMenu`/`ParseAction`）+ `menu.Registry` | `imbot/menu/adapter.go`，telegram/feishu/lark 三个实现 | **imbot 之外零引用**。正好是 A+C 类需要的东西 |
| `core.PlatformDescriptor{Capabilities, Reactions}` 单一事实表 | `imbot/core/platforms.go:30-184` | 只被 `imprompter.go:151` 用了一次（`SupportsInteraction()`）；D 类的 switch 全部绕过它 |
| `core.Message.ContextToken` + 入站已归一化 | `imbot/platform/weixin/adapter.go:60`、`wecom/adapter.go:45` | 入站归一化了，出站却要调用方手工再塞回去（B 类 19 处） |
| `imbot/platform.PlatformConfigs`（认证字段规格 `FieldSpec`） | `imbot/platform.go:25-202` | `manager.go:201-228` 的 `buildAuthConfig` 和 `:403-417` 的 `hasValidAuth` 各写了一套并行 switch，没读这张表 |

**结论**：与其新造抽象，不如让已有抽象**可达且强制**。这直接降低了本次重构的
设计风险 —— 大部分目标形态在仓库里已经有实现和测试。

---

## 3. 抽象泄漏已经造成的真实缺陷

### 3.1 Feishu / Lark 上 remote_control 的内联按钮全部丢失（确定性）

调用链：

```
remote_control → metadata["replyMarkup"] = models.InlineKeyboardMarkup  (go-telegram 类型)
                                    │
feishu.Bot.SendMessage (bot_sdk.go:348)
  → sendText (:367)
    → :384  if _, has := opts.Metadata["replyMarkup"]; has → sendInteractiveCard(...)
      → buildInteractiveCard (:505)
        :516  if kb, ok := replyMarkup.(interaction.InlineKeyboardMarkup)      → 不匹配
        :528  else if kbMap, ok := replyMarkup.(map[string]interface{})        → 不匹配
        :551  if len(buttons) > 0 { ... }                                      → buttons 为空
```

`models.InlineKeyboardMarkup` 不在这个类型开关里 → `buttons` 为空 → 发出去的是一张
**只有文本、没有任何按钮**的卡片。用户看到消息，但看不到 Clear / CD / Project。

而 `card_metadata.go:33-37` 为 Feishu 特意渲染的 `metadata["card_json"]` **从未被消费**
—— 全仓库只有 `imbot/platform/feishu/menu.go:97,177` 读它，那是 `menu.Adapter` 路径，
`remote_control` 从不走。`metadata["card"]` 同理，Feishu 发送路径不读。

即：**为 Feishu 写的那 161 行 `feishu_card_renderer.go` 目前是死代码，而 Feishu 用户
拿到的是无按钮卡片。** 这正是 A 类抽象缺失的账单。

> 待办：合入前用真实 Feishu bot 跑一次确认（本环境无凭据）。类型开关的推导是确定的，
> 但"用户实际看到什么"应当由真机验证背书。

### 3.2 非 Telegram 平台上键盘撤不掉（C 类）

`telegram_callback.go:287-298`、`bot_command.go:279-297`、
`feature/telegram_dir_browser.go:382-384` 全部形如：

```go
if tgBot, ok := imbot.AsTelegramBot(bot); ok {
    tgBot.RemoveMessageKeyboard(...)
}
// else: 静默什么都不做
```

Feishu 支持卡片更新，tingly 支持消息编辑，但因为 `AsTelegramBot` 是**具体类型断言**
（`bot.(*telegram.Bot)`，见 `imbot/imbot.go:31-37`），它们永远走不进去。用户在
Feishu 上点完按钮，旧键盘一直挂着，可以重复点击进入陈旧状态 —— 而 `bot_command.go:277`
的注释明说撤键盘就是为了防这个。

### 3.3 `handler_verbose.go:7` 的能力判定被注释掉了

```go
// Check if platform supports verbose mode
//if !SupportsVerboseMode(h.botSetting.Platform) {
//	return false
//}
```

函数体注释掉、注释文字还留着（`handler_constructor.go:184`："Returns false for
platforms that don't support verbose mode (e.g., Weixin)"）。文档说的和代码做的不一致。
这是 D 类没有能力表可依的直接后果 —— 能力判定无处安放，就被注释掉了。

---

## 4. 设计：四条 seam

设计目标（按优先级）：

1. **新增一个平台，不需要改 `remote_control` 一行。** 这是"抹平"的可证伪定义。
2. **调用方表达意图，平台决定呈现。** 调用方说"给这条消息挂三个动作按钮"，不说
   "塞一个 `models.InlineKeyboardMarkup` 到 `replyMarkup`"。
3. **能力是查表得来的，不是 switch 出来的。**
4. 每一步独立可发布，不要一次性大爆炸。

### Seam 1 — 出站交互载荷进类型系统

`core.SendMessageOptions` 增加**类型化、平台中立**的字段，取代 `Metadata` 里的
`replyMarkup` / `card` / `card_json` 三件套：

```go
type SendMessageOptions struct {
    Text      string
    Media     []MediaAttachment
    ReplyTo   string
    ...
    // MessageActions 是挂在"这条消息"上的动作（消息级作用域）。各平台自行渲染：
    // Telegram → inline keyboard，Feishu/Lark → 卡片 action 元素，tingly → Button，
    // 无交互能力的平台 → 由 interaction adapter 降级为编号文本。
    //
    // 命名刻意不叫 Keyboard：Telegram 的 reply keyboard 是会话级的另一个概念
    // （telegram/menu.go:142-147 已区分），两者不该共用一个词。见 §5.4(2)。
    MessageActions []interaction.Action

    // Card 是更富的中立交互载荷（标题/分节/动作）。同时设置时 Card 优先。
    Card *interaction.Card

    Metadata map[string]interface{} // 保留：平台专有逃生舱，不再承载交互载荷
}
```

按 §10 决议 1，`MessageActions` 与 `Card` **两者都保留**：`Card` 是超集，但 13 个
调用点里多数只需要一行按钮，强制包一层 `Card` 会让常见情形变啰嗦。同时设置时
`Card` 优先。

`interaction.Action` 的载荷形态见 §5.3——`Payload map[string]any` 而非
`CallbackData string`。这是 Seam 1 与 §5 的交汇点，也是 Phase 2 要拆成 2a/2b 的原因。

渲染责任下沉到各 `platform/*/`：

- `telegram`：`Keyboard` → `models.InlineKeyboardMarkup`（把 `imbot/util.go:23` 的
  `BuildTelegramActionKeyboard` 搬进 `platform/telegram`，从公共 API 移除）
- `feishu`/`lark`：`Keyboard`/`Card` → lark 卡片。**`feature/feishu_card_renderer.go`
  整体搬到 `imbot/platform/feishu`**。文件头那句"defined in internal/remote_control
  to avoid import cycles with imbot/platform packages"是**过时的**：它只需要
  `interaction.Card`，而 `feishu/bot_sdk.go:516` 已经直接 import 了 `interaction`，
  不存在环。
- `tingly`：删掉 `adapter.go:22-45` 的双形状解码，只留中立形状
- 其余平台：无交互能力者交给现有 `interaction.Adapter.BuildFallbackText` 降级

**兼容策略**：`Metadata["replyMarkup"]` 保留一个版本，读到时打 deprecation 日志并
按老路径走，让迁移可以分批。

**这一条同时修掉 §3.1。**

### Seam 2 — 回复上下文由 imbot 自己记，不由调用方搬

现状：入站 `weixin/adapter.go:60` 已经把 `context_token` 归一化进
`core.Message.ContextToken`，但出站要调用方从 `hCtx.Message.Metadata["context_token"]`
里挖出来再塞回 `opts.Metadata` —— 19 处，每处 6 行。

目标：`core.BaseBot` 维护 per-chat 的"最近一次入站消息上下文"，`SendMessage` 时
自动补齐；`weixin/weixin.go:237` 的 `getContextToken` 从"读调用方 metadata"改成
"读自己的缓存，metadata 仅作显式覆盖"。

调用方需要精确指定回复目标时，用类型化字段而非魔法 key：

```go
opts.InReplyTo = &hCtx.Message   // 可选；不设则用该 chat 的最近一条入站消息
```

`remote_control` 侧 19 处透传代码全部删除。

> 注：`weixin.go:243-246` 的注释说"新 SDK 内部管理 context token，返回空让 SDK 处理"
> —— 需要在实现前确认手工透传是否已经**部分冗余**。若已冗余，这一条的收益更大、
> 风险更低。

### Seam 3 — 能力与默认值统一到 `PlatformDescriptor`

扩展 `imbot/core/platforms.go` 已有的单一事实表，把 D 类 switch 变成查表：

```go
type PlatformDescriptor struct {
    ID           Platform
    DisplayName  string
    Capabilities *PlatformCapabilities
    Reactions    map[ReactionToken]string

    // 新增：产品级默认行为
    Behavior PlatformBehavior
}

type PlatformBehavior struct {
    // RequiresPairingByDefault：仅凭 bot token 就能获得完整 DM 命令权限的平台
    // （telegram/discord/slack）默认开启 TOFU 配对。
    RequiresPairingByDefault bool
    // SupportsVerbose：能承受中间态流式消息的平台。
    SupportsVerbose bool
    // CommandMenuSetup：把命令注册表推送到平台原生菜单；nil 表示该平台无此概念。
    CommandMenuSetup func(bot Bot, reg *command.CommandRegistry) error
}
```

对应替换：

| 现状 | 目标 |
|------|------|
| `chat_store.go:88-95 PlatformDefaultsRequirePairing` | `GetPlatformBehavior(p).RequiresPairingByDefault` |
| `handler_verbose.go:7`（注释掉的） | `GetPlatformBehavior(p).SupportsVerbose`，恢复启用 |
| `manager.go:168-178` 菜单 switch | `if f := GetPlatformBehavior(p).CommandMenuSetup; f != nil { f(bot, reg) }` |
| `manager.go:201-228 buildAuthConfig` | 由 `imbot/platform.go` 的 `PlatformConfigs[p].Fields` 规格驱动 |
| `manager.go:400-417 hasValidAuth` | 同上，`FieldSpec.Required` 即校验规则 |
| `manager.go:58-67` Weixin options | 由 `FieldSpec` 标注"该字段进 Options 而非 AuthConfig" |

`manager.go` 里所有平台字面量因此消失。

**UX 收益**（对齐 `.design/ux-principles.md` #6「合理默认优于多一个开关」）：认证字段
校验规则与前端表单渲染读同一张表，不会再出现"前端让你填、后端不校验"或反之的漂移。

### Seam 4 — 「消息重述」能力，而不是「编辑消息」能力

原稿把这条设计成 `EditableMessages`（编辑 / 撤键盘）。**已按讨论修正**：编辑是
*手段*，不是*意图*。调用方真正想表达的是"这条消息连同它的按钮已经过期了，用这个
新状态取代它"——至于平台是原地编辑、重发卡片、还是发一条替代消息并撤掉旧键盘，
应该由平台决定。

```go
// core 包内定义，任何平台都可实现
type MessageRestater interface {
    // Restate 用新内容取代一条既有消息的呈现。平台自行选择实现路径：
    // 原地编辑、卡片更新、或"发替代消息 + 撤旧键盘"。
    // Actions 为 nil 表示"取代后不再有可点击动作"。
    Restate(ctx context.Context, ref MessageRef, text string, actions []Action) error
}

func AsRestater(bot Bot) (MessageRestater, bool)   // 接口断言，非具体类型
```

各平台的实现自由度正是这条 seam 的价值：

- **telegram**：`editMessageText` + `editMessageReplyMarkup`（已有实现，去掉
  `ctx interface{}` 这个历史遗留签名）
- **feishu/lark**：卡片更新 API；**若真机验证发现卡片更新会给用户推新通知**
  （§10 决议 2 的顾虑），改成"撤掉旧卡片动作 + 发一条替代消息"，**接口不变**
- **tingly**：测试环境同步实现，让 E2E 覆盖非 Telegram 路径
- **其他**：不实现，`AsRestater` 返回 false，调用方逻辑与今天一致（什么都不做）

这样 §9 决议 2 那个"编辑还是替代"的问题**不再是阻塞项**——它退化成 feishu 包内部
的一个实现选择，真机验证的结论不会反过来推翻接口设计。

`AsTelegramBot` 缩回真正 Telegram 专有的东西（`ResolveChatID` —— `/join` 命令本就
是 Telegram-only 且已用 `WithPlatforms(PlatformTelegram)` 正确声明，见
`command.go:535-536`，这一处是**合理**的平台特化，保留）。

### 附带：归属与命名清理

| 动作 | 理由 |
|------|------|
| `feature/feishu_card_renderer.go` → `imbot/platform/feishu/card_render.go` | Seam 1 的前提；原"避免循环依赖"注释已过时 |
| `feature/weixin_qr_client.go` → `imbot/platform/weixin/qr_client.go` | 纯 Weixin API 客户端，唯一调用方是 `internal/command/remote_add.go:363`，与 remote_control 无关 |
| `file_store.go:114-190` Telegram getFile → `telegram.Bot` 实现 `core.FileResolver` | `imbot/core/media_url.go:18` 已经定义了 `tgfile://` scheme 却不解析它；解析逻辑（含 bot token）不该在 remote_control。顺带删掉 `SetTelegramToken` 这条 token 二次传递链（`handler_constructor.go:52-54`、`handler_message.go:205`） |
| `feature/telegram_keyboard.go` → `feature/action_menu.go` | 内容 100% 中立（全部走 `imbot.NewKeyboardBuilder`），名字在骗人。对应 UX 原则 #3「命名碰撞必须拆开」 |
| `feature/telegram_dir_browser.go` → `feature/dir_browser.go`；`telegram_callback.go` → `callback.go` | 同上，Seam 1+4 完成后其内容确实中立 |

---

## 5. 最大的复杂点：Telegram 内联键盘不能再当最小公分母

前面四条 seam 处理的是"渲染在错的地方"。这一节处理的是更深的一层：
**Telegram 的键盘模型约束最强，而它的约束已经泄漏成了全平台的应用架构。**

### 5.1 四个平台的交互模型其实不同构

| | 按钮身份 | 载荷上限 | 作用域 | ACK 义务 | 更新方式 |
|---|---|---|---|---|---|
| **Telegram** | `callback_data` 字符串 | **64 字节**（硬限，超了整条消息被 API 拒） | 消息级（inline）/ 会话级（reply keyboard，另一套概念） | **必须** answer，否则客户端转圈 ~15s | `editMessageReplyMarkup` 原地改 |
| **Feishu/Lark** | button `value` | 任意 **JSON 对象**，无实际上限 | 卡片级 | 无 | 重发 card content |
| **Discord** | `custom_id` | 100 字符 | 消息级 | **3 秒内**必须 ACK，interaction token 15 分钟 | 编辑消息 components |
| **Slack** | `action_id` + `value` | value ≤2000 字符 | Block 级 | **3 秒内** ACK，`response_url` 30 分钟/5 次 | `response_url` 重发 |
| **tingly** | 任意 | 无 | 任意 | 无 | 任意 |

Telegram 在**每一列**都是最紧的那个（或者是唯一有该约束的那个）。所以任何"直接把
Telegram 的形状抬举成通用接口"的抽象，都会把它的约束强加给其他平台。

### 5.2 这件事已经发生了，有代码为证

**证据 A —— 目录浏览器被迫发明索引式导航**（`feature/telegram_dir_browser.go:274`）：

```go
// Directory buttons (use index instead of path to avoid 64-byte limit)
callbackData := imbot.FormatCallbackData("bind", "dir", fmt.Sprintf("%d", i))
```

因为路径塞不进 64 字节，按钮只能携带**下标**，于是必须在服务端保存一份
`BindFlowState.Dirs []string` 快照（`feature/telegram_keyboard.go:21`）来做下标还原，
还得配套 `ExpiresAt`、`Page`、`MessageID` 一整套会话状态。

这套状态机**只为 Telegram 的 64 字节而存在**，Feishu 完全不需要（button value 直接
放整个路径即可），但今天 Feishu 也得吃这套——包括快照过期后下标错位的风险。

**证据 B —— `FormatDirPath` 的编码方案是 Telegram 私货，且会在 Feishu 上损坏**
（`imbot/interaction/keyboard.go:138-143`）：

```go
func FormatDirPath(path string) string {
	// Telegram callback data max length is 64 bytes
	return strings.ReplaceAll(path, ":", "\x00")   // 注释提了 64 字节，代码没管
}
```

- 它把 `:` 换成 **NUL 字节**，只因为 `FormatCallbackData` 用 `:` 做分隔符。
  这个"扁平字符串 + 冒号分隔"协议本身就是 Telegram 的 64 字节逼出来的。
- Feishu 的 button `value` 是 **JSON 对象**，NUL 字节进 JSON 字符串是要么被转义成
  `\u0000` 要么直接非法——这个编码在非 Telegram 平台上是**会坏的**。

**证据 C —— 64 字节从未被校验，是个潜在真 bug**：

全仓唯一提到 64 的地方是上面那行注释，**没有任何一处做长度检查**。而
`feature/telegram_keyboard.go:62` 仍然直接把原始路径塞进去：

```go
imbot.CallbackButton("✅ Create", imbot.FormatCallbackData("bind", "create", imbot.FormatDirPath(path)))
```

`"bind:create:"` 是 12 字节，所以路径超过 **52 字节**就会让 Telegram API 返回
`BUTTON_DATA_INVALID`，**整条消息发不出去**。
`/home/user/projects/my-company/backend-services/api-gateway` 是 58 字节——不是极端
构造，是日常路径。dir browser 那边绕开了，这条 `/cd` 创建确认路径没有。

### 5.3 结论：按钮身份不能是 `callback_data`

上面三条证据指向同一个设计错误：**把 Telegram 的传输编码当成了按钮的身份**。

正确的 seam 是——**调用方给按钮附一个任意大小的应用载荷，由 imbot 负责让它到达对端**：

```go
type Action struct {
    ID      string         // 稳定的动作名，如 "bind.create"
    Label   string
    Payload map[string]any // 任意大小；调用方不关心怎么送过去
}
```

各平台自行决定投递方式：

- **Feishu/Lark**：`Payload` 直接进 button `value` JSON。零损耗。
- **Discord / Slack**：编码进 `custom_id` / `value`；超限再走 token。
- **Telegram**：先尝试编码进 64 字节；**放不下时，由 imbot 侧存一个短 token**
  （随消息生命周期回收，与 `pendingRequests` 同一套过期机制），`callback_data`
  只带 `tok:<8字节>`，回调时还原成完整 `Payload` 再交给上层。

**收益**：`BindFlowState.Dirs` 快照、索引式导航、`FormatDirPath` 的 NUL 编码，
**这三样全部可以删掉**。dir browser 回归"按钮就带它自己的路径"这个本来该有的写法，
Feishu 上零成本拿到正确行为，Telegram 上由 imbot 透明降级。§5.2 证据 C 的潜在 bug
也随之消失——长度处理成为平台的责任，而不是每个调用方都要记得的事。

### 5.4 另外两件必须显式建模的 Telegram 特性

**(1) ACK 语义**。今天 `telegram/telegram.go:186-191` 在 `EmitMessage` **之后立刻**
无条件 answer，所以不转圈。但这也意味着：answer 的 toast/alert 能力用不上，且这个
"先 ACK 后处理"的选择被硬编码在平台里。Discord/Slack 的 3 秒硬性 ACK 让这件事不能
一直含糊——抽象里需要一个显式的 `Ack(ctx, interaction, opts)` 概念，Telegram 的实现
可以保持"已自动 ACK，此处为 no-op"，但接口先立住，等接 Discord 时不用返工。

**(2) inline keyboard 与 reply keyboard 是两个概念**。前者绑定在**某条消息**上，
后者是**整个会话**的输入区键盘，生命周期完全不同。`telegram/menu.go:142-147` 已经
区分了 `replyKeyboard` / `replyMarkup`，但 `remote_control` 只用得到前者。
按 UX 原则 #3（命名碰撞必须拆开），Seam 1 的字段应当叫
`SendMessageOptions.MessageActions`（消息级）而不是笼统的 `Keyboard`，给未来的
会话级键盘留出独立的轴。

### 5.5 反向问题：那 Telegram 专有的键盘能力怎么办？（评审提出）

前面五小节讲的都是"别让 Telegram 的**约束**绑架其他平台"。反过来的问题同样要害：
**别让抽象把 Telegram 的能力抹掉。** 这两件事必须同时成立，否则就是从一个错误
滑到另一个。

**先说现状，比想象的糟。** 今天的中立按钮模型（`imbot/interaction/keyboard.go:8-13`）
只有三个字段：

```go
type InlineKeyboardButton struct {
	Text         string
	CallbackData string
	URL          string
}
```

而 Telegram 的 `InlineKeyboardButton` 有九种变体：`web_app`（Mini App）、
`login_url`、`switch_inline_query`（及 `_current_chat` / `_chosen_chat`）、
`copy_text`、`pay`、`callback_game`。reply keyboard 那一侧还有
`request_users` / `request_chat` / `request_contact` / `request_location` /
`request_poll`、`is_persistent`、`input_field_placeholder`。

**这些今天全都表达不了**——不是本次重构弄丢的，是本来就没有。今天唯一能用上它们的
办法，是绕过中立模型、直接往 `Metadata["replyMarkup"]` 里塞原始
`models.InlineKeyboardMarkup`。

**而 Phase 2a 恰恰要删掉这条路。** 所以如果不同时提供替代品，Phase 2a 就是一次
**能力回退**：把唯一的（虽然丑陋的）逃生舱堵死，却没开新门。这是本提案原稿的缺口，
必须在 Phase 2a **同一个 PR 内**补上，不能延后。

### 5.6 三层模型：什么该抽象，什么不该

不是所有平台差异都该被抹平。判据是**这个差异对用户是不是有意义**：

**Tier 1 — 通用动作（必须抽象）**
每个目标平台都能表达的：可点击动作 + 打开链接。这是 `Action{ID, Label, Payload, URL}`
的职责范围，也是 remote_control 今天 13 个调用点的全部所需。

**Tier 2 — 能力门控动作（抽象语义，不抽象实现）**
多个平台**各有实现但形式不同**的能力。用中立语义词表达，由平台能力表决定怎么落地：

```go
Action{Kind: ActionOpenMiniApp, URL: dashboardURL, Fallback: FallbackAsURL}
// Telegram → web_app 按钮；Feishu → 小程序卡片；其余 → 退化成普通 URL 按钮
```

关键是 `Fallback` **必须显式声明**，不能静默丢弃——静默丢弃正是 §3.1 那个 Feishu
缺陷的形成机理，不能在新设计里重演。

**Tier 3 — 平台专有能力（不抽象，但要有正门）**
只有一个平台有、且抽象它没有意义的（`pay`、`callback_game`、`switch_inline_query`）。
这类**不进中立模型**，走**类型化逃生舱**——在平台包里提供构造器：

```go
// imbot/platform/telegram
func WebAppButton(label, url string) interaction.Action
func CopyTextButton(label, text string) interaction.Action
func SwitchInlineButton(label, query string) interaction.Action
```

它们返回一个填好 `Ext[PlatformTelegram]` 的普通 `Action`，其他平台看到 `Ext` 里没有
自己的 key，按 `Fallback` 处理。

```go
type Action struct {
    ID       string
    Label    string
    Payload  map[string]any   // §5.3：任意大小，投递由平台负责
    URL      string
    Kind     ActionKind       // Tier 2 的中立语义
    Ext      map[Platform]any // Tier 3：仅目标平台读取
    Fallback FallbackPolicy   // 不支持时：Drop / AsURL / AsText / FailSend
}
```

**为什么"类型化逃生舱"比今天的 `Metadata` 口袋好**，尽管两者都能装平台专有的东西：

| | 今天 `Metadata["replyMarkup"]` | Tier 3 构造器 |
|---|---|---|
| 平台意图是否可见 | 不可见——`map[string]any`，要读实现才知道给谁的 | 可见——`import ".../platform/telegram"` 这行 import 本身就是声明 |
| 能否 grep / lint | 不能 | 能：禁止/审计 `internal/**` 对 `imbot/platform/*` 的 import |
| 类型安全 | 无，塞错形状静默丢（§3.1 就是这么来的） | 有，编译期检查 |
| 其他平台行为 | 未定义 | 由 `Fallback` 显式声明 |

**核心区别不是"有没有平台专有代码"，而是它是隐式的还是显式的。**
本次重构要消灭的从来不是"Telegram 特化"，而是"**看不见的** Telegram 特化"。

### 5.7 这条规则如何落到目录结构上

由此推出两类平台专有代码的去处——它们性质不同，不能混谈：

**(a) 平台专有的「基础设施」→ 进 imbot，remote_control 永不可见。**
菜单注册、文件 URL 解析、键盘渲染、卡片序列化。这是本提案 §4 全部内容要搬走的东西。
`manager.go:13-14` 今天 import 了 `imbotfeishu` / `imbottelegram` 就是这类越界，
Seam 3 会消除它。

**(b) 平台专有的「产品特性」→ 留在 remote_control，但必须大声。**
"在 Telegram 上用 WebApp 按钮直接打开 tingly-box 仪表盘"——这是**刻意**给某个平台
更好的体验，是产品决策，不是技术债。它就该待在 remote_control。

仓库里已经有 (b) 做对了的先例：`/join` 命令（`command.go:535-543`）——
`WithPlatforms(imbot.PlatformTelegram)` 显式声明适用平台，其他平台给明确提示而不是
静默失败。**未来的 Telegram 键盘需求应当照抄这个形状**：Tier 3 构造器 +
`WithPlatforms` 或 `Fallback` 声明。

### 5.8 对实施计划的影响

这不是"再加一个阶段"，而是**改变了 Seam 1 的目标形态**：`Keyboard` 字段不能只是
把 `interaction.InlineKeyboardMarkup` 原样搬进类型系统（那样只是把 Telegram 编码
搬了个家），必须同时引入 `Action.Payload` 与平台侧的投递责任。

因此 Phase 2 拆成两步：**2a** 先把载荷搬进类型系统（修 §3.1 的 Feishu 按钮丢失，
收益立刻兑现），**2b** 再把按钮身份从 `callback_data` 换成 `Payload`（删掉索引导航
与 NUL 编码）。2a 不依赖 2b，可以先发。

---

## 6. 方案比较

| 方案 | 做法 | 优点 | 缺点 | 结论 |
|------|------|------|------|------|
| **0. 不动** | 维持现状 | 零成本 | Feishu 按钮持续丢失；每加一个平台要改 21 处 | ✗ |
| **1. 只做归属搬迁** | 把 E 类三个文件搬进 imbot，不动 seam | 改动小、无 API 变更 | 治标：A/B/C/D 全留着，Feishu 缺陷不修 | ✗ |
| **2. 复用 `menu.Adapter`** | `remote_control` 改用已有的 `menu.Registry` | 完全不改 imbot API | `menu` 面向"菜单会话"（`MenuContext`/`MenuResult`/`menuId` 生命周期），而 remote_control 要的是"给这条消息挂几个按钮"——阻抗不匹配，会逼出一堆一次性 Menu 对象 | ✗（但其 `HideMenu` 的实现思路可直接喂给 Seam 4） |
| **3. 四条 seam（本提案）** | 载荷进类型系统 + 能力查表 + 能力接口 | 根治；新平台零改动；顺带修 2 个缺陷；大量复用已有结构 | 触碰 `SendMessageOptions` 这个核心类型，影响面跨 imbot/remote/internal 三处 | ✓ |
| **4. 彻底事件化** | 出站也走 `core.OutboundMessage` 全新类型体系 | 最干净 | `SendMessageOptions` 是全仓最热的类型，一次性替换 = 大爆炸，无法分阶段 | ✗（过度） |

**选定：方案 3。** 关键理由是 §2 —— 四个目标结构（`menu` 的平台实现、
`PlatformDescriptor`、`ContextToken`、`PlatformConfigs`）在仓库里已经有代码和测试，
本提案主要是**把它们接通**，而不是从零发明。

---

## 7. 分阶段实施

每个阶段独立可发布、可回滚。建议顺序按"收益/风险比"排：

### Phase 1 — 归属搬迁（纯机械，零行为变更）
- `feishu_card_renderer.go` → `imbot/platform/feishu`
- `weixin_qr_client.go` → `imbot/platform/weixin`
- 文件重命名（`telegram_keyboard.go` → `action_menu.go` 等）

风险：极低。纯 move + import 调整。为 Phase 2 铺路。

### Phase 2a — Seam 1 上半：载荷进类型系统 ★ 修复 §3.1
- `SendMessageOptions` 加 `MessageActions` / `Card`（此阶段 `Action` 仍沿用
  `CallbackData string`，**不动按钮身份**）
- 各平台实现渲染；`Metadata["replyMarkup"]` 降级为带 deprecation 日志的兼容路径
- 迁移 13 个调用点（**含 `imchannel/imprompter.go:176`**）
- 从 `imbot` 公共 API 移除 `BuildTelegramActionKeyboard`
- 删除 `card_metadata.go` 的 `card_json` 分支和 tingly 的双形状解码
- **同一 PR 内**：`Action.Ext` + `Action.Fallback` 字段 + `imbot/platform/telegram`
  的 Tier 3 构造器（至少 `WebAppButton`）。**这是硬性前置**——见 §5.5，移除
  `Metadata["replyMarkup"]` 会堵死今天唯一能表达 Telegram 专有按钮的通路，
  不同时开新门就是能力回退

风险：中。核心类型变更，但有兼容路径兜底。
验证：Feishu 真机确认按钮出现；`imbot/tests/telegram_e2e_test.go:83` 走新字段；
新增一个 Tier 3 按钮的往返测试（Telegram 渲染成 `web_app`，Feishu 按 `Fallback` 降级）。

**这一步单独就把 §3.1 的用户可见缺陷修掉了**，不必等 2b。

### Phase 2b — Seam 1 下半：按钮身份换成 `Payload` ★ 见 §5.3
- `interaction.Action.CallbackData string` → `Payload map[string]any`
- Telegram 侧实现"编码优先、超限走 token"的透明降级 + **补上缺失的 64 字节校验**
  （§5.2 证据 C 的潜在 bug）
- Feishu 侧 `Payload` 直接进 button `value`
- 删除 `feature/telegram_dir_browser.go` 的索引式导航与 `BindFlowState.Dirs` 快照
- 删除 `interaction/keyboard.go` 的 `FormatDirPath` / `ParseDirPath`（NUL 编码）

风险：中高——这是本次唯一触碰**回调协议**的一步，改错会让按钮点了没反应。
建议独立 PR，且先在 tingly E2E 上把"编码路径"和"token 路径"两条分支都覆盖到。

### Phase 3 — Seam 3 + Seam 4（能力表 + 消息重述）★ 修复 §3.2 §3.3
- `PlatformBehavior` 落表，替换 5 处 switch
- `MessageRestater` 接口 + feishu/tingly 实现，替换 8 处 `AsTelegramBot`
- 恢复 `handler_verbose.go` 被注释的能力判定

风险：中低。查表替换是同义改写；feishu 侧无论选"更新卡片"还是"发替代消息"，
失败都退回现状的"什么都不做"，不会更糟。

### Phase 4 — Seam 2（回复上下文）+ 清理
- `BaseBot` 记忆入站上下文，删除 19 处手工透传
- `FileResolver` 接口，Telegram getFile 下沉，删除 token 二次传递
- 移除 Phase 2a 留下的 `Metadata["replyMarkup"]` 兼容路径

风险：低。前置需确认 §Seam 2 里那条"是否已部分冗余"。

### Phase 5 — `menu` 包归位（§10 决议 3）
前四个阶段落地后，`imbot/menu` 与新 seam 会有明显重叠（`ShowMenu`/`HideMenu` ≈
`MessageActions` + `MessageRestater`）。此阶段单独评估三选一：
让 `menu` 建立在新 seam 之上（保留其"菜单会话"生命周期语义）、
把它降级为 `remote_control` 用不到的独立能力、或直接删除。

风险：低（此时它仍是零外部引用）。**刻意排在最后**——先让新 seam 的形状被真实
使用验证过，再决定 `menu` 该不该活，而不是反过来。

---

## 8. 影响面与风险

**跨仓库**：本次改动全部落在 `tingly-box` 主仓（`imbot/` 是仓内目录，非 submodule）。
`libs/` 下三个 SDK submodule 不受影响。

**风险清单**：

| 风险 | 缓解 |
|------|------|
| `SendMessageOptions` 是全仓最热类型 | 只**新增**字段，不改既有字段；`Metadata` 兼容路径保留一个版本 |
| Feishu 卡片更新 API 行为未知（会不会给用户推新通知） | 已由 §10 决议 2 化解：`MessageRestater` 只表达"取代"意图，feishu 内部选编辑还是发替代消息；两者都不可行时不实现该接口，退回当前行为，不会退化 |
| 无真实 Feishu/Weixin 凭据可测 | tingly 平台是仓内 E2E harness（`imbot/platform/tingly/testenv/`），可覆盖中立路径；平台专有渲染用单测断言输出 JSON 形状；真机验证列为合入门槛 |
| `menu` 包在本方案后更显冗余 | 已排为 Phase 5 单独处理（§10 决议 3）；此前它保持零外部引用，不构成阻塞 |
| Phase 2b 触碰回调协议，改错则"按钮点了没反应" | 独立 PR；tingly E2E 必须同时覆盖"编码进 64 字节"和"走 token"两条分支；2a 先行确保用户可见缺陷不被 2b 的风险绑架 |
| **移除 `Metadata["replyMarkup"]` = 堵死唯一的平台专有按钮通路**（评审发现） | Tier 3 逃生舱（`Action.Ext` + 平台构造器）必须与 Phase 2a **同 PR** 落地，不得延后；见 §5.5 |
| 中立按钮模型只有 `{Text, CallbackData, URL}`，Telegram 九种变体中七种今天就表达不了 | 这是**既有缺口**而非本次引入；Tier 2/3 分层给了它一个正式去处，但补齐具体按钮类型按需推进，不在本次范围 |

**明确不在范围内**：
- `internal/server/module/imbot/**`（108 处平台引用）—— 那是 HTTP facade 的
  **配置/接入向导**（Feishu 一键注册、Weixin 扫码），平台形状是**本质的**、面向用户
  的引导流程，不是需要抹平的兼容逻辑。
- `.design/bot-arch.md §9` 列的命名债（`Consumer`→`Capability`、`bot.Manager`→
  `Supervisor`、`internal/remote_control`→`remote_agent`）—— 正交议题，不要和本次
  混在一个 PR 里。

---

## 9. 验收标准

本次重构的可证伪定义：

1. **`internal/remote_control` 里不再有*隐式*平台分支。** 注意判据不是"平台字面量归零"
   —— 那会连带禁掉合理的产品特化（§5.7）。准确的判据是，每一处残留的平台引用必须
   属于以下之一：
   - `/join` 那样的**显式产品特化**：带 `WithPlatforms(...)` 或 `Fallback` 声明
     （`command.go:535-543` 是先例）
   - Tier 3 逃生舱调用：`import ".../imbot/platform/telegram"` + 显式 `Fallback`
   - 注释与日志文案

   反过来，**基础设施类**的平台分支必须归零：`buildAuthConfig` / `hasValidAuth` /
   菜单 switch / `AsTelegramBot` / 键盘预渲染 / `context_token` 透传，一处不留。
2. Feishu/Lark 上 Clear / CD / Project 按钮**可见且可点**（§3.1 修复）
3. Feishu 上点完按钮旧键盘被撤除（§3.2 修复）
4. `handler_verbose.go` 的能力判定恢复启用（§3.3 修复）
5. 在 imbot 新增一个假想平台，`internal/remote_control` **零改动**即可跑通
   `manager_channel_test.go` 的 notify 全链路
6. **回调载荷不再有长度约束泄漏到调用方**：`grep -rn "64" imbot/interaction/` 只在
   Telegram 平台包内出现；`FormatDirPath` / `BindFlowState.Dirs` 已删除（Phase 2b）

---

## 10. 决议（已拍板）

| # | 问题 | 决议 | 落点 |
|---|------|------|------|
| 1 | `Card` 与 `Keyboard` 是否都要 | **都留**，`Card` 优先 | Seam 1；字段名改为 `MessageActions`（§5.4(2)） |
| 2 | Feishu「编辑卡片」的产品语义未知 | **增加"替代"抽象能力**，而不是把编辑写死 | Seam 4 由 `EditableMessages` 改为 `MessageRestater`；编辑 / 重发由平台内部决定，真机结论不影响接口 |
| 3 | 是否借本次处理 `menu` 包 | **作为新阶段抽象** | 新增 Phase 5，排在四条 seam 之后 |

**追加约束 1（讨论中提出）**：Telegram 的键盘交互能力是本次最大的复杂点。已展开为
§5.1–5.4——结论是它的 64 字节约束已泄漏成全平台的应用架构，Seam 1 因此必须把按钮
身份从 `callback_data` 换成 `Payload`，并把 Phase 2 拆成 2a/2b。

**追加约束 2（评审提出「未来 TG keyboard 需求怎么办，也抽象吗？」）**：不是所有平台
差异都该抽象。已展开为 §5.5–5.7，确立三层模型：Tier 1 通用动作（抽象）、Tier 2
能力门控（抽象语义不抽象实现，`Fallback` 必须显式）、Tier 3 平台专有（**不抽象**，
走类型化逃生舱 `Action.Ext` + 平台包构造器）。

要点：本次要消灭的**不是"Telegram 特化"，而是"看不见的 Telegram 特化"**。因此
Phase 2a 必须同 PR 提供逃生舱——否则删掉 `Metadata["replyMarkup"]` 是能力回退。
§9 验收标准第 1 条据此改写，不再要求"平台字面量归零"。

---

## 11. 下一步

- [x] §10 三个问题达成一致
- [ ] `/sdlc spec` 产出 Phase 1 + 2a 的实施规格（含 `SendMessageOptions` 精确签名
      与兼容期策略）；Phase 2b 因触碰回调协议，单独出规格
- [ ] 本文档在方案确认后升格为 `.design/imbot-platform-seams.md`
      （放在 `.sdlc/research/` 而非 skill 默认的 `.sdlc/docs/`，因为本仓
      `.gitignore:2` 的 `docs` 规则会忽略任意层级的 `docs/` 目录）

## 参考

- `.design/bot-arch.md` — resource / channel / consumer 三层模型
- `.design/ux-principles.md` — #3 命名碰撞、#6 合理默认
- `.design/imbot-sync.md` — 边沿触发 + reconcile 兜底（本次不涉及，仅背景）
