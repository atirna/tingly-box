# 模型数据分层

模型相关的静态数据有两条正交的轴,分别有各自的文件与维护主体,不要互相塞:

| 层 | 位置 | 回答的问题 | 维护方式 |
|----|------|-----------|---------|
| **能力目录(catalog)** | `internal/protocol/catalog/<vendor>.models.json` + `<vendor>.go` | 这个模型本身能做什么。能力是**模型族属性**,与哪个 provider 提供无关,每个模型只声明一次。当前只覆盖 thinking/reasoning(其余能力用不上就不建模)。 | 人工策展,新模型发布时更新 JSON,不改代码。 |
| **供给注册表(providers)** | `internal/data/providers.json` | 谁在哪个端点、以什么限额提供哪些模型(base_url、context、max_output、deprecated)。这些**可能**因 provider 而异,所以挂在 provider 条目下。 | 人工策展。 |

运行时从 provider API 拉取的模型列表(`ModelListManager` / DB)是第三层缓存,不在本文件讨论范围。

## Catalog 使用规则

- **只建模消费方实际用到的字段,不镜像 vendor API 的全量 capabilities 响应。**
  早期版本整体复制了 Anthropic `/v1/models` 的 capabilities 结构(batch/citations/
  code_execution/context_management/image_input/pdf_input/structured_outputs…),
  但代码只读 thinking 和 effort 两块 —— 其余是没人消费的死重量,已删除。加字段前
  先确认有真实消费方。
- Schema 参考 OpenRouter `reasoning: {supported_efforts: [...], default_effort, ...}`
  的扁平写法,而非 Anthropic `capabilities.effort.<level>.supported` 的嵌套布尔树:
  ```json
  { "id": "claude-opus-4-7", "thinking": { "budget": false, "adaptive": true, "efforts": ["low","medium","high","max"] } }
  ```
  `budget` = 是否接受 `thinking.type=enabled` + `budget_tokens`;`adaptive` = 是否接受
  `thinking.type=adaptive`;`efforts` = `output_config.effort` 支持的档位列表,省略即无
  effort 支持。
- 每个 vendor 一对文件:`claude.models.json`(数据)+ `claude.go`(加载与查询,如
  `catalog.LookupClaudeThinkingCaps`)。openai / gemini 需要能力判定时按同样模式扩展,
  字段集合由各自实际消费方决定,不必与 claude 的 schema 一致。
- 查询按"完整 id + 去日期 family 名"双索引、最长 key 优先做子串匹配,所以裸名
  (`claude-opus-4-5`)、带日期 id、云厂商修饰名(`us.anthropic.…-v1:0`、`…@20251001`)都能解析。
- **完备性不变式**:`catalog/completeness_test.go` 断言 providers.json 中出现的每个 Claude
  模型 id 都能在 catalog 中解析。加新模型先加 catalog,再加 providers.json,否则测试失败。
- 不在 catalog 中的模型(第三方代理模型、比快照新的发布)由消费方给保守兜底
  (thinking 场景:budget-only、剥离 effort,见 `ops.anthropicModelThinkingCaps`)。

## 消费方

- `internal/protocol/ops/request_anthropic_model.go`:按模型能力对 thinking 三方言
  (adaptive / enabled+budget / output_config.effort)做 vendor 阶段互转与钳制。
- effort↔budget 的统一阶梯在 `internal/protocol/thinking`(见 `.design/rule-flags.md`
  的 `thinking_effort` 行),catalog 只负责"模型支持哪些方言/档位"。

## 注意

- 最新一代条目(opus-4-8 / sonnet-5 / opus-5 / fable-5)的能力按 opus-4-7 的
  adaptive-only + effort profile 推断填写(providers.json 仅提供限额),官方 capabilities
  披露后应回填核对。
