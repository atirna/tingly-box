# 模型数据分层

模型相关的静态数据有两条正交的轴,分别有各自的文件与维护主体,不要互相塞:

| 层 | 位置 | 回答的问题 | 维护方式 |
|----|------|-----------|---------|
| **能力目录(catalog)** | `internal/data/catalog/<vendor>.models.json` + `<vendor>.go` | 这个模型本身能做什么(thinking 方言、effort 档位、structured outputs…)。能力是**模型族属性**,与哪个 provider 提供无关,每个模型只声明一次。 | 镜像 vendor 的 models API 响应形状(Claude 文件即 `/v1/models` 形状),新模型发布时更新 JSON,不改代码。 |
| **供给注册表(providers)** | `internal/data/providers.json` | 谁在哪个端点、以什么限额提供哪些模型(base_url、context、max_output、deprecated)。这些**可能**因 provider 而异,所以挂在 provider 条目下。 | 人工策展。 |

运行时从 provider API 拉取的模型列表(`ModelListManager` / DB)是第三层缓存,不在本文件讨论范围;长期方向是能力数据也从 API 刷新、embedded catalog 退化为离线 seed。

## Catalog 使用规则

- 每个 vendor 一对文件:`claude.models.json`(数据)+ `claude.go`(加载与查询,如
  `catalog.LookupClaudeThinkingCaps`)。openai / gemini 需要能力判定时按同样模式扩展。
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
