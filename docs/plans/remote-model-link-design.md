# 远程模型链接获取设计

状态：已按 2026-10-10 架构审查修订，仍未实施。基线：`d8d211f`。
本文件只定义新增准备阶段，不改变当前 CLI 行为，也不授权实施、提交或发布。

## 问题与约束

操作者希望把 Hugging Face 或 ModelScope 的模型链接交给 InferYard，并继续使用现有测评流程。
当前 `device-check --model`、`config assets`、`config create` 和 `engine-fit plan` 只接收本地路径；`run` 只执行已绑定的本机服务。
`--model-repo` 和 `--model-revision` 只写入来源声明，默认分别是 `local` 和本地文件 SHA256，不会访问远程仓库。

以下约束保持不变：

- CLI 不启动、停止或重启模型服务；服务仍由操作者在外部启动。
- 正式测量仍绑定本地文件、SHA256、进程身份、监听地址和启动参数。
- 缺测保持 `null` 和原因，不把远程元数据缺失填成 0。
- 凭据不写入 token、Cookie 或明文凭据；凭据只能通过环境变量名引用。
- 获取失败不覆盖已有模型、配置或运行目录。

## 调用方式

新增一个准备命令，不把网络获取加入 `run`：

```bash
inferyard model acquire \
  --source 'https://huggingface.co/Qwen/Qwen3-4B-GGUF/blob/REVISION/model.gguf' \
  --out bench-work/models/qwen3-4b
```

成功后，操作者使用新目录中的本地 GGUF 走现有流程：

```bash
inferyard device-check --model bench-work/models/qwen3-4b/model.gguf --out bench-work/preflight
```

命令只接收一个单文件链接。支持的主机只有 `huggingface.co` 和 `modelscope.cn`，路径形式为 `/OWNER/REPO/blob/REVISION/PATH`。
`PATH` 可包含仓库子目录，但必须指向一个 `.gguf` 文件，不能以 `/` 结束。
`REVISION` 必须是 40 位 commit 或文件仓库明确给出的不可变文件标识；`main`、`master` 和其他可变引用拒绝。

可选 `--token-env NAME` 只传递环境变量名。命令行、配置和证据不接收 token 本身。
URL 只接受 `https`，拒绝用户名、密码、query、fragment 和非 ASCII 路径。
成功时 stdout 仍是一个 CommandResult：`status="prepared"`、`completeness="complete"`、`ready_to_run=false`。
退出码沿用现行分类：

| 退出码 | 原因 | 条件 |
| --- | --- | --- |
| 2 | `invalid_model_source` | 协议、凭据、主机、路径、revision、编码或文件后缀不合法 |
| 2 | `model_source_metadata_mismatch` | 远程清单与最终内容的大小或 SHA256 不符 |
| 2 | `model_acquire_incomplete` | 远程拒绝、鉴权失败、网络超时或响应中断 |
| 2 | `output_exists` | 目标目录已存在 |
| 4 | `io_error` | 本地创建、写入、flush、fsync 或关闭失败 |
| 130 | `cancelled` | 操作者取消；已开始的写入必须停止并保留失败目录 |

原始响应正文、请求头和 token 不进入 stdout、stderr 或证据。

## 结构

新增 `src/inferyard/platforms/model_source.py`，作为远程清单的唯一解析与校验者。
CLI 只解析参数；配置模块只接收已校验的本地结果。现有 `run`、绑定和封存代码不导入此模块。

远程清单使用下列冻结字段：

```python
{
    "definition": "remote_model_source.v1",
    "parser": "remote-model-source-parser.v1",
    "platform": "huggingface" | "modelscope",
    "repository": "OWNER/REPO",
    "revision": "<40 hex chars or immutable file revision>",
    "path": "FILE.gguf",
    "bytes": 2497280256,
    "sha256": "<64 lowercase hex chars>",
    "metadata_sha256": "<64 lowercase hex chars>",
    "retrieved_at": "2026-10-10T00:00:00Z"
}
```

`metadata_sha256` 覆盖解析前保留的原始元数据字节。原始响应不写入证据；哈希和解析器版本足够定位解析规则。
获取器先读取仓库元数据，核对 revision、文件路径、大小和 SHA256，再流式写入临时文件。
流式过程同时计数和计算 SHA256，不把整个模型读入内存。
临时文件完成 flush/fsync 后，才原子改名为目标文件。
目标目录同时写入 `source.json`。该文件记录解析后的来源和最终文件哈希，不记录请求头、token 或重定向历史。

后续候选生成不按目录或文件名查找旧的 `source.json`。
操作者必须把这次获取产生的记录路径传给候选生成；记录的 `bytes` 和 `sha256` 必须等于当次选中文件的大小和哈希。
不符时拒绝生成候选。显式 `--model-repo` 或 `--model-revision` 与记录冲突时也拒绝，不用命令参数覆盖已核验来源。
`model.local_path` 仍指向本地文件，`model.sha256` 仍是本地实际字节的 SHA256。
若缺少基础权重、转换工具或量化配方证据，不生成 `model_lineage`，也不把远程文件哈希冒充为完整量化血缘。
这只阻止声称同基础权重或同量化配方的比较；同一本地 GGUF 的普通比较仍按现行文件身份规则判断。

## 方案比较

本次没有独立模型候选或独立评判，以下是主会话的顺序比较。

| 方案 | 结果 |
| --- | --- |
| 让 `run` 直接接收 URL | 拒绝。测量命令会获得网络、磁盘和远程鉴权副作用，并且失败后难以区分获取失败与测量失败 |
| 调用 Hugging Face Hub 或 ModelScope SDK 自动拉取仓库 | 拒绝。它们可解析可变引用、缓存和多文件仓库，超出单文件获取的证据边界 |
| 新增独立获取命令，产出已哈希的本地文件 | 采用。它保留现有测量入口，并把网络失败限制在准备阶段 |

采用方案吸收现有新目录、排他创建和失败保留不完整目录的规则。
它不吸收自动选择最新文件、整仓库镜像或自动启动引擎。

## 取舍与风险

首个版本只支持单个 `.gguf` 文件。多分片仓库、目录链接和非 GGUF 权重仍由操作者自行准备。
获取成功只证明本地文件与远程清单一致，不证明模型可加载、服务可用或测评可比较。

两个平台的元数据接口不同。解析器使用冻结的 `parser` 版本区分规则，不能把一个平台的字段名套到另一个平台。

元数据请求不跟随跨主机重定向。文件下载最多跟随一次重定向，目的主机只能是：

- Hugging Face：`huggingface.co` 或 `*.hf.co`
- ModelScope：`modelscope.cn` 或 `*.modelscope.cn`

重定向目标必须是 `https`，不能包含用户名或密码。
文件 CDN 地址可以保留平台签名所需的 query；其他 query、fragment 和回调地址拒绝。
若请求使用 `--token-env`，Authorization 头只发给原始仓库主机，不转发给 CDN。
最终内容仍核对元数据声明的长度与 SHA256；哈希一致不能放宽主机或凭据转发限制。

一般获取的整体期限是 1800 秒，其中网络读取最多 180 秒，失败收尾最多 60 秒。
声明大小加临时文件开销不能超过目标卷可用空间，上限为 20 GiB。
下载与写盘在可取消的子进程中执行；取消或到期时先停止子进程，再在收尾期限内关闭文件。
现有 Windows 源码获取库的 64 MiB 下载预算不适用于模型文件，不能直接复用。

此设计依赖以下现在没有的能力：

- 没有模型仓库 URL 解析器，也没有 Hugging Face 与 ModelScope 的元数据客户端。
- 没有带大小、哈希、超时、取消和磁盘上限的模型文件下载器。
- 没有 `remote_model_source.v1` 证据结构及对应测试。
- 没有把已核验来源写入候选配置的生成器；现有字段只保存声明。
- 没有真实仓库响应固件。现有测试不覆盖链接解析、元数据不符和中断后重试。

## 实施顺序

实施必须等待明确授权，并按下列顺序交付：

1. 冻结 URL 解析与拒绝规则，先用离线固件覆盖合法链接、可变引用、错误主机和非 GGUF。
2. 用录制的仓库元数据覆盖两个平台的大小、哈希和 revision 不符。
3. 实现有界下载器和新目录提交，覆盖取消、短写、哈希不符和目标已存在。
4. 接入 CLI，确认 `run --help` 没有获得网络参数，获取成功后仍然不能直接运行。
5. 最后才把已核验来源接入 macOS 和 Windows 的候选生成。候选生成复用获取时已计算的文件哈希核对来源记录，不再完整扫描一次模型。
   Linux 仍使用手工配置，不因此获得自动候选生成。缺少转换与量化证据时，只拒绝声称同基础权重或同量化配方的比较。
   ModelScope 实现前必须另有一个固定 revision 的合法单文件覆盖；可变 `master` 不能充当该覆盖。

每一步先增加失败回归，再实现对应行为。真实网络请求不进入默认测试；只有任务明确授权后，才对下面的固定公开文件做一次有界验证。

## 端到端测试模型

固定模型是 `ggml-org/models-moved` revision `499bc8821c6b12b4e53c5bffcb21ec206f212d81` 的 `tinyllamas/stories260K.gguf`。
2026-10-10 的仓库树元数据记录它为 1,185,376 字节，SHA256 为 `270cba1bd5109f42d03350f60406024560464db173c0e387d91f0426d3bd256d`。
ModelScope 的 `ggml-org/models` 模型详情列出同一路径、大小和 SHA256；它当前只公开可变引用 `master`，因此 ModelScope 链接不能作为这次固定来源。

测试链接：

```text
https://huggingface.co/ggml-org/models-moved/blob/499bc8821c6b12b4e53c5bffcb21ec206f212d81/tinyllamas/stories260K.gguf
```

选择它是因为它是公开、官方仓库中的单文件 GGUF，只有约 1.13 MiB。
仓库说明写明这些模型供 llama.cpp CI 使用，不能用于生产，因此这次端到端测试只证明链接解析、下载、落盘和哈希一致。
它不产生质量成绩，也不取得性能比较资格。

这次固定测试另设 60 秒网络超时和 2 MiB 下载上限；它不改变上面的一般期限和 20 GiB 上限。
最终文件必须同时满足字节数和 SHA256；任一不符都删除临时文件并返回 `model_source_metadata_mismatch`。
直接文件地址会重定向到 `us.aws.cdn.hf.co`。这一次跳转符合上面的 `*.hf.co` 规则；最终内容仍按上述两个值校验。
