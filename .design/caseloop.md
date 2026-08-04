# Caseloop — loopx × autoresearch 的原生实现（设计稿）

> 工作名 `caseloop`（可改）。目标：用一个原生工具替代「长驻 Claude Code session +
> 两个 skill + 手工 progress 文档」的调研/产 case/跑 case 工作流。
>
> 参考对象：
> - [huangruiteng/loopx](https://github.com/huangruiteng/loopx) — loop engineering 状态内核：
>   durable goal / todo claim-lease / evidence writeback / gate / quota-aware wake。
> - [karpathy/autoresearch](https://github.com/karpathy/autoresearch) — 实验循环协议：
>   生成变更 → 固定预算运行 → 单一指标评分 → accept/reject → 迭代。
>
> 本文档是设计稿，不是实现记录。

---

## 1. 问题诊断：为什么现在的做法会失效

现状：一个长驻 Claude Code session，skill A（调研 + 产 case，~3 个 subagent），
skill B（跑 case，~4 个 subagent），一个 progress 文档记录进展，结果 accept/reject
并记录理由。

三个痛点其实是同一个根因的三种表现：**工作流的状态（进度、任务状态、协议本身）
活在 LLM 的 context window 里，而 context window 是易失的**。

| 痛点 | 根因 |
|---|---|
| 1. progress 文档经常 miss 更新，压缩几次后干脆忘了这个文件 | 更新 progress 是靠模型「记得去做」的软约束。compaction 摘要一旦丢掉这条约定，约定就不存在了 |
| 2. memory 里的提示不起作用 | memory/CLAUDE.md 只是又一段 prompt，仍然是软约束，且与 compaction 后的摘要竞争注意力 |
| 3. 各 task 的 status 无法 persist | task 状态只存在于对话文本里，没有任何进程外的、有 schema 的存储 |

推论：**任何靠「提示模型去维护状态」的方案都不可能修好这三个问题**。修法只有
一种 —— 把状态挪出 context，放进一个由普通程序拥有的 durable store，并让状态
更新变成 *结构性必然*（orchestrator 从 agent 的结构化输出里写回），而不是
*行为期望*（提示 agent 记得写文档）。

这正是 loopx 的论点（state kernel + evidence writeback）加上 autoresearch 的
论点（loop 协议 + 单一 verdict）。两者拼起来刚好覆盖这个工作流。

## 2. 核心设计：控制反转

现状是「LLM session 持有 loop，工具是被调用方」。新设计倒过来：

```
现在:   Claude session (长驻, 持有 loop 和状态)
           ├── skill A → 3 × gen subagent
           └── skill B → 4 × run subagent
           └── progress.md   ← 靠模型记得去写

新设计:  caseloop orchestrator (原生进程, 持有 loop 和状态)
           │
           │  SQLite (source of truth) + events.jsonl (append-only 审计流)
           │
           ├── gen pool  (≤3 并发) ──┐    每次调用都是短生命周期、单一目的、
           ├── run pool  (≤4 并发) ──┤    带 task packet 进、结构化 JSON 出
           └── verdict/verifier ─────┘    的 headless claude 子进程
           │
           └── PROGRESS.md   ← 从 DB 渲染出的投影, 只读, 永远不是 source of truth
```

关键性质：

- **没有任何 agent 需要长驻**。每个 subagent 是一次 headless 调用：读 task
  packet → 干活 → 输出符合 JSON Schema 的结果 → 退出。compaction 不再是威胁，
  因为没有需要活过 compaction 的 session。调研型 gen 任务如果确实需要多轮，
  也是「一个 case 一个 session」，状态照样在 DB 里。
- **agent 从不直接写 progress**。orchestrator 校验 agent 的结构化输出后自己
  写库（schema 不符则带错误重试）。痛点 1、2 从机制上消失 —— 不是"提醒得更狠"，
  而是根本不存在"忘记更新"这个动作。
- **PROGRESS.md 降级为投影**。`caseloop status` / render 从 DB 生成，给人看、
  给下一个 agent 的 packet 做摘要。手改无效（会被覆盖），这是特性不是缺陷。

## 3. 领域模型

```
Goal        调研目标/方向 (对应 autoresearch 的 program.md, 人写人改)
 └── Case       gen 产出的最小工作单元 (case spec + 来源依据)
      └── Attempt   一次运行 (哪个 worker、输入、输出、日志、耗时、token 花费)
           └── Verdict   accept / reject + reason + evidence 引用
```

Case 状态机（借 loopx 的 claim/lease，避免 worker 崩溃后任务卡死）：

```
draft → ready → claimed(lease, TTL) → running → judged:accepted
                     │                    │           └────→ judged:rejected(reason)
                     └── lease 过期 → ready (重新入队)
                                          └── failed(retryable, n<max) → ready
                                          └── failed(terminal)
```

存储分两层，职责不同：

- **SQLite**：当前状态的 source of truth。单文件、事务性、并发 claim 用
  `UPDATE ... WHERE state='ready' LIMIT 1 RETURNING` 即可，天然解决抢占。
- **events.jsonl**：append-only 事件流（case_created / claimed / attempt_started /
  verdict / lease_expired …）。审计 + 可重放（DB 损坏可从事件流重建），也是
  loopx "evidence log" 的对应物。

Verdict 协议（borrow autoresearch）：

- 尽量把「好/坏」压到**单一可比指标或二值判定**，判定标准写在 Goal 里而不是
  每次靠 judge agent 现场发挥；
- verdict 必须带 `reason`（结构化字段，非自由文本附带一段引用 evidence）；
- 可选 adversarial verify：重要 case 由 N 个独立 verifier 投票（N=3, ≥2 通过），
  防止 plausible-but-wrong 的 accept。

## 4. Agent 契约（唯一需要认真设计 prompt 的地方）

每次调用给 agent 一个 **task packet**，从 DB 现算：

```jsonc
{
  "role": "gen | run | verify",
  "goal_digest": "Goal 的当前摘要（orchestrator 维护，非 agent 自述）",
  "case": { /* run/verify 时: case spec 全文 */ },
  "context": {
    "recent_verdicts": [ /* 最近 N 条 accept/reject + reason, 让 gen 不重复踩坑 */ ],
    "dedup_keys": [ /* 已存在 case 的指纹, 让 gen 去重 */ ]
  },
  "output_schema": { /* JSON Schema, 强制 */ }
}
```

要点：

- **上下文是喂进去的，不是 agent 记住的**。gen agent 之所以「知道进展」，是
  因为 packet 里有 recent_verdicts 摘要 —— 这个摘要由 orchestrator 从 DB 生成，
  永远新鲜、永远在。
- 输出走 JSON Schema 校验（claude CLI 的 structured output / 或输出末尾 JSON
  块 + 校验重试），不合格的输出不进库。
- reject 的 reason 会回流到后续 gen 的 packet 里 —— 这是整个 loop 的学习通路，
  对应 autoresearch 里「失败实验也留在 log 里指导下一次」。

## 5. 语言选型：Go，不是 Rust

结论先行：**用 Go 写，且作为 tingly-box 生态的一个模块/伙伴工具**。理由：

1. **你已经有 80% 的地基，全是 Go**：
   - `agentboot/`：驱动 Claude Code CLI 子进程的完整实现 —— 进程管理、
     stream-JSON 协议解析、permission/ask 路由、session 列表与 resume。
     这正是 caseloop「worker 调用层」最难写的部分，direct reuse。
   - `afk/`：进程内 agent runtime（Anthropic Messages 原生、compaction、
     skill 加载、JSONL session log），如果某些 worker 想不经 CLI 直接打
     tingly-box gateway，用它。
   - tingly-box gateway 本身：模型路由、配额、usage tracking —— caseloop 的
     token 记账和 quota-aware 调度（loopx 的 `quota should-run`）可以直接
     建在它上面。
   - 用 Rust 意味着这三块全部从零重写，还要跨语言维护两套 Claude 协议解析。
2. **工作负载性质**：这是个 I/O 编排器 —— 等子进程、读写 SQLite、渲染
   markdown。瓶颈在 LLM 延迟，Rust 的性能优势在这里买不到任何东西。
3. Rust 真正的卖点（无 GC 尾延迟、极端内存控制、嵌入式）在此场景均不成立；
   单二进制分发 Go 同样给到。goroutine + channel 表达「两个 pool + 队列 +
   lease 超时」也比 async Rust 直白得多。
4. 诚实的反面：如果你把这个项目当学 Rust 的练手、且愿意放弃复用 agentboot，
   Rust 也能写 —— 但那是学习目标，不是工程选型。工程上 Go 无悬念。

## 6. loopx 这个 repo 本身能不能直接用？

**概念全要，代码不必**。loopx 是 Python 实现的通用 state kernel，直接引入
意味着：Python 运行时依赖、通用模型（goal/todo/gate）到你的领域模型
（case/attempt/verdict）之间的映射层、以及它靠 slash-command 约定接入 agent
—— 而「靠约定接入」恰恰是你要逃离的软约束模式。

从 loopx 搬走的思想：state kernel 与投影分离、claim + lease、evidence
writeback、review-packet（给人看的决策摘要）、quota-aware should-run。
从 autoresearch 搬走的思想：单文件式的人改方向盘（Goal 即 program.md）、
固定预算、单一指标、accept/reject 即历史。

## 7. CLI 面（草案）

```
caseloop init                     # 建 .caseloop/ (db + events + config)
caseloop goal edit                # 打开 GOAL.md (人写方向, 对应 program.md)
caseloop start [--gen 3 --run 4]  # 启动 loop; 前台 TUI 或 --daemon
caseloop status [--md]            # 渲染投影 (即新的 progress 文档)
caseloop case list|show <id>      # 查 case / attempt / verdict 链
caseloop review                   # 待人裁决的 gate 队列 (loopx review-packet)
caseloop accept|reject <id> -m "" # 人工覆盖 verdict
caseloop resume                   # 崩溃/中断后继续 (从 DB + lease 恢复, 无需任何"记忆")
caseloop export                   # accepted cases 导出为下游可用工件
```

Gate（human-in-the-loop）：默认全自动跑，但 Goal 里可声明规则，如
「reject 率连续 > 80% 时暂停 gen 并进 review 队列」「涉及 X 的 case 必须人裁」。

## 8. 落地路径

1. **M1 内核**（无 agent）：SQLite schema + 状态机 + events.jsonl + `status`
   渲染。手工 `caseloop case add` / `accept` 也能用 —— 内核先于智能。
2. **M2 run pool**：agentboot 驱动 headless claude 跑 case，结构化 verdict
   写回，lease/重试/超时。此时 gen 仍可由你在普通 Claude session 里做，
   只是产出用 CLI 入库。
3. **M3 gen pool + 回流**：gen packet 携带 recent_verdicts/dedup，闭环成形。
4. **M4 打磨**：adversarial verify、gate 规则、quota-aware 调度（接 gateway
   usage）、TUI。

风险与对策：
- 结构化输出偶发不合规 → schema 校验 + 带错误信息的一次性重试，仍失败记
  failed(retryable)。
- gen 重复产 case → case 指纹（spec 归一化后 hash）+ packet 里的 dedup_keys。
- 长调研型 gen 超单次调用容量 → 该 case 独占一个可 resume 的 session
  （agentboot 已支持 session resume），进度仍以「结构化中间产物入库」为准。
