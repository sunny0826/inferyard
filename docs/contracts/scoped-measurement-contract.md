# 职责身份、持久化与校准契约

依据 [ADR036](../decisions/036-scoped-measurement-cost.md)，格式支持范围以 [ADR038](../decisions/038-inferyard-current-format.md) 为准，遵循[证据血缘总规则](../data-contract.md#证据血缘与比较结论)。

## 身份与分派

新 run/batch 保存 implementation_identity，definition=implementation-identity.v1；
measurement/scoring/presentation 各包含源码清单、摘要、依赖版本与缺测原因。
measurement 包含实际执行、传输、采集、资产/安全/存储以及所用共享契约与数据；
scoring 包含评分器、规则、题包解释及结构验证；presentation 包含离线呈现与模板。
重叠共享文件同时影响包含它的身份。清单显式保存，不靠目录名前缀作身份判断。
Python 实现/版本与 httpx/httpcore/anyio/h11/certifi/idna、jsonschema 及其验证依赖、
平台实际使用的 psutil 按职责记录；jinja2/MarkupSafe 属于呈现，pytest/ruff 仅开发。
缺依赖版本以 null+原因表达，不将未知与未知判相等，不阻断无关指标的描述读取。
每命令一个上下文计算一次并显式传给 journal、batch、rerun；原评分上下文计数保持。

新运行以 `implementation_identity` 为唯一执行身份门，不再要求 `tool_source_sha256`。
缺少此字段的旧 schema3 run/batch 仍用全包身份，不补当前摘要。
measurement/scoring 应匹配，呈现修改不阻断执行；保存身份不改值。
重跑在工具版本、scorer 或这条身份与父运行不同时仍可启动，比较记下差异且资格不成立。
当前比较 format4 / phase2.v3 按指标读取对应身份，并包含 `run.tool_version`。
报告仅接受 v8，隐式比较计算使用 phase2.v3；report v1–v7 和 comparison v1–v3 按 ADR 038 与 ADR 045 拒绝。
model-assets.v2 目录清单列相对路径与内容哈希；根路径只用于定位，缓存/README 不属于该资产集合。
当前 engine_fit_plan.v1 保留无 definition 清单及全目录身份算法；plan.v2–v4 的目录清单应声明
model-assets.v2，不再接受历史无 definition 的目录回退。

## 请求期检查与原始持久化

每次守卫核对 PID、启动时刻、可执行文件、启动参数、监听和已声明的 slots_debug。
模型绑定只核对启动参数路径的设备号与 inode。Linux 不为此读取 `maps`。
macOS 仍用一次 `lsof` 视图核对可执行文件、监听和 Metal。Windows 的 CUDA 已加载库核对保持。
不新增模板或运行库的逐次内容重读。

Windows lab 绑定阶段仍全量哈希。请求期以绑定的原生 stamp 核验引擎文件，模型路径
映射和原生 stamp 保留；psutil argv/环境/映射详情扫描在持有进程句柄前后活性检查之间。
API 未知、PID 复用、文件替换或加载路径不符继续拒绝；没有 Windows 真机资格声明。

新运行只删除 requests.jsonl 副本及其写入，不新增 flush/fsync。固定基线 4eb0d4d 的
TrialJournal.event 已对 request_started/request_finished/score/run_stopped 使用 sync=True，
这些原始边界保持。独立的 total_control_runtime.Observer.after 经 ExtensionJournal.event
写 request_finished，沿用其已有 sync=True；BaselineJournal 的 off 臂仍只缓存在内存，
不增加测量期间落盘。
完成事件先于评分，故取消、评分异常或评分写盘失败仍可恢复已持久化的终态；存储错误
立即停止，既有封存/dirty 语义不变。requests.jsonl 历史副本按 ADR038 拒绝；
如有可解释封存，先核验其哈希，坏哈希返回证据错误，不降格为 unsupported。
删除副本同步后，尚未同步的采样日志等待下一现有同步事件、到期的 flush_due 或 seal；
close 也保留原有同步。flush_due 在调用时判断 0.5s 间隔，并非独立定时写盘任务，
不承诺逐样本持久化或硬性的最大落盘延迟。
environment.jsonl 与 schedule.jsonl 保留；删除 sampler.environment 累积列表及仅初始化、
无 append/消费者的 sampler.schedule 字段。
调度只分析已有 late_ns/collector_work_ns，不以无证据的线程化声称改进。

## 性能资格与适用域

当前 comparison format4 / phase2.v3 使用 total-observer-applicability.v2：总开销来源包须通过既有完整核验，
live 且 hardware_qualified；明确绑定来源 manifest、控制臂与目标运行。增量 ABBA 可选，
存在时展示独立诊断状态，不要求与总开销容差相同；原逐指标专项资格仍保留。

适用域比较：同测量实现与运行依赖、采集器/间隔/传感器、模型/引擎内容与加载生成条件、
题包/题序/协议/输入输出预算、每题相同观测输出工作量、环境和同 boot 时间先后。
仅排除定位路径、PID/启动 ticks/端口等执行定位字段；来源执行身份本身仍由原校验器核验。
不证明跨 boot 或不同输出工作量的可复用性，分别保留明确原因；未知测量身份拒绝新资格。
总开销不足时普通描述仍展示当前校准状态，无资格性能差值保持 null。
已有量化 qualification 结果在 phase2.v3 传给性能比较，不重复谱系核验；不同权重的 tokenizer
相等需双方已核验 tokenizer_sha256 且模板相同，不能只看自报字段或猜测词表。


## 具体清单和使用边界

安装包 `data/implementation-files.json` 是显式职责清单。measurement 从 run/batch/trial 的
实际导入链确认，包含请求适配、采集、存储、结束时的 ledger/reducer、动态工厂所选平台模块、
共享结构/语义契约及 `data/metrics.json`；共享文件作为整体保守绑定。scoring 包含题包解释、
评分 v1/v2 与所用共享契约；presentation 包含报告/离线比较的读取、归约、模板及 CLI 展示。
CLI 参数结果已冻结为输入，帮助文本和 HTML 不属于测量源码。新增导入或包数据需审查清单；
运行时不扫描 AST。一次读取计算文件摘要并释放原始字节，总包与职责身份复用这些摘要。
清单成员自身进入相应职责摘要，不把其他职责的列表变化混入该职责。开发测试依赖不记录。

model-assets.v2 支持 safetensors/GGUF/PT/PTH/NPZ 权重、模型 BIN、权重 index JSON、
config/generation_config、tokenizer/vocab/merges/spiece、added_tokens.json 附加词表、聊天模板及模型 Python 实现；
排除 `.git/.cache/__pycache__/cache/logs` 与普通说明文件。所选文件的链接/目标 stamp 在全量
哈希前后对照；目录根只定位，同字节目录搬迁不改变内容摘要。全目录算法仅保留给当前
engine_fit_plan.v1 的模型清单；plan.v2–v4 的未声明版本目录清单拒绝。不存在跨 plan/run 阶段的免校验缓存。

离线 `compare --left-overhead DIR --right-overhead DIR` 两侧目录可只提供
`total-control-binding.json`，格式仍为：

```json
{"definition":"total_observer_binding.v1","path":"/absolute/sealed-control-packet","manifest_sha256":"<该包 manifest 的 SHA-256>"}
```

来源包应通过完整封存、live 来源、控制臂原始记录、独立 guardian 和 hardware_qualified
核验。phase2.v3 消费核验过程中返回的两个 on 臂，不按路径长期缓存，不重复读取目标 ledger。
可选 protocol.json/trials.json 仍独立核验；增量失败不代替总开销判断。首事件、引擎速率等
专项指标仍须其原有目标绑定、环境、同 boot 先后和专项估计通过，不由总开销单独放行。
已有 comparison1–3 文件由 ADR038 明确拒绝；当前资格规则保持。

新适用域仅移除明确定位字段（endpoint、输出目录、模型/模板/二进制/库清单路径、输入引用
路径），启动参数保留加载选项并规范化已绑定资产定位值。设备、模型/引擎摘要、运行库、
生成条件、采集器/传感器/间隔、冻结请求、环境均保留。同 boot 与观测输出一致是当前可实现
的窄边界；跨 boot、不同输出工作量及缺少新测量身份的控制臂不取得新复用资格。
环境缺测或任一来源身份不符仍拒绝；冻结显式 SafetyGuard 和 native 串行语义不变。

权重 index 的 `weight_map` 是实际资产声明：即使分片文件名未命中常规命名，也须纳入全量哈希。
引用应为目录内规范相对路径且不能落入排除目录；缺失、越界及非法 index 拒绝，不生成只绑定
index 而漏掉所引用权重的身份。普通 HF 文件 symlink 仍以最终常规文件字节核验。

报告 v8/comparison v4 的后续封存与定位结构见[离线读取契约](offline-reading-contract.md)，计算语义仍为 phase2.v3。
