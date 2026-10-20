# 本地五引擎正式接口

macOS arm64 可显式选择 llama.cpp、MLX-LM、vLLM Metal、SGLang MLX 或 LM Studio。
配置与证据边界见[本地引擎契约](../contracts/local-engines.md)，当前验收范围见
[进度](../backlog/model-engine.md#本地五引擎正式适配)。旧 Prism 与 Windows 准备入口保持原语义。

## 安装声明与候选

`engine check --installation FILE` 与 `--engine`、`--runtime-receipt` 互斥。
输入为 `local-engine-installation.v1`：`schema_version=3`、`family`、`executable`、
`working_directory`、`runtime_roots`、`environment`；LM Studio 另有 `models_root`。
所有路径必须是本机绝对路径。Python executable 是独立 venv 的解释器；
LM Studio executable 是 `lms`，后台服务身份另行核验。
Python 安装可选填完整 `worker_environment`，与主进程 `environment` 分开冻结；
启动时只传后者，不能把 worker 的线程标记提前注入主进程。
固定外部安装及补丁准备见[正式安装准备](../local-engine-native-preparation.md)。

核验返回 `local-engine-check.v1` details：`eligible`、`reason`、`family`、
`verification_scope=installation_runtime_closure_only`、`model_requests_sent=0`、
`ready_to_run=false`。只读安装核验不启动模型、不发送生成请求；Python 使用隔离的
标准库查询，不能从 eligible 推导可测评或实际模型兼容性。

```sh
inferyard engine check --installation installation.json
inferyard config create --installation installation.json --model /absolute/model \
  --bundle /absolute/workspace/bundles/zh-core.json --results /absolute/runs \
  --context-size 8192 --max-tokens 512 --timeout-seconds 600 \
  --out /absolute/candidate-core
inferyard config describe --candidate /absolute/candidate-core/candidate.toml
```

候选输出冻结安装、模型文件清单、模板、candidate TOML 和 preparation 回执。
目录必须新建；原件不覆盖。默认 context=8192，core 输出上限=512，pelican=4096，
单请求超时=600 秒；可在冻结前显式指定。LM Studio 候选还要求
`--service-pid PID`，记录现有后台 argv/cwd；不是 lms 命令的 PID。

`config describe` 的 `local-engine-description.v1` 输出提供结构化 launch、
资产引用及绑定状态。直接进程的 `launch.argv`、cwd 和环境供操作者执行；
LM Studio 使用独立 launch 描述。CLI 不执行这些启动动作。

## 绑定、运行与交接

服务启动并加载本地模型后，用新目录保存完整绑定：

```sh
inferyard config bind --candidate /absolute/candidate-core/candidate.toml \
  --pid PID --endpoint http://127.0.0.1:48857 --out /absolute/bound-core
inferyard run --config /absolute/bound-core/config.toml
```

默认测评范围仍为 `zh-core` 和 `zh-svg-pelican`，上例仅展示其中一个候选。
两题包分别准备、冻结和封存，最后 `report --runs CORE_RUN PELICAN_RUN --out NEW_REPORT`。
跨题包必须确认旧主进程和全部已绑定 worker 退出，再启动、绑定新服务。
分开执行两个 `run` 也受到主机持久 scope 记录限制。

冻结批次的显式 handoff，或带 `--from-run` 的重跑，需要在 endpoint/PID 参数之外
传入 `--service-binding NEW_BINDING_JSON`。绑定原件必须属于该配置的安装和模型，
不能只更新 PID；旧主进程消失但 worker 仍存活时阻断。dirty 恢复继续要求一次性
recovery token 和说明，且确认旧进程树退出；队列计数为零不能代替这一步。

首次实现只接入固定串行质量流程。长上下文速度、持续负载、缓存实验和受控性能
比较不因新增家族自动开放。真机模型结果与受控协议回归分别记录。
