# Image Edit(/images/edits)

> 适用对象:tingly-box 后端贡献者。
> 描述 image edit(改图)网关能力的设计:入站协议、vendor 分发、
> 以及 Codex(ChatGPT 订阅)原生 images 协议的适配。
> 关联文档:imagegen scenario 见 `../internal/vision/imagegen/imagegen.go`
> 包注释;Codex 请求链路见 `codex-auth.md` / `codex-config.md`。

---

## 1. 背景:为什么需要单独一条协议

image generation 早已就位(`/images/generations` + Responses API 两个并行
surface),但 **edit(基于已有图片改图)** 一直缺失。补齐它的难点不在
OpenAI 兼容侧——SDK 直接支持 multipart `/v1/images/edits`——而在 Codex
(ChatGPT OAuth 订阅)侧:ChatGPT backend **不支持**公开的 multipart
edits 协议。

通过阅读 openai/codex 源码确认(而非猜测),Codex CLI 自身的改图链路是:

```text
模型调用 image_gen.imagegen (namespaced tool)
    ↓ 客户端接管(不是让 /responses 自己执行)
POST {base}/codex/images/edits        ← 独立 endpoint
    JSON: {images:[{image_url:"data:image/png;base64,..."}],
           prompt, model:"gpt-image-2",
           background/quality/size:"auto", n?}
    ↓
{created, data:[{b64_json}], background?, quality?, size?, usage}
```

源码依据(openai/codex,codex-rs):

| 事实 | 出处 |
|------|------|
| `POST images/generations` / `images/edits`,JSON body | `codex-api/src/endpoint/images.rs` |
| `ImageEditRequest{images[].image_url, prompt, model, background?, n?, quality?, size?}` | `codex-api/src/images.rs` |
| data URL 形式的 reference image;响应 `data[].b64_json` | 同上 + endpoint 单测 |
| 默认 model `gpt-image-2`;背景/质量/尺寸默认 `auto`;最多 5 张 reference | `ext/image-generation/src/tool.rs` |
| 请求头 `x-codex-image-turn-id`(响应头 `x-codex-imagegen-request-id`) | `ext/image-generation/src/backend.rs` / `endpoint/images.rs` |
| base URL `https://chatgpt.com/backend-api/codex` | `model-provider-info/src/lib.rs` |
| quality 枚举只有 low/medium/high/auto(无 standard/hd) | `codex-api/src/images.rs` |

关键结论:**edit 走的是 JSON + data URL,不是公开 API 的 multipart**。
generation 我们继续沿用已验证的 Responses API(image_generation tool)
路径——Responses surface 无法给该 tool 挂 reference image,这正是 edit
必须走独立 endpoint 的原因。

---

## 2. 分层设计

与 generation 完全同构,每层只加一个对称成员:

```text
POST /tingly/{scenario}/v1/images/edits            ← routes.go(mixin group)
    ↓ HandleOpenAIImageEdit                        ← 入站解析 + rule/service 选择
    ↓ forwarding.ForwardOpenAIImageEdit            ← 薄转发
    ↓ OpenAIClientInterface.ImagesEdit             ← vendor 分发点
        ├─ OpenAIClient  → SDK multipart /v1/images/edits(OpenAI 兼容上游)
        ├─ CodexClient   → JSON POST images/edits(原生协议,见 §3)
        ├─ KimiClient    → ErrKimiNotSupported
        ├─ vmodel        → not supported
        └─ DashScope/MiniMax → 明确报错(适配器无 edit surface)
```

scenario/transport 不需要新注册:`TransportImageGen` 覆盖整个
`/images/*` 面,新路由挂在同一个 mixin group 上(canonical home 仍是
`imagegen` scenario)。tracing 声明 operation 为 `image_edit`。

### 2.1 入站双编码

`HandleOpenAIImageEdit` 接受两种 Content-Type:

| 编码 | 场景 | image 形态 |
|------|------|-----------|
| `multipart/form-data` | 官方 SDK / curl -F(标准 wire 格式) | 文件字段 `image`(可重复)或 `image[]`,可带 `mask` |
| `application/json` | 程序化调用;把上一次 generation 的 `b64_json` 直接链回来改图 | `image` 为单个或数组的 data URL / 裸 base64 字符串 |

JSON 侧**拒绝** http(s) 远程 URL——网关不代为抓取任意 URL(SSRF),只
解码 inline 内容。两种编码解析到同一个 `openai.ImageEditParams`,后续
链路无分支。

### 2.2 持久化

edit 结果与 generation 共用 `persistImages`(configDir/image/YYYYMMDD/,
best-effort),sidecar 元数据多一行 `Operation: edit` 以便区分。

---

## 3. Codex 适配细节(决策注解)

### 3.1 复用 SDK http 管道,而不是另起裸 http.Client

`CodexClient.ImagesEdit` 通过 `openai.Client.Post(ctx, "images/edits", ...)`
发送,body 是本包定义的 `codexImageEditRequest`(镜像 codex-rs
`ImageEditRequest`)。这样 OAuth bearer、Account-ID header、超时、logging、
session-bound transport 全部免费继承——代价是 `codexRoundTripper` 必须
认识 images 端点(§3.2)。

输入图(SDK union 里的 `io.Reader`)在这里被读出、`http.DetectContentType`
嗅探、编码成 `data:{mime};base64,...`,与 Codex CLI 的行为一致(它也是
读本地文件转 data URL)。

### 3.2 codexRoundTripper 的 images 特例

RoundTripper 原本按"一切都是 Responses SSE"设计:强制注入
`stream:true/store:false`、拒绝非 SSE 的 200 响应。images 端点是普通
JSON 请求/响应,二者都会把它打死。所以:

- path 重写新增一条:`/backend-api/images/*` → `/backend-api/codex/images/*`
  (SDK base 是 `https://chatgpt.com/backend-api`,相对路径 `images/edits`
  落在前者);
- `isCodexImagesPath` 命中时:跳过 body 过滤、跳过 `OpenAI-Beta:
  responses=experimental`、跳过 SSE 校验,JSON 响应原样透传;
- 非 200 错误处理保持共用。

### 3.3 参数归一

| OpenAI edit 参数 | Codex wire | 处理 |
|------|------|------|
| `quality: standard` | 枚举无此值 | 归一为 `medium`(与 generation 路径同规) |
| `background`/`size` 未设 | — | 填 `auto`(Codex CLI 的默认) |
| `n` | `n?` | 原样透传(wire schema 支持,虽然 CLI 自己不传) |
| `mask` / `response_format` / `output_format` / `output_compression` / `input_fidelity` | 无 | 丢弃 + debug log |
| 超过 5 张 reference | 后端硬限 | 只 log 不截断——让后端明确报错,不静默变更语义 |

`x-codex-image-turn-id` 每次请求生成新 uuid——网关没有 Codex 的 turn
概念,fresh id 是合理的替身。

### 3.4 为什么 generation 不一并切到原生 endpoint

现有 Responses-tool generation 路径已验证可用;原生 generations endpoint
是它的平行实现而非修复。一次只引入一条新协议,等 edit 路径在真实订阅上
跑稳,再评估是否统一。

---

## 4. 关键文件索引

| 功能 | 文件 |
|------|------|
| Codex 原生 edit 协议(types + ImagesEdit + data URL 转换) | `internal/client/codex_images.go` |
| RoundTripper images 特例 + path 重写 | `internal/client/codex_round_tripper.go` |
| 接口成员 + OpenAI 兼容实现 | `internal/client/openai.go` |
| Kimi / vmodel 的 not-supported 存根 | `internal/client/kimi_client.go`、`vmodel/client/openai.go` |
| 入站 handler(multipart + JSON 解析、校验) | `internal/protocolserver/openai_image_edit.go` |
| 持久化共用核心(`persistImages`) | `internal/protocolserver/openai_image.go` |
| 转发器 | `internal/protocolserver/forwarding/openai.go` |
| 路由注册 | `internal/protocolserver/routes.go` |
| 启动横幅 endpoint 打印 | `internal/server/server_lifecycle.go` |

---

## 5. 测试

| 层 | 用例 |
|----|------|
| Codex 请求构造 | 单图→data URL;多图+options;quality standard→medium;n 透传;无图报错(`codex_images_test.go`) |
| RoundTripper | images path 重写;JSON body 不被注入 stream/store;JSON 200 透传;非 200 报错;`isCodexImagesPath`(同上) |
| 入站解析 | multipart `image`/`image[]`/字段;JSON data URL/裸 base64/数组;拒绝远程 URL;必填校验(`openai_image_edit_test.go`) |
| decodeInlineImage | 声明 mime / 嗅探 mime / 非 base64 data URL / 非法 base64 |
| 持久化 | edit sidecar 带 `Operation: edit`;generation 原测试不回归 |

尚未覆盖(需真实 ChatGPT 订阅):对 `chatgpt.com/backend-api/codex/images/edits`
的端到端请求。上线前建议用 `codex_e2e_test.go` 的模式补一个 opt-in e2e。

---

## 6. 前端 / 后续

- 前端 imagegen playground 目前只有 generation;edit 的 UI(选图 + prompt)
  是后续工作,API 已就绪(`/tingly/imagegen/v1/images/edits`,JSON 编码对
  前端最友好)。
- 这些网关路由不在 swagger 管理范围内(swagger 只覆盖 `/api/v1` 管理面),
  无需 codegen。
