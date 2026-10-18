# 社区安装准备接口

本页是[CLI 接口](../cli-surface.md) 的子文档，收录「社区安装准备接口」「参数、平台和产物」「各动作的冻结行为」的正文；内容不变，入口保留原节标题与锚点。

本节定义安装包的准备入口，操作示例见[安装指南](../installation.md)。
首批为 Linux x64、Windows x64、macOS arm64；帮助、版本和资源查询仍延迟加载后端。
除下述 `model acquire` 与 `engine acquire` 的文件获取外，这些动作不发送 HTTP 或模型请求。
所有准备动作均不操作主机锁/dirty，不启动、终止或重启模型服务。
`runtime prepare` 和 `config create` 的引擎 `--version` 子进程是明确的例外副作用：
只查询版本，不传模型参数；失败不输出成功回执。`config create` 固定 30 秒超时
（macOS 新解压 ad-hoc 签名二进制的执行期校验偶发超过 10 秒）；`runtime prepare`
使用冻结 profile 的 `version_timeout_seconds`（当前 10 秒）。

## 参数、平台和产物

下表是完整的新命令参数集；所有 `--out NEW` 都是**不存在的新目录**，不是 TOML 文件名。
各路径参数按调用 cwd 解析，写出的配置和 stdout 路径为绝对路径；安装资源用包资源 API 读取，
不查找仓库根。所有命令支持 `--help`，不提供 `--force`、启动或跳过核验开关。

| 命令 | 必填参数 | 可选参数及默认值 | 平台和新目录布局 |
| --- | --- | --- | --- |
| `model acquire` | `--source URL --out NEW` | `--token-env NAME`：只接受环境变量名；`--timeout-seconds 1800` | 三平台；单 GGUF 文件的基名和 `source.json`，失败目录保留 |
| `engine acquire` | `--source URL --out NEW` | `--timeout-seconds 1800` | 三平台；单引擎文件的基名和 `engine-source.json`，失败目录保留 |
| `init` | `--out NEW` | `--bundle {zh-smoke,zh-core,zh-svg-pelican}`；省略时导出 `zh-core` 和 `zh-svg-pelican` | 三平台；`README.md`、`configs/{linux,macos,windows}.example.toml`、`bundles/NAME.json`、`assets/`、`results/`、`preflight/`、`candidates/`、`bound/`、`reports/`、`preparation.json` |
| `runtime prepare` | `--profile NAME --archive FILE --out NEW` | `--runtime-archive FILE`，仅 CUDA 必填，CPU 禁止 | 仅 Windows x64；`engine/`、CUDA 的 `runtime/`、`engine/engine-sha256.json`、`runtime-receipt.json` |
| `config assets` | `--model FILE --engine FILE --out NEW` | 无 | 仅 Linux x64；`chat-template.jinja`、`engine-sha256.json`、`assets.json` |
| `config create` | `--bundle FILE_OR_NAME --out NEW`；无 workspace 时另必填 `--preflight FILE --results DIR`；另按平台指定下述参数 | `--workspace DIR` 推导题包、preflight 和 results；`--port INT` 默认 48857，范围 1..65535；`--model-source FILE` 显式来源记录；无记录时 `--model-repo TEXT` 默认 `local`、`--model-revision TEXT` 默认所选模型 SHA256，显式文本须非空 | 仅 macOS arm64 / Windows x64；`candidate.toml`、`chat-template.jinja`、`preparation.json`；macOS 另有 `engine-sha256.json`，Windows 引用回执目录清单 |
| `config bind` | `--candidate FILE --pid INT --endpoint URL --out NEW` | 无；PID 应为正整数 | 三平台及既有可绑定适配器；`config.toml`、`preparation.json` |

`runtime prepare` 的 NAME 只有 `prism-b10743-adfffbe-win-cpu-x64` 和
`prism-b10743-adfffbe-win-cuda-12.4-x64`，可信归档、清单和回执字段见
[Windows 准备契约](../contracts/windows-prism-batch-contract.md#社区安装的离线运行时准备)。
`config create` 在 macOS 必填 `--engine FILE` 且禁止 `--runtime-receipt`；
Windows 必填 `--runtime-receipt FILE` 且禁止 `--engine`。两者不能同时提供。
不提供跨平台模拟生成参数；Linux 使用 `config assets` 和示例手工冻结配置。
`--model-source` 只读取显式传入的 `remote_model_source.v1/v2/v3` 记录，不按目录查找旧记录。
记录的大小和 SHA256 必须等于当次所选文件；候选生成复用当次文件哈希，不重复扫描模型。
省略 repo/revision 声明时使用记录中的 repository/revision，显式声明冲突则返回
`model_source_metadata_mismatch` / 2。缺少转换和量化证据时不生成 `model_lineage`；
普通比较仍遵循[证据血缘规则](../data-contract.md#证据血缘与比较结论)。
`--results` 只冻结结果根并检查所在卷，不创建运行或覆盖结果；它可为现有目录，
每次实际运行仍按原规则创建新的 run。它不能位于安装包目录或本次 `--out` 内。

## 各动作的冻结行为

`model acquire` 只接受 `https://huggingface.co/OWNER/REPO/blob/REVISION/PATH`
或 `https://modelscope.cn/OWNER/REPO/blob/REVISION/PATH`，revision 必须是 40 位十六进制。
PATH 可包含子目录，但只能是一个 `.gguf` 文件；输出使用其文件基名。
可变 main/master、凭据、query、fragment、非 ASCII 和路径穿越均拒绝。
`--token-env` 只接受环境变量名；token 值放在初始请求的 `Authorization` 头中，
跟随重定向时由 httpx 在跨 origin 跳转自动剥离，不转发给 CDN；
token、请求头和响应正文不输出或持久化。
下载为进程内流式 GET：正文写入 `文件名.part`，flush/fsync 后 rename 发布，
`source.json` 同样先写 `.part` 再发布；失败清理 `.part` 并保留目录，不能重用。
`--timeout-seconds` 默认 1800，作用于各网络操作；必须为正有限数。
没有总期限、自动重试或续传；慢链路应在开始前显式增加该值。
`source.json` 保存 `remote_model_source.v3`、parser、bytes、sha256
及平台、仓库、固定 revision、仓库内路径和获取时间；bytes/sha256 为实测值。
v1/v2 记录仍严格只读，不补写、不转换；字段见[模型获取契约](../contracts/model-acquisition.md)。
成功为 `prepared` / `complete` / 0，details 含 `ready_to_run=false`。
失败原因固定为 `invalid_model_source`、`model_acquire_incomplete`、
`output_exists`（退出 2）、`io_error`（退出 4）、`cancelled`（退出 130）。
stderr 只输出固定原因，不输出原始异常或签名 URL。
模型文件下载完成不证明可加载、服务就绪或测评完成。`run` 仍不接收模型 URL。

获取客户端使用 `trust_env=False`，不使用 `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY`
或系统代理。依赖本机代理的网络需要能接管直接连接的 TUN/全局路由，
仅开启系统代理不足以让获取经过代理。操作者须确认仓库和 CDN 均可达。

`engine acquire` 接受任何 `https://HOST/PATH` 链接（无 query、fragment、
userinfo 或端口，路径分段无穿越），基名即输出文件名；不限 host 与扩展名，
没有 token 选项。下载语义与 `model acquire` 相同：进程内流式 GET、
`Accept-Encoding: identity`、`.part` 后 rename 发布、失败保留目录。
产物为文件基名与 `engine-source.json`（`remote_engine_source.v1`：
parser、source_url、bytes、sha256、retrieved_at，bytes/sha256 为实测值）。
命令不解压、不装配、不启动服务、不核验 profile；引擎身份仍由
`config create` 的 `binary_sha256` 与 `runtime_library_manifest` 冻结，
Windows 归档继续经 `runtime prepare --archive` 核验。失败原因固定为
`invalid_engine_source`、`engine_acquire_incomplete`、`output_exists`
（退出 2）、`io_error`（退出 4）、`cancelled`（退出 130）；规则细节见
[引擎获取契约](../contracts/engine-acquisition.md)。

```bash
inferyard model acquire --source 'https://huggingface.co/OWNER/REPO/blob/COMMIT40/sub/model.gguf' --out bench-ops/models/new
inferyard device-check --model bench-ops/models/new/model.gguf --out bench-ops/preflight/device-1 --json
inferyard config create --preflight bench-ops/preflight/device-1/device-preflight.json --engine /path/to/llama-server --bundle bench-ops/bundles/zh-smoke.json --results bench-work/results --out bench-ops/candidate --model-source bench-ops/models/new/source.json
```

`COMMIT40` 是占位符，必须替换为实际固定 revision；示例候选命令适用于 macOS，
Windows 改用 `--runtime-receipt FILE`，Linux 继续手工配置。服务由操作者另外启动。
慢链路可在获取命令后追加 `--timeout-seconds 14400`，它同时是连接和读取的期限。

1. `init` 导出三个平台的去设备化占位示例。省略 `--bundle` 时导出 `zh-core` 和
   `zh-svg-pelican`；显式 `--bundle` 只导出一个固定题包。示例明确未绑定且不能直接运行；
   不带真实 PID、设备标识、历史绝对路径、凭据或预算资格。题包来自
   `data/community/bundles/`，逐字节复制根 `bundles/` 的权威已审版本，保留内嵌审核信息。
   不更改题目或审核哈希，也不自动选修订题包。`assets/` 和 `results/` 初始为空。
   导出两个题包不触发执行；`run --config` 每次只执行配置中的一个题包，不自动连续运行两包。
2. `config assets` 只读现有单 GGUF、引擎及引擎同目录的 `*.so` / `*.so.*`，
   使用现有 GGUF 解析器与 SHA256 能力，不执行引擎、`ldd` 或版本命令。
   模板按 `tokenizer.chat_template`、再按 `tokenizer.chat_template.default` 选取；
   应是非空字符串。写入恰好 `template.encode("utf-8")`，不改换行、不加 BOM 或末尾换行。
   写后文件 SHA256 应等于该字节串 SHA256；模型和引擎不复制。
   清单为 `{文件基名: SHA256}`，包含引擎和所发现的同目录共享库；库链接只允许解析到
   同引擎目录内的普通文件，按原基名记录，拒绝悬空/越界链接和名称冲突。
   清单是本地资产清点，不证明服务实际加载集合；后续 run 的文件/进程核验保持原规则。
   不生成自动预算或完整 Linux 候选，不借用历史模板。
3. `config create` 接受 `device-check --out` 产生的 `device-preflight.json`，
   或 `device-check --json` 的完整 CommandResult（只从其 `details` 取同一预检对象）。
   应是当前本机平台、`recommendation.status = recommended` 且 selected 非空；
   所选模型、mode、线程、上下文、GPU 索引及内存/磁盘阈值由该推荐冻结，不临时降低预算。
   macOS 保留现有实时复查：重新检查模型、资源、mode 和 AC 电源，变化则要求重新 device-check；
   Windows 保留既有推荐生成规则并严格匹配 CPU/CUDA profile，不把估算称为实跑验证。
   Windows 先按可信 profile 重验回执、清单及实际文件，再执行版本查询；macOS 查询所选引擎版本。
   两平台均只接受现有 `prism-b10743-adfffbe` 版本匹配规则；模板按上一项原字节规则写出。
   `--bundle` 加载并核验原人工审核，配置使用该文件的 version；显式 `--results` 替代仓库路径。
   生成参数沿用对应平台原生成器，不借此统一或改变测量配方。PID/启动时间为未绑定占位，
   `ready_to_run=false`；调用 load_config 及 TOML round-trip 校验后才完成准备。
4. `config bind` 复用原绑定脚本：加载候选、校验本机无凭据 endpoint，读 PID 启动时间及实际 argv，
   再读启动时间以拒绝进程更替；Linux 为 `/proc`、macOS 为原生身份、Windows 为完整 FILETIME。
   只把候选中已有 `--host` / `--port` 值替换为显式 endpoint，分离值与 `--flag=VALUE`
   形式均保留原写法；随后将期望 argv 与实际 argv
   按原脱敏规则严格比较；不能用实际 argv 覆盖其余期望参数。写入 endpoint/PID/start ticks，
   保留其余配置和资产声明，递归 TOML 序列化后复核；不触发服务请求或宣称实时预检已通过。
   后续直接 `run`，由 run 完成当次身份、资产和普通/流式探测；独立 `probe` 仍是可选排障。
