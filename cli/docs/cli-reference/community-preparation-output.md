# 社区安装准备的输出与退出码

本页是[CLI 接口](../cli-surface.md) 的子文档，收录「JSON 与共享类型」「社区准备 details 的字段类型补充」「新目录、失败与退出码」的正文；内容不变，入口保留原节标题与锚点。

## JSON 与共享类型

stdout 恰好一个 UTF-8 JSON 对象，使用现有 CommandResult 外层。`command` 分别为
`init`、`runtime prepare`、`config assets`、`config create`、`config bind`（包含空格）。
成功退出 0，`status="prepared"`、`completeness="complete"`、`run_id=null`、
`evidence_dir=null`、`limitations=[]`。complete 只表示准备产物完整，不是测评完成。
`details` 必含 `out`（绝对目录）、`model_requests_sent=0`、`ready_to_run=false`，
并按命令包含下表字段；文件路径为字符串，SHA256 为 64 位小写十六进制，bytes 为非负整数。

| command | details 的其余必填字段 |
| --- | --- |
| `init` | `bundles`（按导出顺序，每项含 `bundle`、`bundle_path`、`bundle_sha256`）；顶层 `bundle`、`bundle_path`、`bundle_sha256` 仍指向第一个题包；`readme`、`configs`（键 linux/macos/windows 对应绝对路径）、`preparation` |
| `runtime prepare` | `profile`、`mode`（cpu/cuda）、`engine`、`engine_sha256`、`runtime_library_manifest`、`receipt` |
| `config assets` | `model`、`engine`、`template`（各为 `{path, bytes, sha256}`）、`runtime_library_manifest`、`libraries`（基名到 SHA256）、`assets` |
| `config create` | `candidate`、`template`、`runtime_library_manifest`、`device_preflight`、`bundle`、`results`、`model`（路径）、`model_sha256`、`engine_sha256`、`mode`（cpu/cuda/metal）、`preparation` |
| `config bind` | `config`、`candidate`、`pid`、`start_ticks`、`endpoint`、`preparation` |

`init`、`config assets/create/bind` 的 JSON 侧文件使用
`kind=community_init.v1/config_assets.v1/config_candidate.v1/config_binding.v1` 和
`schema_version=3`；内容为 `{kind, schema_version, ...details}`。Windows 回执有独立字段契约。
准备侧文件不注册为 run/报告，不由 `verify --path` 猜测为封存证据。

示例（路径仅示意）：

```json
{
  "command": "config bind",
  "status": "prepared",
  "completeness": "complete",
  "run_id": null,
  "evidence_dir": null,
  "limitations": [],
  "details": {
    "out": "/work/bound-new",
    "model_requests_sent": 0,
    "ready_to_run": false,
    "config": "/work/bound-new/config.toml",
    "candidate": "/work/candidate-new/candidate.toml",
    "pid": 1234,
    "start_ticks": 5678,
    "endpoint": "http://127.0.0.1:48857",
    "preparation": "/work/bound-new/preparation.json"
  }
}
```

请求仍用 `application/types.py` 的 CommandRequest，结果仍为 CommandResult，不另建 CLI DTO。
新增可空字段含 `workspace: Path`，其余字段冻结为：`community_bundle: str`、`runtime_profile: str`、`archive: Path`、
`runtime_archive: Path`、`engine_path: Path`、`runtime_receipt: Path`、`preflight: Path`、
`bundle_path: Path`、`candidate: Path`、`model_repo: str`、`model_revision: str`、`port: int`。
复用 `out`、`model_path`、`output_root`（--results）、`server_pid`（--pid）、
`endpoint_url`（--endpoint）；默认值在新命令构造请求时填入，不影响其他命令。
CLI/application 只负责参数、分派、结果和错误映射；资源、资产、候选、绑定放在 config/platforms。

## 社区准备 details 的字段类型补充

上述冻结字段名不变。所有绝对文件/目录路径（`out`、`bundle_path`、`readme`、`preparation`、
`runtime_library_manifest`、`receipt`、`assets`、`candidate`、`config`、`device_preflight`、
`results`，以及 create 的 `model`/`template`、runtime prepare 的 `engine`）均为非空 string。
`bundles` 在 init 为非空 array；省略 `--bundle` 时按 `zh-core`、`zh-svg-pelican` 顺序含两项，
显式选择时含一项。每项为含 `bundle`、`bundle_path`、`bundle_sha256` 的 object，
字段类型与顶层同名字段一致。顶层 `bundle` 在 init 为第一项的题包名称 string，
在 create 为题包绝对路径 string；`profile` 为固定名称 string。
`mode` 为表内枚举 string。`configs` 为恰含 linux/macos/windows 三键的 object，值为绝对路径 string。
`libraries` 为非空 object，键为文件基名 string、值为 SHA256 string；所有 `*_sha256`
均为 64 位小写十六进制 string。assets 的 `model`/`engine`/`template` 为恰含
`path: string`、`bytes: integer >= 0`、`sha256: string` 的 object。
`pid`、`start_ticks` 为正 integer，`model_requests_sent` 为 integer 0，`ready_to_run` 为 boolean false；
整数不接受 boolean。`endpoint` 为按现有规则验证的本机无凭据 origin string。

Windows candidate 引用 runtime prepare 目录中的实际引擎和清单，不复制这些资产。
移动或删除该目录会使既有候选路径失效；应在确定的新资产位置重新准备候选并绑定服务，
不能只移动候选文件或修改回执来继承原服务身份。单独移动完整 runtime 目录后生成**新候选**时，
回执相对路径仍须通过包内 profile 和实际文件复核。

可识别的不支持格式返回 unsupported_format/2，details 含 artifact、saved_version、supported_versions。
坏 JSON、布尔版本、坏 seal/hash 或当前格式语义冲突为证据错误/4；配置输入错误为 2。
支持矩阵与主机状态规则见[当前格式契约](../contracts/inferyard-current-format.md)。
## 新目录、失败与退出码

输入校验通过后排他创建新目录；已存在的文件、空目录、符号链接（包括悬空链接）一律拒绝。
父目录应已存在且可写，不悄悄创建多级父目录；输入不能位于本次新输出内部。
写文件使用排他创建，拒绝输出树中的链接/Windows reparse point，不覆盖并发创建的对象。
检查失败不动输入或旧输出；创建目录之后的 IO 失败/取消可保留本次不完整目录，
不写成功回执，重试应换新目录。元数据最后写出；完成前复核文件、配置和模板字节。

| 退出码 / status | limitations 固定原因与边界 |
| --- | --- |
| 2 / blocked | `invalid_input`：缺参数、未知参数、互斥参数、非法 JSON/TOML/结构/字段类型、非法 PID/port/endpoint；`unsupported_platform`：动作不支持当前平台/架构；`output_exists`：输出已存在或发生排他创建冲突 |
| 2 / blocked | `unsupported_runtime_profile`、`runtime_archive_required`、`runtime_archive_forbidden`；`archive_identity_mismatch`、`unsafe_archive_entry`、`runtime_library_collision`；`runtime_receipt_invalid`、`runtime_profile_mismatch`、`runtime_asset_mismatch`：输入回执/清单不合法或与包内可信 profile/实际资产不符 |
| 2 / blocked | `device_preflight_blocked`、`device_selection_changed_repeat_device_check`、`ac_power_required`；`model_chat_template_required`、`invalid_library_manifest`、`unsupported_engine_build_requires_adapter_validation`；`service_identity_changed`、`startup_arguments_mismatch`；其他已有本地配置/身份阻断沿原安全原因映射，不退化为成功 |
| 4 / error | `io_error`：文件缺失、权限、磁盘/读写错误；`engine_version_query_failed`：版本查询非零或超时；`package_resource_invalid`：包内模板/profile/题包副本缺失或自检失败；`serialization_mismatch`：工具写出的 TOML/模板复核不符；其余未预期故障为 `internal_error` |
| 130 / interrupted | `cancelled`；只结束本命令自有版本查询子进程，不操作操作者的服务 |

失败外层仍有 `command`，`completeness="incomplete"`、`run_id=null`、`evidence_dir=null`、
`details=null`，limitations 为对应原因数组；stderr 只输出脱敏诊断，不泄露凭据或原始进程命令行。
`config bind` 读取进程身份/argv 时，保留代码定义的 `service_process_unavailable` 和
`service_identity_unreadable` 两个固定原因；仍退出 2 / blocked，不创建输出目录。
未知预检异常或包含额外文本的原因继续走原安全映射，不回显异常原文。
JSON 重复键、NaN/Infinity、bool 冒充整数均拒绝。新准备动作不使用退出 3；
已有执行及核验入口的退出语义不改变。
