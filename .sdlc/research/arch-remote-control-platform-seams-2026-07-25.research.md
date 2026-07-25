# Research: 抹平 `internal/remote_control` 残留的平台兼容逻辑

**类型**: 架构研究 + 重构提案（未实施，待讨论）
**日期**: 2026-07-25
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

**提案**: 引入 4 条 seam（出站交互载荷、回复上下文、能力表、可编辑消息），把 5 类
平台逻辑从 `remote_control` 推回 imbot。分 4 个阶段，每阶段独立可发布、可回滚。
预计净减少 `remote_control` 约 400 行、消除 21 处平台条件分支，并顺带修掉 Feishu
按钮丢失这个真实缺陷。

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
    // Keyboard 是平台中立的内联键盘。各平台自行渲染：Telegram → inline keyboard，
    // Feishu/Lark → 卡片 action 元素，tingly → Button，无交互能力的平台 →
    // 由 interaction adapter 降级为编号文本。
    Keyboard *interaction.InlineKeyboardMarkup

    // Card 是更富的中立交互载荷（标题/分节/动作）。同时设置时 Card 优先。
    Card *interaction.Card

    Metadata map[string]interface{} // 保留：平台专有逃生舱，不再承载交互载荷
}
```

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

### Seam 4 — 可编辑消息升级为能力接口

`imbot/imbot.go:14-37` 的 `TelegramBot` 接口 + `AsTelegramBot` 具体类型断言，拆成
按能力划分的可选接口：

```go
// core 包内定义，任何平台都可实现
type EditableMessages interface {
    EditMessage(ctx context.Context, chatID, messageID, text string, kb *interaction.InlineKeyboardMarkup) error
    RemoveKeyboard(ctx context.Context, chatID, messageID string) error
}

func AsEditable(bot Bot) (EditableMessages, bool)   // 接口断言，非具体类型
```

- telegram：已有实现，改签名（去掉 `ctx interface{}` 这个历史遗留）
- feishu/lark：用卡片更新 API 实现（**新能力**，修掉 §3.2）
- tingly：测试环境同步实现，让 E2E 能覆盖非 Telegram 路径
- 其他：不实现，`AsEditable` 返回 false，调用方逻辑不变

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

## 5. 方案比较

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

## 6. 分阶段实施

每个阶段独立可发布、可回滚。建议顺序按"收益/风险比"排：

### Phase 1 — 归属搬迁（纯机械，零行为变更）
- `feishu_card_renderer.go` → `imbot/platform/feishu`
- `weixin_qr_client.go` → `imbot/platform/weixin`
- 文件重命名（`telegram_keyboard.go` → `action_menu.go` 等）

风险：极低。纯 move + import 调整。为 Phase 2 铺路。

### Phase 2 — Seam 1（出站交互载荷）★ 修复 §3.1
- `SendMessageOptions` 加 `Keyboard` / `Card`
- 各平台实现渲染；`Metadata["replyMarkup"]` 降级为带 deprecation 日志的兼容路径
- 迁移 13 个调用点（**含 `imchannel/imprompter.go:176`**）
- 从 `imbot` 公共 API 移除 `BuildTelegramActionKeyboard`
- 删除 `card_metadata.go` 的 `card_json` 分支和 tingly 的双形状解码

风险：中。核心类型变更，但有兼容路径兜底。
验证：Feishu 真机确认按钮出现；`imbot/tests/telegram_e2e_test.go:83` 走新字段。

### Phase 3 — Seam 3 + Seam 4（能力表 + 可编辑消息）★ 修复 §3.2 §3.3
- `PlatformBehavior` 落表，替换 5 处 switch
- `EditableMessages` 接口 + feishu/tingly 实现，替换 8 处 `AsTelegramBot`
- 恢复 `handler_verbose.go` 被注释的能力判定

风险：中低。查表替换是同义改写；feishu 卡片更新是新增能力（失败即退回现状的
"什么都不做"，不会更糟）。

### Phase 4 — Seam 2（回复上下文）+ 清理
- `BaseBot` 记忆入站上下文，删除 19 处手工透传
- `FileResolver` 接口，Telegram getFile 下沉，删除 token 二次传递
- 移除 Phase 2 留下的 `Metadata["replyMarkup"]` 兼容路径

风险：低。前置需确认 §Seam 2 里那条"是否已部分冗余"。

---

## 7. 影响面与风险

**跨仓库**：本次改动全部落在 `tingly-box` 主仓（`imbot/` 是仓内目录，非 submodule）。
`libs/` 下三个 SDK submodule 不受影响。

**风险清单**：

| 风险 | 缓解 |
|------|------|
| `SendMessageOptions` 是全仓最热类型 | 只**新增**字段，不改既有字段；`Metadata` 兼容路径保留一个版本 |
| Feishu 卡片更新 API 行为未知 | Phase 3 中该能力失败时退回"不实现 `EditableMessages`"，即当前行为，不会退化 |
| 无真实 Feishu/Weixin 凭据可测 | tingly 平台是仓内 E2E harness（`imbot/platform/tingly/testenv/`），可覆盖中立路径；平台专有渲染用单测断言输出 JSON 形状；真机验证列为合入门槛 |
| `menu` 包在本方案后更显冗余 | 不在本次范围内处理。Seam 4 完成后单独评估是删除还是让它建立在新 seam 之上 |

**明确不在范围内**：
- `internal/server/module/imbot/**`（108 处平台引用）—— 那是 HTTP facade 的
  **配置/接入向导**（Feishu 一键注册、Weixin 扫码），平台形状是**本质的**、面向用户
  的引导流程，不是需要抹平的兼容逻辑。
- `.design/bot-arch.md §9` 列的命名债（`Consumer`→`Capability`、`bot.Manager`→
  `Supervisor`、`internal/remote_control`→`remote_agent`）—— 正交议题，不要和本次
  混在一个 PR 里。

---

## 8. 验收标准

本次重构的可证伪定义：

1. **`grep -riE 'telegram|feishu|lark|dingtalk|wecom|weixin|discord|slack|whatsapp'
   internal/remote_control --include='*.go' | grep -v _test` 只剩下**：
   - `/join` 命令的 `WithPlatforms(PlatformTelegram)` 声明（`command.go:535-543`，合理特化）
   - 注释与日志文案
2. Feishu/Lark 上 Clear / CD / Project 按钮**可见且可点**（§3.1 修复）
3. Feishu 上点完按钮旧键盘被撤除（§3.2 修复）
4. `handler_verbose.go` 的能力判定恢复启用（§3.3 修复）
5. 在 imbot 新增一个假想平台，`internal/remote_control` **零改动**即可跑通
   `manager_channel_test.go` 的 notify 全链路

---

## 9. 待确认问题（需要产品/架构侧拍板）

1. **`Card` vs `Keyboard` 是否都要？** `Card` 是超集（键盘 = 只有 actions 的 card）。
   只留 `Card` 更干净，但 13 个调用点里大部分只需要键盘，强制包一层 Card 会变啰嗦。
   倾向：两个都留，`Card` 优先。
2. **Feishu 上「消息编辑」的产品语义**：Feishu 卡片更新会不会给用户推新通知？如果会，
   §3.2 的修复在 Feishu 上是否反而是负 UX？需要真机确认后再决定 Phase 3 是"编辑卡片"
   还是"发一条替代消息"。
3. **是否借这次把 `menu` 包处理掉？** 它有完整实现和测试但零外部引用。倾向：本次不动，
   Phase 4 后单独评估。

---

## 10. 下一步

- [ ] 就 §9 三个问题达成一致
- [ ] 通过则 `/sdlc spec` 产出 Phase 1+2 的实施规格（含 `SendMessageOptions` 精确签名
      与兼容期策略）
- [ ] 本文档在方案确认后升格为 `.design/imbot-platform-seams.md`
      （放在 `.sdlc/research/` 而非 skill 默认的 `.sdlc/docs/`，因为本仓
      `.gitignore:2` 的 `docs` 规则会忽略任意层级的 `docs/` 目录）

## 参考

- `.design/bot-arch.md` — resource / channel / consumer 三层模型
- `.design/ux-principles.md` — #3 命名碰撞、#6 合理默认
- `.design/imbot-sync.md` — 边沿触发 + reconcile 兜底（本次不涉及，仅背景）
