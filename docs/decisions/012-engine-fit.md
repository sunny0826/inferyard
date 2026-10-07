# ADR 012：同机同模型引擎适配诊断

状态：Accepted

## 决策

新增独立 `engine-fit` 命令组，冻结模型资产、短文本、顺序和预算，分别连接操作者启动的本机引擎。
记录执行终态、客户端耗时、token 与进程树资源，形成独立封存产物及两引擎比较。
不计算正式答案质量或性能赢家；核心 `run` 保留完整基准职责。

## 理由与代价

OpenAI API 兼容不能证明本机资产、有效参数或全服务空闲，适配必须核验进程、模型与引擎自己的观测来源。
仅凭 HTTP 完成清理 dirty 会遗漏后台释放，因此取消/排空保持完整状态。
不同模型格式、量化或转换产物不能因同名而视作同资产。
独立 definition 增加版本分派，但避免重解释旧核心证据。

平台与引擎扩展分别见 [ADR 013](013-engine-fit-macos.md)、[014](014-macos-common-engines.md)、
[020](020-windows-engine-fit-native.md)。停止覆盖见 [016](016-engine-fit-temperature-override.md)、
[019](019-engine-fit-memory-override.md)，离线读取见 [037](037-offline-evidence-reading.md)。
基本接口见[engine-fit 契约](../contracts/engine-fit-contract.md)。
