# InferYard 首次上手路线图

本工作区是 `{{WORKSPACE}}`。请使用本地已有的模型与引擎文件；如需获取请另行下载。
任何命令都不会启动或停止模型服务。保留原始产物，每次准备或报告都使用新目录。

随时可用以下命令查看当前进度并续接：

```bash
inferyard progress --workspace "{{WORKSPACE}}"
```

`progress` 只读取、不写入文件。JSON 会给出每个题包的阶段、阻断原因和下一步命令。
退出 0 只代表工作区可读，不代表测评完成。需要引擎、运行时回执、PID 或端点的命令
使用显式 shell 变量占位；执行前把它们设置为真实本地资产和已由你启动的服务。

## Context-speed workspace

For an explicitly selected `zh-context-speed` workspace, use the dedicated flow below
instead of the ordinary core/SVG and report steps. The shipped corpus carries human
approval for all three cases. Changed or unapproved content still fails the review gate.

1. Prepare with `config create --profile context-speed --context-size 65536` and the
   existing platform asset arguments. Supported context sizes are 65536, 131072 and 262144.
2. Start the declared service externally and use `config bind` with its actual PID.
3. Freeze with `plan --preset context-speed --config BOUND_CONFIG --out NEW_PLAN`.
4. Run with `run --plan NEW_PLAN/plan.json --output-root NEW_BATCH`.
5. Read JSON with `speed-results --batch NEW_BATCH`; no HTML report is produced.

Each new measurement uses a new plan and batch. Do not resume or rerun old trials.
`progress` recommends the dedicated commands and can open a batch with zero trials.

## 1. 检查设备与已有模型

```bash
inferyard device-check --model /path/to/model.gguf --json --out "{{WORKSPACE}}/preflight/device-1"
```

preflight 目录已存在；在其中使用新的子目录。不带模型的检查可以成功，
但准备候选需要一条模型推荐结果。

## 2. 准备核心题包候选

macOS arm64：

```bash
inferyard config create --workspace "{{WORKSPACE}}" --bundle zh-core --engine /path/to/llama-server --out "{{WORKSPACE}}/candidates/zh-core-1"
```

Windows x64 需先单独准备固定运行时，并把 `--engine FILE` 换成 `--runtime-receipt FILE`。
Linux x64 使用 `config assets` 和导出的 Linux 示例手工准备候选；不支持 `config create`。
`--workspace` 会选取最新的 preflight 产物，并把结果根默认为 `{{WORKSPACE}}/results`；
显式传入的 `--preflight` 与 `--results` 优先。

## 3. 外部启动服务后绑定

阅读 candidate.toml 中的 `engine.binary_path` 和全部冻结的 `engine.startup_args`，
在另一个终端自行启动该服务，确认其 PID 与本地端点。CLI 绝不隐式下载、启动、停止或重启服务。

```bash
inferyard config bind --candidate "{{WORKSPACE}}/candidates/zh-core-1/candidate.toml" --pid 1234 --endpoint http://127.0.0.1:48857 --out "{{WORKSPACE}}/bound/zh-core-1"
```

把 1234 替换为真实 PID。凭据只能通过 `endpoint.api_key_env` 引用；
不要把密钥明文写进命令或配置。

## 4. 运行核心题包

发送模型请求前核对设备、预算与停止条件。

```bash
inferyard run --config "{{WORKSPACE}}/bound/zh-core-1/config.toml"
inferyard progress --workspace "{{WORKSPACE}}"
```

## 5. 为第二个题包重复准备、外部启动、绑定与运行

```bash
inferyard config create --workspace "{{WORKSPACE}}" --bundle zh-svg-pelican --engine /path/to/llama-server --out "{{WORKSPACE}}/candidates/zh-svg-pelican-1"
```

绑定前先冻结适合 SVG 长输出的预算与上下文；不要沿用核心题包的短输出预算。
在外部启动或更换服务，绑定到 `{{WORKSPACE}}/bound/zh-svg-pelican-1`，再运行其 config.toml。
默认测评覆盖两个题包；仅当用户显式声明固定题包时可只运行该题包，
`init --bundle NAME` 导出该范围。

## 6. 汇总与核验

使用 run 或 progress 返回的实际封存 run 路径：

```bash
inferyard report --runs CORE_RUN SVG_RUN --out "{{WORKSPACE}}/reports/summary-1"
inferyard verify --path "{{WORKSPACE}}/reports/summary-1"
inferyard progress --workspace "{{WORKSPACE}}"
```

报告保留来源血缘。封存的不完整 run 不等于完整测评。保留原始 run 与历史报告，永不覆盖。
