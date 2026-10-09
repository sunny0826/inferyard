# 工程脚本

安装用户使用 `inferyard`，准备流程见[安装指南](../docs/installation.md)。本目录用于源码维护与专项操作，
运行前检查参数、默认路径和副作用；名称为 `verify` 或 `prepare` 不表示只读。不要批量执行 `verify_*`。
开发环境见[贡献指南](../CONTRIBUTING.md)，命令使用 `mise exec -- uv run --frozen python scripts/NAME.py`。

## 常用工具

| 职责 | 入口 | 输入与副作用 |
| --- | --- | --- |
| Schema/catalogue | [export_schemas.py](export_schemas.py)、[export_catalogue.py](export_catalogue.py) | 从活动源定义生成资源；`--check` 只检查一致性 |
| 社区资源 | [export_community.py](export_community.py) | 复制已审题包原字节、生成 profile/摘要；读取固定归档，不执行引擎 |
| 发行物 | [build_distribution.py](build_distribution.py)、[check_distribution.py](check_distribution.py)、[export_runtime_constraints.py](export_runtime_constraints.py)、[prepare_release_candidate.py](prepare_release_candidate.py)、[verify_release_candidate.py](verify_release_candidate.py) | 新目录构建、安装结果绑定、候选生成与字节暂存，不上传 |
| 配置 | [bind_service_config.py](bind_service_config.py)、[create_macos_benchmark_config.py](create_macos_benchmark_config.py)、[create_windows_benchmark_config.py](create_windows_benchmark_config.py)、[create_windows_candidate.py](create_windows_candidate.py) | 读取本机身份和资产，写新候选或 bound；部分入口查询引擎版本 |
| 题包 | [build_phase2_bundle.py](build_phase2_bundle.py)、[build_position_bundle.py](build_position_bundle.py)、[build_structured_clarity_bundle.py](build_structured_clarity_bundle.py)、[build_structured_fields_bundle.py](build_structured_fields_bundle.py) | 创建题包与审核材料；改内容后仍须人工审核 |
| 扫描准备 | [prepare_length_scan.py](prepare_length_scan.py)、[prepare_position_scan.py](prepare_position_scan.py) | 从准备证据生成新实验定义，检查各脚本要求 |
| 原生采集 | [collect_windows_machine.py](collect_windows_machine.py)、[collect_thermal_diagnostic.py](collect_thermal_diagnostic.py) | 读取设备/温度与来源，写诊断，不等于模型测评 |
| 观察器 | [build_observer.py](build_observer.py)、[analyze_observer.py](analyze_observer.py) | 构建独立 Go 二进制，或只读 JSONL/封存 run 写新摘要 |
| 来源核对 | [verify_lineage_files.py](verify_lineage_files.py)、[import_external_measurements.py](import_external_measurements.py) | 核对谱系、导入有来源观测；历史迁移工具已移除，旧数据回原项目处理 |

题包生成器依赖 `phase2_review.html`，它是活动模板，保留。
真实 candidate/bound/frozen 配置和 `validation/` 不作为这些工具的默认回归或发行输入。

## 配置兼容脚本

包内 `config assets/create/bind` 的输出是新目录；旧脚本保留其文件输出与旧回执接口。
`create_macos_benchmark_config.py` 仅原生 macOS，读取本地 Prism 资产、重查容量/AC、
执行有界 `--version`，输出 TOML、模板、库清单和 preparation 侧文件，不启动模型服务。

`create_windows_candidate.py` 从已核验 receipt 绑定模型 SHA/revision，从对应 GGUF 内嵌模板
创建同名前缀 `.chat-template.jinja` 并计算摘要；缺模板拒绝，不能借用历史设备的模板或哈希。
实际 Windows 准备与绑定应优先采用[包内入口](../docs/installation.md#windows-x64先准备固定-runtime)。

[prepare_windows_runtime.py](prepare_windows_runtime.py)、[prepare_windows_cuda.py](prepare_windows_cuda.py)
与 [bootstrap-windows.ps1](bootstrap-windows.ps1) 是保留的操作者工具，可能下载固定资产或准备环境，
仍有各自默认路径；它们不是安装包 `runtime prepare` 的离线行为。
[run_windows_mvp.py](run_windows_mvp.py) 会启动、测评并回收自己拥有的服务，名称保留；核心 CLI 始终连接外部服务。

## 有真实执行副作用的专项工具

| 工具 | 必需输入 | 范围 |
| --- | --- | --- |
| [verify_current_sampler_overhead.py](verify_current_sampler_overhead.py) | `--input DIR --out NEW`，DIR 含冻结 `config.json` / `bundle.json` | Linux 短负载采集开销，会启动自有服务并发模型请求；保留固定配方与停止条件 |
| [verify_resource_performance_trial.py](verify_resource_performance_trial.py) | `--input DIR --out NEW` | 同类资源/性能专项，显式输入不代表通用协议或已获运行授权 |
| [verify_real_position_prepare.py](verify_real_position_prepare.py) | `--plan FILE --out NEW` | 使用明确计划的材料位置验证，检查服务及清理行为 |
| [operator_acceptance.py](operator_acceptance.py) | `--packet DIR --source-sha256 SHA --plan-sha256 SHA --out NEW` | 特定 Linux 40 题复现包；正常模式启动自有服务，`--check` 只核对包但仍写检查结果 |

`operator_acceptance.py` 保留原特定安全配方，不是所有实验的统一验收入口，也不要求另一操作者签收。
这些工具不自动继承历史温度/内存覆盖授权；停止后保留原件，不因脚本名字而反复执行。
历史请求快照迁移及 Windows 证据转换脚本已移除；旧数据处理回原项目进行。

## 社区资源与发行检查

```bash
mise exec -- uv run --frozen python scripts/export_community.py --check
mise exec -- uv run --frozen python scripts/export_community.py --check --archives .tools/community-archives
```

`export_community.py --archives DIR` 先核验三份 Windows ZIP 的整包大小/SHA，再派生成员与库清单。
去掉 `--check` 才写生成资源。无归档的 `--check` 只核对已提交 profile/资源和题包漂移，不是重新验证归档。
模型、归档和原始运行不进入发行包；题包字节与人工审核信息保留。

约束导出按目标平台核对 `uv.lock` 的运行依赖闭包；检查器核对 wheel/sdist、资源允许列表、
元数据和摘要，可比对 sdist 重建 wheel。

完整命令顺序见[候选构建与安装检查](../CONTRIBUTING.md#local-candidate-build-and-installation-checks)：
`build_distribution.py` → `tests.packaging.prepare_inputs` → `tests/packaging/run_installed.py`
→ `prepare_release_candidate.py` → `verify_release_candidate.py`。前两步生成构建和合成输入；
安装检查必须在源码外新目录实际完成，输出 `installed_safe_checks.v3`。候选生成器接受
`--manifest BUILD/manifest.json --installed RESULT --source-commit SHA --out NEW`，
`--installed` 可重复，按实际平台汇总为 `community_distribution.v2`。
至少一个平台通过，其余为 `not_verified`；旧 v1 不能默认为通过新条件。

清单摘要、版本、许可证和来源绑定继续核验；暂存及后续上传使用已核验的同一份字节，不重新构建。
本地候选不要求手填三平台 `gate_a/gate_b` 或名称归属布尔值；仓库公开状态、PyPI 名称和
Trusted Publisher 留给实际上传准备。核验器要求 `--manifest`、生成器输出的 `--manifest-sha256`、
同一 `--source-commit` 和新 `--stage` 目录；可加 `--packages-only` 只暂存 wheel/sdist，仍核验完整候选。

发布 workflow 仅手动触发，默认 `destination=verify-only`。必须指定成功且由 `workflow_dispatch`
触发的 package-check 来源 `run_id`、完整 `source_commit`、候选 `manifest_sha256`；
dispatch ref 与源码提交一致。PR 运行不满足发布来源条件。核验后从暂存原字节生成 `SHA256SUMS`。
显式选择 `github-release` 上传附件并创建草稿 Release，公开草稿是独立操作；
`pypi` 或 `both` 使用相同核验字节实际上传 PyPI，并要求预先配置 Trusted Publisher。
完整操作见[Actions 发布步骤](../CONTRIBUTING.md#preparing-a-release-with-github-actions)。
项目采用 [MIT 许可证](../LICENSE)。原生模型准备与性能实验不作为安装检查结果，
具体候选状态见 [backlog](../docs/backlog.md)。

`tests/packaging` 使用合成证据在源码外检查 uv tool/uvx、资源、离线报告与升级/回退。
首次准备解释器和依赖可联网；合成升级不代表历史正式版本兼容。固定主机锁/dirty 的
`installed_host_state.py` 与 `installed_request_chain.py` 仅用于显式允许的一次性 GitHub-hosted runner，不能在操作者工作机运行。
`ci_installed.py` 和初始化入口核验 `GITHUB_ACTIONS=true`、`RUNNER_ENVIRONMENT=github-hosted`、
`LAB_DISPOSABLE_HOST_TEST=yes` 三项；顺序固定为 **自动初始化 → success 请求链 → crash/dirty 持久化 → dirty 拒绝**。
初始化阶段调用 `installed_host_state.py --initialize`，依赖首次 `HostLock` 进入自动建立 clean 状态，
保存 `host-initialization.json`；已有新 state 即拒绝，不清空 dirty。其余阶段只使用既有 clean
状态，crash 后的 dirty 保留到 runner 销毁。
请求链复用导出的 scenario 时关闭临时夹具初始化；导出仍完整包含 helper，
默认 scenario 可在源码外独立构建。隔离回归使用复制包与临时注入根，不冒充 wheel 安装或 Windows 原生验收。

## 源码获取宿主库

`ninfer_source_host/` 的 custody 保存原进程引用并推进期限，Windows backend 负责原子入 Job、
非阻塞管道与句柄。文件 I/O 只在直接子进程或注入测试中执行；控制宿主不执行文件 I/O。
`file_io.py`、`probe_host.py`、`probe_fixture.py` 的真实 CLI 入口仍拒绝运行，不提供下载或模型入口。
库接口与待接手状态见[宿主契约](../docs/contracts/windows-source-host-contract.md)，未接入事项见 [backlog](../docs/backlog.md)。
