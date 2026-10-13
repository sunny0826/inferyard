# 运行原件容器契约

决策见 [ADR 048](../decisions/048-run-evidence-containers.md)。本契约定义当前运行原件；
代码与验收完成状态以 [backlog](../backlog.md#运行证据文件合并) 为准。

## 权威与结构

新 run 的 inputs 与 manifest 必须一致声明 `evidence_layout_version=2`，内层核心为 3。
旧 layout 1 只能从现有合法 v3 快照结构识别，无新写入、自动转换或补写。
新旧权威源混合、声明冲突、记录重复/越序、坏成员哈希或缺已引用记录均严格失败。
独立 frozen-plan、engine-fit、extension 封包及 baseline 继续原协议。

| 文件 | 结构与归属 |
| --- | --- |
| inputs.json | 一次原子发布冻结成员，之后不可变 |
| observations.jsonl | 同步追加封闭观测 kind/stage，每条独立内容绑定 |
| request-bodies.jsonl | 发送前完整脱敏 prepared body，按 request 身份唯一 |
| events.jsonl | 当前事件、回答、评分、终态 |
| memory.jsonl | 当前内存/资源样本，不新增测量 |
| environment.jsonl | 当前周期环境流 |
| schedule.jsonl | 当前调度流 |
| manifest.json | 七基础文件及实际附加流的字节哈希/大小与当前身份；不哈希自己 |

inputs envelope 字段冻结为 `schema_version`、`evidence_layout_version`、`run_id`、`documents`、
`configuration_source`、`configuration_source_missing_reason`。documents 固定 run、plan、selection、
config、bundle 五个键，每个成员为 `{payload, sha256}`，sha256 按既有 json_bytes 计算。
配置原文为第六逻辑成员：configuration_source 含 `media_type`、`encoding=base64`、`content`、
`sha256`、`bytes`，对既有读取/脱敏后持久化的原字节绑定。没有原文时为 null 并写明确原因，
不得由 parsed config 重建；旧 batch 无原文的事实保留。新实际 writer 可从 LoadedConfig 传入原文。
selection 的 config/bundle hash 仍绑定成员内容，不改为容器字节摘要。

observations envelope 固定 `schema_version`、`evidence_layout_version`、`run_id`、`trial_id`、`seq`、
`kind`、`stage`、`key`、`clock_id`、`monotonic_ns`、`utc`、`payload`、`payload_sha256`。
同文件 seq 连续递增；记录时间不冒充 payload 的原传感器读取时间。
每种 kind 只接受下表阶段；未列 kind 禁止万能文件名路由。

| kind | stage / key 与出现规则 |
| --- | --- |
| parent / service_binding / service_reuse / safety_policy | prepare，单例，原功能写入时保留 |
| environment | start/end，各一个槽；失败缺失保留原因 |
| service_properties | pre_idle(kvmem/ninfer adapter) / post_idle(Prism adapter)，权威单例；不按发现后的 observation mode 改发布时点 |
| engine_capabilities | pre_idle，native 实际能力单例 |
| recovery | post_idle，单例，不补造接手依据 |
| token_budgets / input_target_check | prepare，保持 v2 phase/case 清单及原核验 |
| collector / observer / boundary_observer | prepare，实际采集器/观察器存在时单例 |
| effective_parameters | ordinary/stream，各一个槽 |
| identity | post_probe/finalizing，两路径合计一个权威单例，禁止最后写入覆盖 |
| native_clean_receipt | client_complete，key=request_id，各请求最多一次且同步耐久；layout2 native 已完成请求必有，layout1 保留 pre-receipt v3 的原支持边界 |
| safety_check / execution_budget | finalizing，原功能写入时保留 |

prepared envelope 固定 `schema_version`、`evidence_layout_version`、`run_id`、`trial_id`、`request_id`、
`ordinal`、`phase`、`body`、`body_sha256`。phase 为 probe/warmup/formal；ordinal 从当前请求生成器
取得，按生成顺序追加且严格递增，同 run 请求身份与序号唯一。body 完整保留已有脱敏和 messages_sha256 语义。
prepared 没有 started 合法，不能计 executed；started 有消息摘要就必须绑定耐久原件。

## 共享读取接口

物理读写只在 evidence 边界；业务使用 `TrialEvidence(root, projection=False)`。
实现可拆私有模块，以下跨 owner 接口更改先由 root 更新契约再通知所有调用方。

| 接口 | 返回与职责 |
| --- | --- |
| input(kind) | run/plan/selection/config/bundle 的已解析 dict |
| input_digest(kind) / input_reference(kind) | 成员内容 hash / 真实来源引用 |
| configuration_source() | `{media_type, data: bytes, missing_reason}`；缺原文时 data/media_type 为 None；不创建虚拟 Path |
| observation(kind, stage=None, key=None) | 实际保存 payload 或 None；不取全局最后一条 |
| has_observation(kind, stage=None, key=None) | 已保存且满足身份/阶段的真实成员存在性 |
| observation_keys(kind, stage=None) | 实际匹配槽的 key 清单；用于 receipt 库存核验，旧布局也只枚举真实原件，不由事件补造 |
| observation_reference(kind, stage=None, key=None) | 对实际成员的物理容器及内容绑定 |
| request_body(event) / request_reference(event) | 按原 event 身份定位唯一 prepared body / 引用 |
| log_reference(name) | 对真实独立日志的物理引用，不接受旧快照虚拟名 |
| jsonl(name) / checked_jsonl(name) / observed() | 复用物理日志读取与同次字节摘要 |
| manifest / layout_version / root / errors / hashes / sizes | 当前物理库存、读取状态与真实物理摘要 |
| container_tails | 已扫描 observations/prepared 容器的尾截断原因，按实际文件名分组；全量 preload 后 ledger 纳入 partial，不缓存 body 或把截断当成功 |
| check_input_errors() / preload(manifest_files) | 保留严格错误处理与必要观测预读 |
| verify_reference(root, ref, reads=None, readers=None) | 核验真实容器摘要及可选成员定位/内容摘要；reads 属于同命令相同 resolved root；readers 是同命令按实际子目录保存的完整 reader 字典，原路径先过结构、包含性与 symlink 校验；不接受 projection 或跨命令缓存 |

只读 layout1 adapter 内部允许旧文件名映射；业务模块不得继续用 .json("run.json") 或虚拟 hash key。
既有 TrialReads 可作为私有物理读取实现，hashes/sizes 永远只含真实文件。
完整读取校验所有物理原件；projection 保留现行 inventory/size/事件摘要范围，不升级为完整资格。
`read_trial(..., metadata=...)` 的 metadata.evidence 只供同次命令复用领域读取，不进入 JSON
产物或持久状态；来源摘要与成员引用继续绑定该次已核验字节。

request-bodies 单次扫描全部完整行并严格校验，缓存身份到 byte range 与该行摘要，不常驻全文。
observations 同次扫描建立 kind/key/stage 索引；单次领域查询不再遍历全部收据。
取 body 重读区间后核对已核验行摘要，变化立即拒绝。无磁盘索引、隐式请求上限或跨命令缓存。
合法未封存尾半行按既有 partial/3；中间坏行、封存截尾及缺 started 原件为 4。
inputs 已发布而日志尚未创建的未封存前缀可读取为 partial/3：没有事件时保留
not_executed 分母，缺失日志不产生物理摘要或来源引用。已封存文件缺失、
request_started 对应的 prepared 原件缺失仍拒绝为 4。

## 来源引用与外部绑定

新增独立 `EVIDENCE_REFERENCE`，不扩大全部 frozen-plan 的 REFERENCE 含义。
物理引用仍为 `{path, sha256}`；容器成员增加 `member`，其中 input 定位为
`{kind:input, name, sha256}`，observation 为 `{kind:observation, seq, sha256}`，request 为
`{kind:request, request_id, sha256}`。member sha256 为成员内容，外层 sha256 为物理文件字节。
metric_observations、report/export/rescore/public、来源搬移均消费同一核验接口；不虚构旧文件。

独立 Go observer 不属于本轮迁移；其旧布局绑定与独立流继续遵循
[observer 契约](observer-contract.md)，不支持 layout2 run 绑定。

## 写入生命周期

TrialJournal 在创建采样日志前原子发布 inputs；通用 EvidenceStore snapshot/日志/seal 不改协议。
构造器接收 `configuration_source={media_type, data, missing_reason}`；data 为真实原文 bytes，
默认无原文时记录 not_persisted。脱敏后才计算原文的物理大小与摘要，不重建 parsed config。
低频观测同步方法 `observe(kind, payload, *, stage, key=None)` 由实际 owner 调用；
prepared 方法 `prepare_request(request_id, phase, ordinal, body)` 返回后原件已 flush/fsync。
`has_observation(kind, *, stage=None, key=None)` 只查询已成功耐久的槽；失败 append 不占槽。
共同写者仍在现有 event loop，同步 append 无 await；不引入 I/O worker 或多写者线程。
identity 权威槽显式查询，原发布时点不变。native receipt 写入耐久后才清 dirty。
append/flush/fsync 失败停发并保留 dirty；close/seal 纳入两个新增日志且保持目录 fsync。
写入失败的 writer 禁止再次 append、重试或 seal；关闭只释放资源，不把失败改成成功。
失败前已耐久的前缀保留为离线 partial 证据；未封存尾行的读取支持不构成同一 writer 的恢复授权。
BaselineJournal 实现对应领域方法但继续自身 buffer 协议；ExtensionJournal 不增加 run 容器。
独立 baseline 的低频旁证由 `BaselineEvidence` 读取其现有物理协议，不使用放宽了识别规则的
TrialEvidence。run 目录仍须严格识别版本及冻结输入；缺 run 的任意旁证目录不成为合法 run。

## 验证边界

先迁所有 reader 再切共享 writer；软件、模拟 CLI、离线浏览器、性能、原生能力分别登记。
11/19 清点只适用完整普通初始 Prism 与两个默认 scope；高频扩展、父链及 batch 外层另计。
保持 single 的默认内存采集器；不能以新增 collector 声明或补测来凑八文件。
