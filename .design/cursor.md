# Cursor Scenario — the public-URL constraint

> 适用对象：tingly-box 前端贡献者。
> 覆盖范围：`cursor` scenario 页面（`UseCursorPage.tsx` / `CursorConfigModal.tsx`）
> 为什么默认隐藏，以及这个限制从哪来。

---

## 1. 结论

**Cursor 的 "Override OpenAI Base URL" 是从 Cursor 自己的云端服务器
（`api2.cursor.sh`）发起请求的，不是从本机的 Cursor 客户端发起。** 这与
Xcode（原生 macOS App，直接从本机发请求）和我们的 VS Code 扩展（扩展进程
本身在本机发请求）都不同——Cursor 的架构决定了：

- `http://localhost:<port>/tingly/cursor` 这类地址**必然不可达**：云端服务器
  连不到用户本机的 localhost，请求会挂起（"Problem reaching OpenAI"），本机
  也不会有任何 TCP 连接落地，日志排查无从下手。
- 私网 IP（`10.x`、`192.168.x`、`172.16-31.x`）同理不可达。
- Base URL **必须是公网可达的 HTTPS 地址**。本机/自建部署要接入 Cursor，
  需要额外经过隧道（Cloudflare Tunnel、ngrok 等）或者本身就是公网可达的
  部署（云主机、有域名的自建服务器）。

因此，"打开 tingly-box → 复制 localhost 地址 → 粘贴进 Cursor" 这条对 Xcode/
VS Code 成立的路径，对 Cursor **不成立**——这是 Cursor 侧的架构限制，
tingly-box 这边无法绕过。

---

## 2. 对产品设计的影响

- **`cursor` 加入 `DEFAULT_HIDDEN`**（`frontend/src/pages/scenario/scenarioRegistry.tsx`），
  与 `pi`（"集成细节待验证"）同等对待：默认不出现在侧边栏，但仍能在
  Agents 总览网格里看到（标 Hidden），用户可以主动打开。这是"未做" vs
  "做了但对典型本地部署默认不可用"之间的取舍——比起默认展示一个对多数
  用户（本机跑 tingly-box）会静默失败的功能，默认隐藏更符合 UX-First
  原则（不制造confusing failure mode）。
- **`CursorConfigModal.tsx` 顶部常驻一条 `Alert`**，明确说明"Base URL 从
  Cursor 云端发起，必须公网 HTTPS 可达"；当检测到 `baseUrl` 是
  `localhost`/`127.0.0.1`/私网 IP/非 HTTPS 时，Alert 升级为 `warning`
  severity 并给出隧道建议。检测逻辑是纯前端启发式（`looksUnreachableFromCursorCloud`），
  不代表后端做了任何校验或拦截——用户依然可以配置任意 Base URL，Alert
  只是提前把"为什么不通"讲清楚，避免用户去 tingly-box 侧排查一个其实是
  Cursor 架构决定的问题。
- **不做的事**：没有在后端拒绝 localhost 的 rule/provider 配置——`cursor`
  scenario 底层复用的仍是通用 OpenAI-compatible rule 机制，localhost 在其他
  场景（Xcode、VS Code、本地测试）完全合法，不能在通用层加限制。这个约束
  纯粹是 Cursor 这一个 client 的产品特性，只应该体现在 Cursor 专属的 UI 文案里。

---

## 3. 何时可以放开默认隐藏

- 能够可靠判断"当前 tingly-box 是否公网可达"（例如已配置的 base URL 不是
  localhost/私网），并在这种情况下才把 `cursor` 从 `DEFAULT_HIDDEN` 移除
  ——但这需要跨 scenario 的部署形态判断，目前没有这类基础设施。
  或者：一旦产品提供托管/公网可达的 tingly-box 部署形态，对那部分用户可以
  默认展示。
- 在此之前，`cursor` 保持 Pi 式的"默认隐藏、用户可自行开启"状态。

---

## 4. 参考

- Cursor 社区反馈（"Override OpenAI Base URL" 从云端发起、localhost 不可达、
  需要公网 HTTPS）：
  - https://forum.cursor.com/t/problem-reaching-openai-error-on-a-local-model-with-overriden-base-url/3975
  - https://forum.cursor.com/t/the-custom-override-of-the-openai-base-url-is-unusable/152675
  - https://forum.cursor.com/t/use-on-prem-model/149334
  - https://dev.to/orchidfiles/why-localhost-doesnt-work-as-openai-base-url-in-cursor-and-how-to-fix-it-589e
