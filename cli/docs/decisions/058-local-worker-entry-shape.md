# ADR 058：本地引擎 worker 入口核验从协议复刻改为形状识别

状态：Accepted。日期：2026-11-13。回归与简化前真实 worker 对照验收。

## 决策

`validate_python_worker` 只识别隔离启动结构与标准 multiprocessing
入口形状：`-I -S -B` + `-c` 结构、`import sys` 首语句剥离，以及
`ImportFrom(multiprocessing.spawn|resource_tracker|forkserver)` 的
`spawn_main`/`main` 单名导入与同名调用。不再用 AST 复刻 CPython spawn
协议的 keyword 值（tracker_fd、pipe_handle、authkey_r、forkserver 的
sys_path/main_path、`sys.argv[1:]` 继承的精确 AST 比较）。
`observe_workers` 复核遍的环境检查从与观察遍的全等比较改为
`check_environment` 受控面检查（声明子集 + 前缀黑名单），与观察遍一致。

伴随删除 root 的 venv 结构解析（`_python`/`import_paths`）：该解析原
只为取得 forkserver `sys_path` 的期望值；sys_path 检查删除后无消费者，
root executable 的受控性由候选声明 argv 匹配、运行时清单 (device, inode)
锚定与 bootstrap 精确匹配共同保证。

worker 环境前缀黑名单维持现状：Studio 启动 direct 引擎时显式构造
环境（七键白名单 + 声明环境，`engine_process_tree.go` 有回归保护），
黑名单与白名单零交集；仅操作者手工启动路径全量继承 shell，拒绝未声明
的影响性变量是设计意图。

## 取舍

简化前的协议值复刻防的是「标准入口但参数异常」的 worker。在受控启动
前提下该场景不可达：root 进程 argv 已由候选声明精确冻结
（`validate_arguments` 的 bootstrap.py 严格匹配），worker executable
按 (device, inode) 匹配预哈希运行时清单，完整 argv 以 `argv_sha256`
进入绑定快照。协议值复刻的增量贡献趋零，而它把 CPython 内部协议
细节硬编码进核验代码，Python 升级即产生误拒。

复核遍的环境全等比较对无控制面的动态状态做字节级比较，误报面大于
受控面模型；`check_environment` 是该模块已确立的受控面比对语义。

## 风险与拒绝

形状级识别会放行「入口形状标准、keyword 值任意」的 worker。该暴露
仅存在于已被 root argv 冻结约束的场景内，且完整 argv 进入证据快照可
复核。拒绝路径回归保留：结构反例仍拒
（`local_engine_python_worker_isolation_required`），非标准入口仍拒
（`local_engine_python_worker_entrypoint_unverified`）。

`local_engine_service_changed` 在 workers.py 内的触发点随全等比较
移除而消失；service.py `_direct` 的稳定性确认仍触发该错误码，
错误码整体非死码，契约文案不变。

worker 识别与入口契约见[本地引擎契约](../contracts/local-engines.md)；
证据解释遵循[血缘规则](../data-contract.md#证据血缘与比较结论)。
