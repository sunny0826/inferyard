# 证据、封存与离线读取

证据子系统负责把一次 trial 的全部输入、观测、请求原件、事件与样本以只追加方式落盘，
封存为可离线核验的目录，并在离线侧严格重建账本。写入侧的触发与停止（主机锁、dirty、
取消、排空）属于[运行时执行与引擎适配](runtime-execution.md)；评分、报告与导出等
派生物属于[评分、比较与报告](scoring-reporting.md)。本页只覆盖
`cli/src/inferyard/evidence/` 内的写入、封存、读取与格式边界行为。

## 职责与事实源

| 部分 | 责任 | 事实源 |
| --- | --- | --- |
| 通用追加存储 | 目录创建（`0o700`）、追加日志、周期性 flush、原子快照、封存 manifest、凭据脱敏 | `EvidenceStore` 在 [`cli/src/inferyard/evidence/storage.py`](../../cli/src/inferyard/evidence/storage.py)；`Redactor` / `StreamRedactor`（流式分块脱敏）同文件 |
| 原子发布与路径校验 | 临时文件 + flush/fsync + 原子改名发布；拒绝绝对路径、`..` 与越界符号链接 | `atomic_bytes` 与 `local_file` 在 [`cli/src/inferyard/evidence/storage.py`](../../cli/src/inferyard/evidence/storage.py) |
| run 写入器（layout 2） | 构造时原子发布 `inputs.json` 并创建六份日志；观测槽、prepared 请求、`event`/`sample`/`seal` | `TrialJournal` 在 [`cli/src/inferyard/evidence/journal.py`](../../cli/src/inferyard/evidence/journal.py)；观测与 prepared 行构造在 `RunRecords.observe` / `RunRecords.prepare_request`，见 [`cli/src/inferyard/evidence/run_records.py`](../../cli/src/inferyard/evidence/run_records.py) |
| inputs envelope | 冻结五份文档 + 配置原文（base64 + 摘要），成员 sha256 按 `json_bytes` 计算 | `frozen_inputs` 在 [`cli/src/inferyard/evidence/run_inputs.py`](../../cli/src/inferyard/evidence/run_inputs.py) |
| 布局识别 | 依据 `inputs.json` 与 manifest 一致声明识别 layout 2；旧快照 layout 1 只读映射 | `identify`、`INPUT_FILES`、`OBSERVATION_FILES` 在 [`cli/src/inferyard/evidence/layout.py`](../../cli/src/inferyard/evidence/layout.py) |
| 领域读取接口 | 业务唯一的读取入口：input/observation/prepared 查询、来源引用、物理库存 | `TrialEvidence` 在 [`cli/src/inferyard/evidence/trial_evidence.py`](../../cli/src/inferyard/evidence/trial_evidence.py)；物理读取与哈希缓存在 `TrialReads`，见 [`cli/src/inferyard/evidence/trial_reads.py`](../../cli/src/inferyard/evidence/trial_reads.py) |
| 容器索引 | `observations.jsonl` 单趟扫描建立 kind/stage/key 索引；`request-bodies.jsonl` 缓存身份到字节区间，按区间重读并核对行摘要 | `Observations` 在 [`cli/src/inferyard/evidence/observations.py`](../../cli/src/inferyard/evidence/observations.py)；`PreparedRequests` 在 [`cli/src/inferyard/evidence/prepared.py`](../../cli/src/inferyard/evidence/prepared.py) |
| 来源引用核验 | 核验物理字节摘要与容器成员绑定，拒绝 projection/跨命令缓存 | `verify_reference` 在 [`cli/src/inferyard/evidence/reference.py`](../../cli/src/inferyard/evidence/reference.py)；`EVIDENCE_REFERENCE` 结构在 [evidence 契约](../../cli/docs/contracts/run-evidence-layout.md#来源引用与外部绑定) |
| 账本离线重建 | 严格读取 run 目录并归约为 summary/requests/计数，不发模型请求 | `read_trial` 在 [`cli/src/inferyard/evidence/ledger.py`](../../cli/src/inferyard/evidence/ledger.py)；事件归约在 `reduce_events`，见 [`cli/src/inferyard/evidence/event_ledger.py`](../../cli/src/inferyard/evidence/event_ledger.py)；计数与完整度在 `trial_summary`，见 [`cli/src/inferyard/evidence/trial_summary.py`](../../cli/src/inferyard/evidence/trial_summary.py) |
| 封存 | 全量 flush/fsync 后写 manifest（身份、来源、文件哈希，不含自身）；派生物字节级另封存 | `EvidenceStore.seal` / `verify_manifest` / `verify_manifest_files` 在 [`cli/src/inferyard/evidence/storage.py`](../../cli/src/inferyard/evidence/storage.py)；报告/比较封存在 `artifact_seal.seal` / `read_sealed`，见 [`cli/src/inferyard/evidence/artifact_seal.py`](../../cli/src/inferyard/evidence/artifact_seal.py) |
| 格式边界 | 当前格式核验：`schema_version=3`、`origin=measured`，拒绝旧格式与历史来源 | `require_core` / `UnsupportedFormat` 在 [`cli/src/inferyard/evidence/formats.py`](../../cli/src/inferyard/evidence/formats.py)；历史来源拒绝在 `require_current_sources`，见 [`cli/src/inferyard/evidence/request_snapshots.py`](../../cli/src/inferyard/evidence/request_snapshots.py) |
| 原生 clean receipt | layout 2 已完成请求必有 receipt；库存与 dirty_token 绑定核验 | `verify_clean_receipts` 在 [`cli/src/inferyard/evidence/native_receipts.py`](../../cli/src/inferyard/evidence/native_receipts.py)；receipt 写入与清 dirty 的次序在 [`cli/src/inferyard/runtime/service_observation.py`](../../cli/src/inferyard/runtime/service_observation.py)（属运行时） |
| 旁证读取 | 独立 baseline 旁证按自身物理协议读取，不借用放宽识别规则的 `TrialEvidence` | `BaselineEvidence` 在 [`cli/src/inferyard/evidence/baseline_reads.py`](../../cli/src/inferyard/evidence/baseline_reads.py) |
| 错误与投影读取 | 证据错误原因归一呈现；batch 延续字段与 resume 选择集的历史投影读取 | `safe_reason` 在 [`cli/src/inferyard/evidence/error_reasons.py`](../../cli/src/inferyard/evidence/error_reasons.py)；`read_run_projection` 在 [`cli/src/inferyard/evidence/run_projection.py`](../../cli/src/inferyard/evidence/run_projection.py)；`resume_selection` / `plan_progress` 在 [`cli/src/inferyard/evidence/ledger.py`](../../cli/src/inferyard/evidence/ledger.py) |

关键状态的事实源：`EvidenceStore._initialize` 生成 `run_id` 与 `clock_id`，并初始化
`sealed=False` 与 `_write_error=None`；`_seq` 由 `TrialJournal.event` / `sample` 递增，
保证 `events.jsonl`、`memory.jsonl` 的 `seq` 从 1 连续。`TrialJournal` 的冻结选择
（`selected`）与 `scorer_sha256` 在构造期由 selection 文档写入并校验，
`duration_protocol`、`strict_output`、`cache_protocol` 来自 workload 定义。
布局与文件语义的权威定义在[运行原件容器契约](../../cli/docs/contracts/run-evidence-layout.md)，
本页不复制。构造期任何校验或发布失败都会关闭已打开的流并抛错，不留下可继续写入的
半成品 writer。

## 运行路径

### 写入（layout 2 run）

1. `TrialJournal.__init__` 校验 plan/config/bundle 与 trial 选择（resume/repeat/rerun
   绑定父证据），然后调用 `frozen_inputs` 构造 inputs envelope；`atomic_bytes` 先把
   `inputs.json` 原子发布，再 `_open_logs` 创建 `observations.jsonl`、
   `request-bodies.jsonl`、`events.jsonl`、`memory.jsonl`、`environment.jsonl`、
   `schedule.jsonl` 六份日志。发布顺序保证任何时刻目录要么没有 run、要么有冻结输入。
2. 低频观测由各领域 owner 调 `RunRecords.observe`：行带 `seq`/`kind`/`stage`/`key`/
   `payload_sha256`，经契约校验与槽去重后 `_publish_observation` 同步 flush/fsync 追加。
3. 每个请求发送前，运行时调 `RunRecords.prepare_request` 写入完整脱敏 body（ordinal 严格
   递增、request_id 唯一），同步耐久后才开始发送并置 dirty；`request_started` 事件由
   `_check_prepared_start` 核对 prepared 摘要，未绑定原件的 started 直接失败。发送与
   dirty 的生命周期见[运行时执行与引擎适配](runtime-execution.md)。
4. 事件经 `TrialJournal.event` 追加（`request_started`/`request_finished`/`score`/
   `run_stopped` 同步 flush/fsync），样本经 `TrialJournal.sample` 追加 `memory.jsonl`；
   高频环境/调度流经 `observation` 追加 `environment.jsonl`/`schedule.jsonl`，
   `TrialJournal.observation` 另把 `external-cpu.jsonl`、`request-environment.jsonl`、
   `safety-checks.jsonl` 作为独立附加流打开；非同步追加由 `flush_due` 每 0.5 秒聚合
   flush/fsync。
5. native adapter 下，客户端完成证据经 `clean_observed` 先写
   `native_clean_receipt` 观测（同步耐久）再清 dirty；receipt 只证明本进程 HTTP 往返，
   不证明引擎级排空。
6. 收尾 `TrialJournal.seal` 全量 flush/fsync 后写 `manifest.json`（附加
   `evidence_layout_version=2`、`origin=measured` 等身份），此后写入被拒
   （`sealed_run`）。

### 读取与账本重建（离线）

1. `TrialEvidence.__init__` 先读 `manifest.json`，再 `identify` 布局：layout 2 要求
   `inputs.json` 与 manifest 声明一致且无旧文件名混入；layout 1 走 `TrialEvidence`
   私有只读 adapter；随后按布局校验 inputs/成员并核对封存摘要。
2. `read_trial` 预读输入与事件/样本日志，`verify_manifest` 核验物理原件，再依次做
   消息摘要绑定（`verify_message_snapshots`）、能力绑定、固定输出/cache/lab/native
   核验、容量停止校验，最后 `reduce_events` + `trial_summary` 产出 summary 与五终态
   计数。读到的记录与保存来源绑定，归约语义见[数据契约](../../cli/docs/data-contract.md)。
3. 报告、比较、rescore 等消费方通过 `input_reference`/`observation_reference`/
   `request_reference` 取得 `{path, sha256[, member]}` 引用，写新目录，原件不覆盖；
   引用核验统一走 `verify_reference`。事件只存 `messages_sha256`，离线侧由
   `verify_message_snapshots` 绑定 prepared 原件核对摘要，从不重建 prompt。
4. batch history 走 `read_run_projection` 读取延续字段（不做完整测量核验），
   再由 `plan_progress` 归约为历史投影；resume 的选择集由 `resume_selection`
   取父账本中全部且仅 `not_executed` 的 case，不隐式重试已终态请求。
5. `inferyard verify --path` 复用上述严格读取器，退出语义（0/verified、3/partial、
   4 损坏、2 未知布局）见[离线读取契约](../../cli/docs/contracts/offline-reading-contract.md)。

## 约束与失败行为

- 只追加、不可变：既有 run 目录不重开、不改写；`EvidenceStore._initialize` 以 `0o700`
  新建目录，`local_file` 拒绝绝对路径、Windows 盘符、`..` 与解析后越界的符号链接。
- 写入失败是粘性的：append/flush/fsync 失败把 `EvidenceError` 记入 `_write_error`，
  后续 `_ensure_writable` 一律抛同一错误；运行时把 `EvidenceError`/`OSError` 映射为
  退出 4 并保留 dirty，不继续产生无证据结果，也不把失败改成成功（close 只释放资源）。
  失败前已耐久的前缀保留为离线 partial 证据。
- 封存语义：`manifest.json` 记录身份、来源与每个文件的 sha256/字节数，不哈希自身；
  目录 fsync 随快照与 append 一起执行。`verify_manifest_files` 对原件哈希不符直接失败，
  对 `summary.json`/`report.html` 等派生物只报告 limitation（分类由代码决定，不信任
  被篡改的 `derived` 标记）。已封存 run 中日志尾截断为错误；未封存前缀可读并报告
  `truncated_tail_at_byte`。
- 当前格式边界：按 [ADR 038](../../cli/docs/decisions/038-inferyard-current-format.md) 的
  [支持矩阵](../../cli/docs/contracts/inferyard-current-format.md#格式支持矩阵)，
  `require_core` 拒绝非 3 的 `schema_version` 与 `origin=migrated`；
  `require_current_sources` 拒绝 `migration.json`、`requests.jsonl`、
  `token-budgets.json`、非当前命名的 `.request.json` 与 `request-files`，不转换、不
  迁移旧格式。容器化 layout 2 的决策见
  [ADR 048](../../cli/docs/decisions/048-run-evidence-containers.md)。
- 离线安全：离线读取不发模型请求，也不按证据中的 URL 取内容；携带包 sources 只读包内
  相对路径并拒绝 symlink/逃逸。细节见
  [离线读取契约](../../cli/docs/contracts/offline-reading-contract.md)。
- 旁证边界：独立 baseline 由 `BaselineEvidence` 读自身协议；缺 run 的任意旁证目录
  不构成合法 run。engine-fit 与 extension 封包、Go observer 各自遵循独立契约，不进入
  layout 2 run 绑定。
- 呈现封存：报告/比较目录的 `artifact-manifest.json`（`presentation-seal.v1`）由
  `read_sealed` 逐字节核验，semantic 核验另计；`verify` 经 `check_presentation_seal`
  触发，渲染一致性只在显式 `--rerender` 下检查。呈现与退出语义见
  [离线读取契约](../../cli/docs/contracts/offline-reading-contract.md#新呈现封存与定位)。
- 错误呈现：证据错误原因经 `safe_reason` 归一后进入 stderr 与结果 reason，不回显
  凭据；凭据只在 `Redactor` 落盘前脱敏，脱敏后无法复核的评分输入不给可复现成绩。
- 序列化边界：`json_bytes` 固定 sort_keys、紧凑分隔符并拒绝非有限数，成员摘要与事件
  行共用同一编码，离线核对不依赖重新格式化的猜测。
- 读取范围：完整读取校验所有物理原件；`projection=True` 只保留 inventory/大小/事件
  摘要范围，不升级为完整资格；容器尾截断按实际文件名记入 `container_tails`，全量
  preload 后由账本归入 partial，不把截断当成功。

## 长上下文速度接入

专项 [`evidence/context_speed.py`](../../cli/src/inferyard/evidence/context_speed.py)
复用完整 batch history/ledger 核验，保存身份无需匹配当前源码。逐次物理引用、正式请求、
事件序号、token budget 与封存的认证规则集中在
[专项读取说明](../../cli/docs/contracts/context-speed-contract.md#cli-物理读取实现)。
合法零 run 可读，来源矛盾或坏原件不能降格为缺测；只读入口不生成派生文件。

## 验证

以下命令在仓库根执行；pytest 子集覆盖本页写入、布局、账本与离线读取行为：

```sh
cd cli
uv run --frozen pytest -q \
  tests/unit/test_storage.py \
  tests/unit/test_storage_sync.py \
  tests/unit/test_evidence_layout.py \
  tests/unit/test_layout_consumers.py \
  tests/unit/test_layout_consumption.py \
  tests/unit/test_layout_read_reuse.py \
  tests/unit/test_observations.py \
  tests/unit/test_ledger.py \
  tests/unit/test_offline_reading.py \
  tests/unit/test_baseline_reads.py \
  tests/unit/test_run_projection.py \
  tests/unit/test_current_formats.py
uv run --frozen ruff check src tests scripts
uv run --frozen ruff format --check src tests scripts
# 全量回归（与根目录 mise task test 相同）：
uv run --frozen pytest
```
