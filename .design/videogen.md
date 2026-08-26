# Video Generation (videogen)

Status: implemented (backend); frontend scenario page pending.
Package: `internal/vision/videogen` · Scenario: `videogen` · Transport: `videogen`

## 1. 问题与结论

imagegen 落地后的自然追问:视频生成能不能/要不要接?结论是**能接,且必须换一种形态接**。

图像生成大多数厂商是同步的(即使 DashScope 异步,任务也在几十秒内完成,imagegen 在一次 HTTP 请求内 submit + poll 到底)。视频生成不同:**所有厂商都是分钟级异步任务**,把它包装成同步阻塞请求意味着一个 HTTP 连接挂 1–10 分钟——对网关、客户端、超时链路都是错误的形态。

因此 videogen 不复用 imagegen 的"阻塞到完成"模型,而是把 **job 生命周期本身作为网关面**,规范化到 OpenAI Videos API(Sora)的形状——它是目前唯一接近事实标准的视频 job 契约:

```
POST /tingly/{scenario}/v1/videos              提交 job(走路由规则选 provider)
GET  /tingly/{scenario}/v1/videos/{id}         轮询 job 状态
GET  /tingly/{scenario}/v1/videos/{id}/content 获取成片(302 到 CDN,或代理字节流)
```

状态词表:`queued | in_progress | completed | failed`(OpenAI 原词),各厂商状态映射见 §4。

## 2. 跨请求路由:provider 内嵌进 job id

job 型 API 给网关带来一个 imagegen 没有的问题:**提交与轮询是两次独立的 HTTP 请求**。提交时路由规则选中了某个 provider,轮询时如何回到同一个上游?

方案:**把 provider 编码进返回给调用方的 job id**,网关零状态。

```
gateway_id = "tbv_" + base64url(provider_uuid + "|" + native_task_id)
```

- 轮询/下载请求解出 `provider_uuid`,经 `Config.GetProviderByUUID` 找回 provider,直连上游,不再走路由管道;
- 无服务端 job store,id 跨网关重启有效;
- 非本网关签发的 id(如原生 `video_123`)被 `DecodeJobID` 拒绝,返回干净的 invalid_request 而不是转发垃圾。

被否掉的替代方案:服务端映射表(引入持久化与 GC,重启丢失);轮询时重新走路由(可能路由到不同 provider,native id 对不上)。

## 3. 分层(与 imagegen 完全同构)

```
protocolserver/openai_video.go   HTTP handler:scenario 校验、路由(仅 create)、id 编解码
protocolserver/forwarding        ForwardOpenAIVideoCreate:统一转发入口(span/complete)
client/openai.go                 VideoCreate/VideoGet/VideoDownload:vendor 分发
vision/videogen                  叶子包:规范化契约 + 各厂商 adapter(不 import internal/client)
```

- `client.VideoGenerator` 是独立窄接口,**不并入** `OpenAIClientInterface`:今天只有通用 OpenAI wrapper 能服务它,handler 用类型断言探测;Codex/Kimi/vmodel 等特化 wrapper 不必被迫实现,断言失败返回 501。
- vendor 检测基于 API base host(与 imagegen 相同),对用户自建的同 host provider 克隆同样生效。
- OpenAI 风格 provider 默认乐观假设支持 `/videos`(与 imagegen 对 `/images/generations` 的乐观假设一致):不支持的上游会返回它自己的 404,比网关猜测能力更诚实。

## 4. 厂商适配与实现依据(官方文档)

实现时以下列官方文档为准(截至 2026-08 的文档位置;实现环境无外网,链接/文档编号未能在线复核,若失效请以各官网导航为准)。**字段形状的权威锚点是 stub-server 单测**(`videogen_test.go` 的 `Test*Lifecycle`),上游若改契约,先改测试再改 adapter。

### 4.1 OpenAI(Sora)— SDK 直连,不在本包

- API Reference — Videos: https://platform.openai.com/docs/api-reference/videos
  - `POST /v1/videos`(create)、`GET /v1/videos/{video_id}`(retrieve)、`GET /v1/videos/{video_id}/content`(download content)
- Go SDK 类型:`libs/openai-go/video.go`(`VideoService`、`Video`、`VideoNewParams`、`VideoStatus`)——本包规范化类型即以此为蓝本。
- 下载路径返回字节流(`*http.Response`),网关代理透传;其余厂商返回 CDN URL,网关 302。

### 4.2 DashScope(阿里云百炼,通义万相 Wan 文生视频)— `dashscope.go`

- 视频生成 API(国际站):https://www.alibabacloud.com/help/en/model-studio/text-to-video-api-reference
- 视频生成(国内站):https://help.aliyun.com/zh/model-studio/video-generation-wan
- 异步任务通用模式(与 imagegen 的 DashScope adapter 同一机制):提交带 `X-DashScope-Async: enable`,轮询 `GET /api/v1/tasks/{task_id}`
  https://help.aliyun.com/zh/model-studio/developer-reference/get-async-task-result
- Endpoint:`POST {scheme}://{host}/api/v1/services/aigc/video-generation/video-synthesis`
- 状态映射:`PENDING→queued`,`RUNNING→in_progress`,`SUCCEEDED→completed`,`FAILED/CANCELED/UNKNOWN→failed`
- 字段:`Size "WxH"` → `parameters.size "W*H"`;`Seconds` → `parameters.duration`(int);`Extra["img_url"]`/`Extra["negative_prompt"]` 提升到 `input`,其余 Extra 透传 `parameters`;成片在 `output.video_url`,`usage.video_duration` 回填 Seconds。
- 北京(`dashscope.aliyuncs.com`)与新加坡(`dashscope-intl.aliyuncs.com`)同构,host 派生。

### 4.3 MiniMax(Hailuo / T2V-01)— `minimax.go`

- 视频生成 API:https://platform.minimax.io/docs/api-reference/video-generation
- 任务查询:`GET /v1/query/video_generation?task_id=…`(同上文档)
- 文件取回:https://platform.minimax.io/docs/api-reference/files-retrieve —— `GET /v1/files/retrieve?file_id=…` → `file.download_url`
- 三步生命周期:submit → query(status + file_id)→ retrieve(download_url);`Download` 内部串联后两步。
- 状态映射:`Queueing/Preparing→queued`,`Processing→in_progress`,`Success→completed`,`Fail→failed`
- 业务错误藏在 HTTP 200 的 `base_resp.status_code`(与 imagegen 的 MiniMax adapter 同一坑);`file_id` 文档为 string 但实测出现过裸数字,用 `json.Number` 兼容。
- 字段:`Seconds` → `duration`(int);`Size` 仅当某维恰为 512/768/1080 时映射 `resolution` 类("512P"/"768P"/"1080P"),否则交上游默认;`Extra["resolution"]`/`Extra["first_frame_image"]` 显式覆盖。

### 4.4 Volcengine Ark / BytePlus(字节跳动 Doubao **Seedance**)— `ark.go`

- 视频生成(火山方舟,文档中心「视频生成」/「内容生成任务 API」条目):https://www.volcengine.com/docs/82379/1330310
- BytePlus ModelArk(国际版,同构):https://docs.byteplus.com/en/docs/ModelArk/
- 关键事实:**Ark 的 chat 面是 OpenAI 兼容的(`/api/v3` 同 base),但视频面不是**——走专有的
  `POST {base}/contents/generations/tasks` / `GET {base}/contents/generations/tasks/{id}`。
  不单独适配的话会掉进 OpenAI-compat 乐观路径打 `/videos` 直接 404,所以 Seedance 必须有自己的 vendor。
- 状态映射:`queued→queued`,`running→in_progress`,`succeeded→completed`,`failed/cancelled→failed`;成片在 `content.video_url`。
- 生成参数按 Ark 惯例以文本命令附在 prompt 尾部:`Size "WxH"` → `--ratio W:H`(约分)+ 高度命中 480/720/1080 时 `--resolution 480p/720p/1080p`;`Seconds` → `--duration N`。用户 prompt 里已写的 `--flag` 不重复追加(显式优先)。
- 首帧图生视频:`Extra["image_url"]`(可选 `Extra["image_role"]`)作为 `content` 的 `image_url` part。
- host 检测:`volces.com`(火山)/ `bytepluses.com`(BytePlus)。

## 5. Scenario / Transport

- 新增 `ScenarioVideoGen = "videogen"`、`TransportVideoGen = "videogen"`;
- `videogen` scenario 只声明 `TransportVideoGen`:视频在所有厂商上都是 job 型,**不存在** imagegen 那种 "Responses API 平行面",所以不像 imagegen 那样混入 `TransportOpenAI`;
- `openai` scenario 的 mixin 列表追加 `TransportVideoGen`(与 imagegen 先例一致,`/tingly/openai/v1/videos` 可用)。

## 6. 计费/用量

job 提交本身不消耗 token,用量由上游在完成时结算(OpenAI 按秒计费,DashScope 报 `video_duration`,Ark 报 `usage.completion_tokens`)。当前 create 只按请求计数记账(zero-token usage),把完成时用量回填到 usage 记录是后续工作(需要在 GET 轮询到 completed 时补记,注意去重)。

## 7. 已知边界与后续

- **multipart `input_reference` 未支持**:OpenAI 原生面接受 multipart 上传参考图;当前入站只收 JSON。厂商侧参考图走 Extra 字段(`img_url` / `first_frame_image` / `image_url`)。
- **前端**:`videogen` scenario 尚无前端页面(scenarioRegistry / 导航 / playground),需要仿照 imagegen 的 `UseImageGenPage` 补齐,并跑 `task codegen` 生成 client SDK。
- **remix / list / delete** 等 OpenAI Videos 扩展端点未实现,按需追加。
- **完成时用量回填**(见 §6)。
- 成片落盘(仿照 `persistImageGeneration`)未做:视频体积大且多为 CDN URL,是否落盘待产品判断。

## 8. 验证

- `internal/vision/videogen/videogen_test.go`:vendor 检测、job id 编解码往返与拒收、OpenAI 形状往返(含 `object: "video"` wire 断言)、三家厂商 stub-server 全生命周期(submit → poll → download)、MiniMax base_resp 业务错误、Ark 文本命令渲染与显式 flag 优先。
- `go build ./...` 与 `internal/{typ,client,routing,protocolserver,server}` 全量测试通过。
