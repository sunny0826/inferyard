# 事件、容器与封存布局

本页是[扩展协议与受控工作流](../extensions-protocol.md) 的子文档，收录「事件、容器与封存布局」的正文；内容不变，入口保留原节标题与锚点。

`ExtensionJournal`（[`extension_evidence.py`](../../../cli/src/inferyard/extensions/extension_evidence.py)）是扩展容器的事实源，行为要点：

- 每条事件写 `events.jsonl`，`definition` 固定 `extension_event.v1`，字段含 `seq`、`clock_id`、`phase`、`event_type`、`request_id`、`data`、`monotonic_ns`、`utc`，写入前过内联 schema 校验。`request_started`、`request_finished`、`tool_terminal`、`control_checkpoint` 四类事件以 `sync=True` 追加（同步 flush+fsync），其余事件可延迟落盘；`phase` 在 live 编排中固定为 `formal`。live 编排把 `store.event` 挂到适配器的 `record` 回调，capability、取消探测、逐请求有效参数与工具参数事件由此进入同一日志。
- `observation` 对 `external-cpu.jsonl`、`request-environment.jsonl`、`safety-checks.jsonl` 三个名字惰性建文件并追加，其余名字委托 `EvidenceStore.observation`；`sample` 按当前 `sample` 文档校验后写 `memory.jsonl`，并强制补写 `schema_version`、`run_id`、`experiment_id="extension"`、`trial_id=run_id` 与 `clock_id`。
- live 成功路径容器统一为 `extension_packet.v2`（`run.json` 标识与 origin），基础文件为 `plan.json`、`config.frozen.json`、`bundle.json`、`identity.json`；`measurement-plan.json` 仅在 `spec.definition=total_observer_control.v2` 时存在，不能按容器版本推导 spec 文件集合；`observer-arms/` 子目录按实际控制臂生成，`child-evidence.json` 可为空清单。运行期日志 `events.jsonl`、`memory.jsonl`、`guardian.jsonl` 与观测文件；收尾时 `packet.json`、`summary.json`、`child-evidence.json`；最后 `manifest.json` 封存（哈希身份与来源，不哈希自身，语义与正式通路一致）。异常路径多一个 `stop.json`。
- `child-evidence.json` 逐条记录 observer 子运行目录及其 `manifest.json` 的哈希，`verify_packet` 对每个子目录重算 manifest 并核验封存，子证据被改动即拒绝（`extension_child_observer_evidence_changed`）。
- off 臂的 `BaselineJournal`（[`trial_control_baseline.py`](../../../cli/src/inferyard/extensions/trial_control_baseline.py)）实现 `RunRecords` 的对应领域方法但全程只写内存：snapshot/event/observation 都经 redactor 清洗后缓存，`seal()` 仅置内存标志，`close()` 不动作；`data()` 从 formal 阶段的 `request_started`/`request_finished` 事件对重建请求行。臂关闭后 `run_total` 调 `deferred_evidence()`，由父容器把缓冲保存为 `baseline-<n>.json`（`definition="trial_control_baseline.v1"`，`preservation` 字段标注 `memory_during_arm_file_after_measured_close`）。`CommonGuardSafety.check` 是空操作，其 metadata 标注 scope 为独立绑定的共同守护，即 off 臂把 in-process 观测换成守护进程证据、其余请求生命周期保持原样。
- 扩展容器不进入通用 run 布局：`evidence_layout_version=2` 与核心 3 的快照规则不套用，`BaselineJournal` 不加 run 容器文件，`ExtensionJournal` 也不增加 run 容器成员，归属见 [run-evidence-layout 契约](../../../cli/docs/contracts/run-evidence-layout.md)。snapshot 仍是原子发布；凭据 redactor 在 journal 构造时传入，写入侧统一脱敏。
