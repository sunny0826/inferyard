# ADR 019：显式冻结临时内存停止覆盖

状态：Accepted

## 决策

engine-fit 创建计划可用 `--skip-memory-stop REASON`，与显式 `--min-free-memory-mib` 互斥，
冻结 plan.v4/run.v5。内存下限为 null，原因参与哈希；温度是否覆盖独立决定。
内存、RSS、温度和缺测仍保存，报告分别标明停止覆盖，旧定义保持原读取规则。

## 理由与代价

把下限设为 0 不能表达内存缺测也被覆盖，会掩盖诊断条件；运行时切换或编辑旧证据又会破坏冻结来源。
显式新定义增加兼容分派，但明确区分默认保护与操作者限定的诊断。
内存覆盖不改变设备物理容量、模型格式或 CUDA/Metal 要求；磁盘、期限、身份、取消和排空仍执行。

字段与拒绝规则见[常用引擎契约](../contracts/engine-fit-contract.md#常用引擎扩展)，温度覆盖见 [ADR 016](016-engine-fit-temperature-override.md)。
