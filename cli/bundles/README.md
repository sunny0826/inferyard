# 当前题包

活动题包均使用 `schema_version = 3`：

| 文件 | 内容 | 原审核 |
| --- | --- | --- |
| [zh-smoke.json](zh-smoke.json) | 20 道指令题、20 道提取题 | [审核说明](REVIEW.md) |
| [zh-core.json](zh-core.json) | 六类各 20 题，共 120 题 | [核心题包审核说明](PHASE2-REVIEW.md) |
| [zh-svg-pelican.json](zh-svg-pelican.json) | 1 道 SVG 生成题，英文原题；仅采集生成期指标和展示，不评分 | 2026-10-07 本会话人工审核通过，记录已写入题包 |

另有已审修订包 [zh-core-clarified.json](zh-core-clarified.json)：只澄清 20 道嵌套 JSON 题的根对象及顶层字段，答案、规则和其余 100 题不变；不替换默认已审题包。变更与审核内容哈希见[逐题修订材料](STRUCTURED-CLARITY-REVIEW.md)及[变更绑定](zh-core-clarified.review.json)，2026-10-02 的[人工批准](STRUCTURED-CLARITY-APPROVAL-20261002.md)已写入新题包。变更清单中的 pending 保留为生成时快照；旧审批仍只适用于原题包。

[zh-core-fields.json](zh-core-fields.json) 在上述已审包基础上，仅明确两题的错误消息边界和 false 类型，其余 118 题、全部答案及规则不变，内容版本 `2.0.2-draft.1`。见[两题修订材料](STRUCTURED-FIELDS-REVIEW.md)、[变更绑定](zh-core-fields.review.json)及[独立人工批准](STRUCTURED-FIELDS-APPROVAL-20261002.md)。这是看到失分后修订的新协议，不回算旧成绩或切换默认配置。

两份题包通过 `bundle.upgrade_legacy_bundle` 显式迁移。题目、参考答案、评分规则、答案政策及原审核记录保持不变；`review_provenance` 保留原 JSON 文本、原文件文本哈希和原内容哈希。`require_review` 会重新验证原批准与内容等价性，格式迁移不生成新的人工批准。

文件名映射为 `zh-smoke-v1.json → zh-smoke.json`、`zh-core-v2.json → zh-core.json`。题包内的 `bundle_id`、内容 `version` 和审核日期属于原内容身份，继续保留。它们独立于数据契约版本，配置中的 `bundle.version` 须与题包字段一致。

原审核 Markdown、[审核页面](zh-core-v2-review.html)和[边界样例](zh-core-v2.boundaries.json)保留原样；其中旧文件名用于说明被审核的来源。边界样例没有 wire revision，只验证算法边界，不能替代人工审核。

修改题目、答案、规则、政策或元数据后，旧等价证明失效；应移除不再适用的证明并重新人工审核。删除或更改原审核记录也会使继承证明失效。测试夹具位于 `tests/fixtures/`，不作为正式题包。
