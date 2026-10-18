# 首次上手进度契约

本页是[CLI 接口](../cli-surface.md) 的子文档，收录「首次上手进度契约」的正文；内容不变，入口保留原节标题与锚点。

`inferyard progress --workspace DIR` 只读扫描显式工作区，不创建文件、不发送模型请求，
不查询现场硬件或服务。stdout 直接输出 `progress.v1`（不套普通 CommandResult envelope），
诊断走 stderr；有效且可读时退出 0，无论进度；工作区不存在、缺少 preparation.json、
产物不可读或核验失败时退出 2，绝不返回 3/4。失败输出沿用 blocked 错误 envelope。

`init --out DIR` 创建 onboarding 工作区：preparation.json 为 `community_init.v1`，
另含 README.md、bundles/、configs/、assets/、results/，以及空的 preflight/、candidates/、
bound/、reports/。包内 README 的 `{{WORKSPACE}}` 在导出时替换为本次绝对路径。
`device-check --out`、create/bind/report 的输出仍须为新目录，应使用这些目录下的新子目录。

`config create --workspace W --bundle zh-core --engine FILE --out W/candidates/core-1`
将裸名解析为 W/bundles/zh-core.json；完整题包路径仍接受。省略 --preflight 时选择
W/preflight/ 递归发现的 device-preflight.json 中修改时间最新者（同时间按路径排序）；
省略 --results 时冻结 W/results。显式参数优先，--out 仍必填。不提供 --workspace 时
--preflight、--results 仍必填，既有平台、互斥、硬件复核、身份和哈希校验保持原规则。

成功 JSON 字段固定如下：

```json
{
  "kind": "progress",
  "progress_version": 1,
  "workspace": "/absolute/workspace",
  "device_preflight": {"status": "missing", "path": null},
  "bundles": [{
    "bundle": "zh-core",
    "phase": "preflight_missing",
    "candidate_path": null,
    "bound_path": null,
    "runs": [],
    "next_action": "device_check",
    "next_command": "inferyard device-check --json --out /absolute/workspace/preflight/device-1",
    "blocked_reason": null
  }],
  "report": {"status": "missing", "path": null, "runs": []},
  "next_action": "device_check",
  "next_command": "inferyard device-check --json --out /absolute/workspace/preflight/device-1",
  "default_scope_complete": false
}
```

上例省略第二题包条目。bundles 按初始化范围列出，zh-core 优先；每包 phase 依次为
preflight_missing / candidate_missing / bound_missing / run_missing / run_incomplete /
run_complete，动作对应 device_check / prepare_candidate / bind / run / resume / none。
候选识别 candidates/*/preparation.json 的 config_candidate.v1，绑定识别
bound/*/preparation.json 的 config_binding.v1，并按候选路径关联。runs 从 results/ 的
run 或 batch/runs/run 读取，包含绝对 path、complete 和 sealed；完成与封存分别复用
现有证据语义，不把诊断 run 算入完成。单次运行和固定时窗不能 resume，仍报告 resume
阶段并给 blocked_reason，操作者按既有规则新建运行，不自动重试。

顶层 next_action 按 device_check → 各题包 prepare_candidate/bind/run → report → verify → done
排序；未完成 run 的顶层动作是 run，包内动作是 resume。report.status 为 missing / present /
verified，path 为报告目录绝对路径或 null，runs 为其来源绝对路径列表；verified 必须通过现有
报告字节、来源和语义核验。所有导出题包的 run_complete 且汇总报告 verified 时，
default_scope_complete 为 true；固定题包只表示其显式范围完成。

next_command 是包含绝对工作区路径的 shell 命令，路径按 shell 引号转义；
准备和绑定时尚未知的本地资产或进程参数使用必须显式设置的 INFERYARD_ENGINE /
INFERYARD_RUNTIME_RECEIPT / INFERYARD_SERVER_PID / INFERYARD_ENDPOINT 环境变量。
这些变量只传路径、PID 与端点，凭据仍仅由 endpoint.api_key_env 引用。progress 不取得
服务启动权限。重复准备时命令选择尚不存在的输出子目录，不覆盖旧产物。
