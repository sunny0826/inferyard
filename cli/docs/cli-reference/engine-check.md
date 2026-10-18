# 引擎核验契约

本页是[CLI 接口](../cli-surface.md) 的子文档，收录「引擎核验契约」的正文；内容不变，入口保留原节标题与锚点。

`inferyard engine check` 对本地引擎做只读核验，**输入面按平台不对称**：macOS arm64
必填 `--engine FILE`，Windows x64 必填 `--runtime-receipt FILE`（`runtime prepare`
目录中的回执）；两参数互斥，平台错配（Darwin 给回执 / Windows 给引擎）、路径不存在
或不是文件等无效输入返回输入阻断（退出 2）。核验与 `config create` 同源：macOS 核验
引擎同目录 `*.dylib` 清单规则，Windows 按包内可信 profile 重验回执、清单与全部实际
文件。命令不创建目录、不写文件、不启动服务、不发送模型请求。

**执行面披露**：核验会以子进程执行引擎二进制 `ENGINE --version`（固定 30 秒超时，
匹配同一冻结版本模式，当前 `prism-b10743-adfffbe`）；macOS 新提取的 ad-hoc 签名
二进制首次 exec 校验可达约 12 秒，超时预算高于该值。除此版本查询外不执行引擎。

macOS 没有 device preflight 时不存在选定 mode（device-check 推荐可随模型大小在
metal/cpu 间翻转），因此不输出单一 mode，而输出**双 mode 各自结果** `modes`：
`cpu` 为版本与清单规则均通过，`metal` 为 cpu 通过且清单含 `libggml-metal.dylib`；
metal 最终是否采用仍由 `config create` 按 preflight 终审。Windows 的 mode 由回执
profile 自带（`cpu`/`cuda`），与候选 mode 的匹配同样留给 `config create`。**核验
通过不构成候选资格**：preflight、题包审核、模板、mode 匹配与写后复核仍由
`config create` 终审。

输出为 CommandResult，`command="engine check"`；`run_id`/`evidence_dir` 为 null。
核验完成（含不合格结论）退出 0、`status=checked`、`completeness=complete`，details 为：

| 字段 | 类型与含义 |
| --- | --- |
| `eligible` | boolean；通过同源核验为 true |
| `reason` | 下表固定值；eligible 为 true 时是 null |
| `reported_version` | 版本查询通过时引擎自报的首行输出（实测值）；版本不匹配或未执行查询时为 null |
| `expected_release` | 期望的冻结发布名常量 string（当前 `prism-b10743-adfffbe`），与实测无关，恒存在 |
| `modes` | macOS 合格时为 `{cpu: bool, metal: bool}` 双 mode 结果；Windows 或不合格为 null |
| `mode` | Windows 合格时为已验 profile 的 `cpu`/`cuda`；macOS 或不合格为 null |

`expected_release` 如实标注为期望常量，只有 `reported_version` 是引擎实测；
不使用 `engine_release`，避免常量冒充实测。

不合格 reason 固定值（均为退出 0 的核验结论）：

| reason | 含义 |
| --- | --- |
| `invalid_library_manifest` | macOS 引擎没有同目录 dylib、库链接悬空/越界或名称冲突 |
| `unsupported_engine_build_requires_adapter_validation` | 版本输出不匹配冻结版本模式 |
| `unsupported_runtime_profile` | Windows 回执声明未知 profile 名 |
| `runtime_receipt_invalid` | 回执为符号链接，或结构、字段、版本记录不合法 |
| `runtime_profile_mismatch` | 回执常量字段与包内可信 profile 不符 |
| `runtime_asset_mismatch` | 实际文件或清单与可信 profile 的哈希/大小不符 |

**退出码偏离声明**：eligible 与 ineligible 都是核验成功，均退出 0——这是对
device-check 家族「blocked→退出 2」语义的明示偏离，理由是本命令为查询式核验、
不阻断任何后续操作，终审在 `config create`；不合格结论只以 details.reason 呈现。
输入或平台错配退出 2、`status=blocked`、details=null，limitations 为
`invalid_input` 或 `unsupported_platform`。工具错误退出 4、`status=error`，
limitations 为 `engine_version_query_failed`（版本查询非零或超时）、
`package_resource_invalid`、`io_error` 或 `internal_error`，stderr 只输出脱敏诊断，
不输出原始异常或引擎输出原文；取消退出 130（`cancelled`）。

`inferyard --schema engine_check` 导出此 CommandResult 结构；不改变核心证据契约版本。
回归使用夹具文件与模拟版本输出；macOS 真机签名链校验耗时与 Windows 真实归档
核验效果须单独验证，模拟通过不代表原生平台验收。
