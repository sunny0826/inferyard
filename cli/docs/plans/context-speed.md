# 长上下文速度专项测评

题包生成器与[人工审核材料](../../bundles/CONTEXT-SPEED-REVIEW.md)引用下节的已审配方。
运行步骤见[专项使用指南](../guides/context-speed.md)，字段、准入和公式见[专项契约](../contracts/context-speed-contract.md)。
Studio 的队列、取消、重读及历史归属见[专项运行路径](../../../docs/subsystems/studio-panel/context-speed.md)。
未完成的消费者检查、独立审核和真机档位见[专项 backlog](../backlog/context-speed.md#剩余工作)。

## 专项题库与运行协议

新增随包资源 `zh-context-speed`，`task_protocol=performance`；包含三个固定中文长文本
case，分别归属三个 context 档位。文本采用项目自有、许可明确的固定材料，避免大量
重复短句构成失真的唯一负载。原文、case ID、规则和版本入库并绑定内容哈希。
每个 case 的参考答案为空、质量不适用；固定输出目标为 256 token。

样本文本按参考 tokenizer 选择接近截图工作量的长度，准备记录写清参考模型与实测数。
实际运行始终使用被测模型的模板/tokenizer；不同模型得到不同 Prompt length 是合法结果。
预算检查满足 `实际输入 + 256 <= 配置 context` 才发送；不适配时保留阻断原因，
不能临场截断或换文本。这个方案无需扩大 `prepare-length` 的 65,536 字符搜索上限。

专项配方固定并版本化：并发/槽位 1、请求顺序固定、5 个完整 trial、每 trial 1 次短文本
预热、输出 `strict_fixed_length=256`、空 stop、关闭 prompt cache 与投机解码。
采样配方固定 `temperature=0`、`top_p=1`、`top_k=1`、`min_p=0`、
`presence_penalty=0`、`repeat_penalty=1`、`seed=0`、`reasoning_mode=off`；
`seed_support=supported` 仍须按现有协议核验，不能仅凭声明视为已支持。
复用现有 fixed-output probe、cache protocol 和引擎构建验证；probe/预热与正式样本分开。
原始缓存计数必须证明正式样本未复用前缀；无法证明时标为指标缺测，不能按冷缓存成绩汇总。
保留既有 `max_requests` 正式请求计数口径；总墙钟预算包含 probe/预热和排空开销，
UI 区分「5 次正式测量」和额外准备请求，不把额外请求藏在预算外。
超时、总时限、可用内存、磁盘、温度及取消条件在发送前冻结并在 UI 可见。

最初可执行范围为已有计时定义验证的 Prism 适配器；KVMem/NInfer 和未知构建在 UI
给出具体不支持原因，CLI 同样拒绝该专项协议。后续引擎单独适配，不给它们套 Prism 公式。
软件实现覆盖现有 macOS/Windows 候选准备和对应队列；CLI Linux 可使用人工准备的合法
绑定配置。真机覆盖逐平台报告，不把本机或模拟结果推广到所有显卡。

题库新增内容仍走既有人工内容审核。规划审核与 Agent 自审不伪造人工批准；实施阶段
先准备完整文本、hash 与审核材料，再按仓库已有规则完成正式题库准入。`--diagnostic`
只用于明确标记的诊断验证，不作为正式成绩验收。社区资源变更的 owner 同轮重生成
`resources.json` 并执行 `export_community.py --check`。
