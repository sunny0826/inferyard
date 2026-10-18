# KVMem / NInfer 正式文本入口

本页是[CLI 接口](../cli-surface.md) 的子文档，收录「KVMem / NInfer 正式文本入口」的正文；内容不变，入口保留原节标题与锚点。

配置 `engine.adapter = "kvmem"` 绑定 GGUF，`"ninfer"` 绑定原生 NInfer v3 容器。
`probe --config`、离线 `plan --experiment`、`run --config` / `run --plan`、
`report --runs`、`verify --path REPORT` 使用正式 Journal 和评分通路。
Windows 首版为单文件、串行、fixed quality、无缓存文本；原始 run 可直接 verify，
也可通过报告产物核验。跨平台可离线冻结 Windows 计划、生成报告和核验。

示例：[KVMem](../../configs/windows-kvmem.example.toml)、[NInfer](../../configs/windows-ninfer.example.toml)。
示例身份均为占位，须绑定真实文件/模板/库、argv/cwd、PID/FILETIME 和外部已启动服务。
默认 `engine.observation_mode = "auto"`：生成使用 `/v1/chat/completions`，流式请求启用
`include_usage`。probe 输出端点能力矩阵；`/lab/v1` 缺失时使用实际探测成功的原生信号，
缺失项记 `null` 和原因，不阻断普通测评。显式设置 `"lab_required"` 才要求完整 lab 能力。
manifest 与离线报告披露观测路径；原生路径不授予引擎内部全服务排空证明，进程级证据单列。
精确模板预算、实际生效参数与引擎排空无法观测时保持缺测。异常请求保留 dirty 并要求绑定恢复。
本次直接实现 M2，engine-fit 现有诊断范围不变。
字段和真实二进制差距见[适配契约](../contracts/windows-ninfer-adaptation-contract.md)，
当前验证进度见[backlog](../backlog.md)。
