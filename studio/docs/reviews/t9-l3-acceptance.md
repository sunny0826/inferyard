# T9-L3 验收报告：真实 run 端到端（冒烟）

验收人：T9-L3 独立验收员（未参与实现）。
验收时间：2026-10-13 00:42 – 00:56（UTC+8）。llama-server 于 00:42 启动，Studio 于 00:46 启动，两台进程于 00:56 停止。中途浏览器命令的传输层中断发生在取消页截图已经落盘之后；续跑没有重启 llama-server，也没有重启 Studio。
本报告是冒烟验收，不是测评结论。zh-smoke 的答案对错不构成 CLI 失败。

验收期间未修改被验收产品文件、未提交 git。仓库内唯一写产物为本报告。
临时产物在 `/tmp/studio-t9/`。Studio 使用隔离 `HOME`（`/tmp/studio-t9/home-l2`）。
主仓库 `inferyard` 与其 `validation/` 只读。引擎、GGUF、题包均按绝对路径引用，没有写入。

## 一、验收基线与环境

| 项 | 值 |
| --- | --- |
| HEAD commit | `3eb5a0b8d2739d4f4e31fc8b12a016a0b6a79bbc` `docs(contract): optional GPU index, §4.6 rationale fix, §4.7 .part lifecycle` |
| 工作树 | 仅未跟踪 `docs/reviews/`。构建在 `/tmp/studio-t9/buildsrc` 的副本上进行，没有改仓库里的 `web/dist` |
| OS | macOS 26.6.2（darwin/arm64，Build 25G83） |
| 电源 | `pmset -g ps`：`Now drawing from 'AC Power'`，内置电池 93%，AC attached。全程 `pmset -g therm` 无热警告、无性能警告 |
| Go | go1.27.2 darwin/arm64。`GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0`，经 `mise exec`。`go version -m /tmp/studio-t9/studio-l3` 首行为 `go1.27.2` |
| 二进制 | `/tmp/studio-t9/studio-l3`，16M。前端 `bun --registry=https://registry.npmmirror.com run build:web` 退出码 0（svelte-check 0 error）。`go build -o /tmp/studio-t9/studio-l3 .` 退出码 0。产物保留 L2 的 `/tmp/studio-t9/studio`，没有覆盖 |
| 应用版本 | `GET /api/status` 的 `version` 为 `0.1.0` |
| inferyard | wrapper `/tmp/studio-t9/bin/inferyard-wrapper`，`--version` 退出码 0，`inferyard 0.0.2`。`PUT /api/settings` 后 `inferyard_status=ok` |
| Chrome | 154.0.8037.98，Playwright `channel:'chrome'`，headless，1280×900 |
| 题包 | `/Users/guoxudong/codes/local-ai/inferyard/bundles/zh-smoke.json`：`bundle_id=zh-smoke-v1`，`version=1.0.0-draft`，40 题（instruction 20 + extraction 20） |
| 墙钟 | 取消路径从 `POST /api/runs` 到终态 1.9s。完整 run 从 `POST /api/runs` 到终态 63.5s。单 run 低于 15 分钟，整轮低于 60 分钟。没有因超时重试 |

冻结契约：`docs/api-contract.md`、`docs/api-types.ts`（含 HEAD 上的两轮补丁）。线束从 L2 的 `/tmp/studio-t9/harness.py` 复制为 `l3_harness.py` 后改指向 `studio-l3` 与 `logs/studio-l3.log`。L2 文件未改、未删。

资产复核（`shasum -a 256` 退出码 0，与任务钉值一致）：

| 资产 | sha256 |
| --- | --- |
| llama-server | `1ad69ebaada923760c52c1dc50daf198a22c2adb4a9dcccd25779fa7079f353b` |
| SmolLM2 GGUF | `ed5fa30c487b282ec156c29062f1222e5c20875a944ac98289dbd242e947f747` |
| chat template | `872be49dbb638044ad01b60388f48d469ff2980e5f0dccdc22ec907db54d0788` |

引擎库 manifest `/tmp/studio-t9/assets/engine-sha256.json` 为 36 条。参考 TOML 从当日 e2e `inputs.json` 的 `configuration_source.content`（base64）解出，sha256 `4e3d2e66102cb8837716628470f52579730d59dfaf74c5f7282c361aeb6f3151` 与记录一致。派生配置在 `/tmp/studio-t9/configs/zh-smoke-l3.toml`：题包改为 zh-smoke，`output.root=/tmp/studio-t9/results`，端口 `18087`（`lsof` 确认空闲），`server_pid=39446`，`process_start_ticks=1791823346592080`（主仓库 `process_start_ticks` 实测）。

验收前 `/var/tmp/inferyard-host.state.json` 已备份到 `evidence/l3/host-state.before.json`，sha256 `f5bad2682e29c4bfab867e3215797c96b0dd952dde4e031ea4d8260f57cb8960`。当时 `dirty=false`，`run_id=20261012T023430Z-9aaf504e3d744766a8cd0edcbd95d9e8`。

## 二、逐项结论

### L3-0 准备 — PASS

| 检查 | 命令 / 动作 | 退出码或 HTTP | 证据 |
| --- | --- | --- | --- |
| HEAD 与工作树 | `git rev-parse HEAD`，`git status --short` | 0 | `3eb5a0b`，仅 `?? docs/reviews/` |
| AC 在线 | `pmset -g ps` | 0 | 见上表。未中止 |
| 引擎健康 | 按 startup_args 启动 llama-server，端口 18087；`curl http://127.0.0.1:18087/health` | HTTP 200，body `{"status":"ok"}` | `logs/llama-server.pid`（39446），`logs/llama-server.log`，`evidence/l3/llama-health.body` |
| 构建 | 副本内 `bun run build:web` 后 `go build -o /tmp/studio-t9/studio-l3 .` | 均为 0 | `logs/bun-build-l3.log`，`logs/go-build-l3.log`（成功时为空），二进制 `/tmp/studio-t9/studio-l3` |
| 设置与 CLI | headless `-session-fd` 启动后 `PUT /api/settings` | HTTP 200 | `inferyard_status=ok`，`version=0.0.2`，`results_root=/tmp/studio-t9/results`，`models_root=/tmp/studio-t9/models`，`reports_root=/tmp/studio-t9/reports`。`evidence/l3/settings-put.json`，`status.json` |
| 设备刷新 | `POST /api/device/refresh` | HTTP 202，`refreshing=true` | 启动时已有一次刷新进行中，第一次 POST 为 409，等待后重试 202。`evidence/l3/device-refresh-post.json` |
| 真实快照 | SSE `device.updated` 与 `GET /api/device` | HTTP 200 | `kind=device_preflight.v1`，`error=null`。`cpu_model=Apple M4`，`logical_cpus=10`，`memory_total_bytes=17179869184`。`models` 1 条：Qwen2.5-0.5B GGUF，`status=available`，`size_bytes=491400032`，`architecture=qwen2`。`recommendation.status=recommended`。`evidence/l3/device-sse.json`，`device-get.json` |

Studio 监听 `127.0.0.1:61794`，pid 40639。日志只有监听行。会话 URL 在 `evidence/l3/session.url`（0600），本报告不记录 token。

### L3-1 进行中取消 — PASS

`POST /api/runs`，`config_paths=[/tmp/studio-t9/configs/zh-smoke-l3.toml]`，`allow_single_bundle=true`。HTTP 202。任务 `fe19ce90ecaf4b13bb4be73b7569224a`。

| 子检查 | 结果 | 证据 |
| --- | --- | --- |
| SSE `run.state` | PASS。顺序 `starting` → `running`，取消前未终态 | `evidence/l3/cancel-states.json` |
| `run.progress` | PASS。`phase=probe`，`done=0`，`total=40`，`last_seq=3`，40 个 case 均为 `pending`。这是真实 tail 的冻结题集，取消发生在 probe，尚未给题打分 | `evidence/l3/cancel-progress.json` |
| `run.resources` | PASS。采样 `mem_available_bytes=4241833984`，`service_rss_bytes=257851392`，`service_cpu_percent=null`（首点无差分），`temperature_celsius=null` | `evidence/l3/cancel-resource.json` |
| `POST .../cancel` | PASS。HTTP 202 | `evidence/l3/cancel-post.json` |
| 任务终态 | PASS。`state=cancelled`，`exit.exit_code=130`，`cli_status=interrupted`，`completeness=incomplete`，`stop_reason=user_cancelled`，`limitations=["cancelled"]`，`error=null`。墙钟 1.9s | `evidence/l3/cancel-terminal.json`。续跑时再次 `GET /api/run-tasks/{id}` HTTP 200，字段相同：`evidence/l3/resume-task.json` |
| 证据事件 | PASS。`events.jsonl` 有 1 条 `run_stopped`，`phase=finalizing`，`data.reason=user_cancelled`（seq 7） | run 目录见下 |
| 历史 | PASS。`GET /api/runs` HTTP 200。该 run `status=cancelled`，`stop_reason=user_cancelled`，`sealed=true`，`completeness=incomplete`，`score=null`。续跑复核仍为 `cancelled` | `evidence/l3/history-cancel-row.json`，`resume-history-row.json` |

run 目录：`/tmp/studio-t9/results/20261012T164646Z-d8cb0a7efc0b43e3a785bde39b479f49`（API 回显为 `/private/tmp/...`，与 macOS `/tmp` 规范化一致）。

取消后 `/var/tmp/inferyard-host.state.json`（副本 `evidence/l3/host-state.after-cancel.json`）：

```json
{
  "dirty": true,
  "dirty_token": "183c8d0bbfb44b19adccb2cf8dad91b3",
  "endpoint": "http://127.0.0.1:18087",
  "kind": "run",
  "process_start_ticks": 1791823346592080,
  "request_id": "20261012T164646Z-d8cb0a7efc0b43e3a785bde39b479f49-probe-1",
  "run_id": "20261012T164646Z-d8cb0a7efc0b43e3a785bde39b479f49",
  "schema_version": 1,
  "server_pid": 39446
}
```

标记 `kind=run` 且 `run_id` 就是这次被取消的 run，`dirty=true`。历史 status 仍是 `cancelled`。与契约里 cancelled 高于 dirty 的呈现一致。

Chrome（`node /tmp/studio-t9/l3_browser.cjs`，退出码 0）：

- RunDetail `#/runs/fe19ce90ecaf4b13bb4be73b7569224a`：徽章「已取消 · cancelled」，退出码 130，停止原因 `user_cancelled`。无 `internal_error` 横幅。console error 0。截图 `/tmp/studio-t9/shots/l3/cancel-rundetail.png`。文本 `evidence/l3/page-cancel-rundetail.txt`。
- 历史页 `#/runs`：该 run 徽章「已取消 · cancelled」，停止原因 `user_cancelled`，得分「证据不足」。事件已连接。截图 `/tmp/studio-t9/shots/l3/cancel-history.png`。摘要 `evidence/l3/browser-cancel.json`。

### L3-2 完整 run 与报告 — PASS

第二次 `POST /api/runs` 使用同一 config、同一仍在运行的 llama-server。HTTP 202。任务 `e6db67ccb33140a29fc494bf53d1fbe1`。CLI 没有以 exit 2 阻断，因此没有做手工 dirty recovery，也没有换端口或换 pid。这是本次观察到的 CLI 行为，记录在第三节。

| 子检查 | 结果 | 证据 |
| --- | --- | --- |
| SSE 序列 | PASS。`starting` → `running` → `done` | `evidence/l3/run2-states.json` |
| 进度 | PASS。终态前 `phase=finalizing`，`done=40`，`total=40` | `evidence/l3/run2-progress-last.json`。日志可见 probe 后进入 formal（约 22s 时 3/40，43s 时 16/40） |
| 资源 | PASS。`run.resources` 128 个采样点。温度字段未出现 ≥95°C。`pmset -g therm` 仍无警告 | `logs/l3-run2.log` |
| 任务终态 | PASS。`state=done`，`exit_code=0`，`cli_status=finished`，`completeness=complete`，`stop_reason=plan_finished`，`limitations=[]`。墙钟 63.5s | `evidence/l3/run2-terminal.json` |
| 证据事件 | PASS。`run_stopped` 的 `data.reason=plan_finished`（seq 17248）。`request_finished` 45 次，`score` 40 次 | run 目录 `events.jsonl` |
| 历史 | PASS。`status=complete`，`sealed=true`，`completeness=complete`，`stop_reason=plan_finished`。`score.passed=1`，`valid=40`，`rate=0.025` | `evidence/l3/run2-history-row.json` |

run 目录：`/tmp/studio-t9/results/20261012T165151Z-32b494eeef654ea6a3dbfd165fc2f251`。

答案质量（冒烟，不判失败）：RunDetail 评分表 40 行均为 `completed`，质量 `pass` 1 行（`instruction-06`）、`fail` 39 行。页面文本 `evidence/l3/page-complete-rundetail.txt`。截图 `/tmp/studio-t9/shots/l3/complete-rundetail.png`：退出码 0，停止原因 `plan_finished`，进度 40/40。

历史页同时显示这次 `完整 · complete`（1/40 · 2.5%）和上一轮 `已取消 · cancelled`。截图 `/tmp/studio-t9/shots/l3/complete-history.png`。无 `internal_error` 横幅，console error 0。`evidence/l3/browser-complete.json`。

`POST /api/reports`，`run_paths` 为上述 run 目录。HTTP 201。`report_id=report-20261012T165255Z`，`html_url=/reports/report-20261012T165255Z/report.html`。证据 `evidence/l3/report-create.json`。落盘 `/tmp/studio-t9/reports/report-20261012T165255Z/report.html`（334KB）。

Chrome 顶层打开 `/reports/report-20261012T165255Z/report.html?token=`。标题「SmolLM2 135M Instruct · 测评档案」。正文有「答案通过数 1/40」「请求完成率 100.00%（完成 40 / 有效执行 40）」。页面自己写明运行完整性与答案质量分开判断，且不授予严格性能比较资格。截图 `/tmp/studio-t9/shots/l3/complete-report.png`。文本 `evidence/l3/page-complete-report.txt`。

完成后主机标记（副本 `evidence/l3/host-state.after-run2.json`）：`dirty=false`，`kind=run`，`run_id` 换成本次完成的 run，`request_id=...-formal-45`，endpoint / pid / ticks 仍是 18087 / 39446 / 1791823346592080。

### L3-3 安全与卫生 — PASS

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| Studio 日志 | PASS。`logs/studio-l3.log` 56 字节，唯一内容行是 `Studio listening on 127.0.0.1:61794`。会话 token 不在日志中，无 `token=`，无 `/api/`，无 `modelscope` | `evidence/l3/security-grep.json` |
| llama-server 日志 | 无会话 token，无 `token=`，无 `/api/`。有启动行 `listening on http://127.0.0.1:18087`，这是绑定地址，不是 Studio 会话 URL | 同上 |
| 停止 llama-server | `SIGTERM`（15）发往 pid 39446。0.24s 后进程不存在，端口 18087 不再监听。没有升级到 SIGKILL | `evidence/l3/stop.json` |
| 停止 Studio | `SIGINT`（2）发往 pid 40639。0.32s 后进程不存在。headless 按中断退出 | 同上 |
| 主机状态恢复 | 停止后把 `host-state.before.json` 拷回 `/var/tmp/inferyard-host.state.json`。sha256 与备份相同 `f5bad268...cb8960`，模式 `0600`。内容回到验收前：`dirty=false`，`run_id=20261012T023430Z-9aaf504e3d744766a8cd0edcbd95d9e8` | `evidence/l3/host-state.restore.json` |
| 产物清单 | `/tmp/studio-t9` 在排除 `buildsrc` 镜像后 221 个文件，清单 `evidence/l3/artifact-manifest.txt`。`buildsrc/` 是 HEAD 构建副本（`node_modules` 为指向仓库的符号链接），未逐文件展开。L2 的 `studio`、`evidence/`、`attempt1/`、`models/` 仍在 | 见第四节 |

没有删除任何既有证据。

## 三、问题与根因

本轮没有判 FAIL 的产品缺陷。下面三条是实测行为，供后续区分。

### 3.1 同进程 dirty 标记没有让 run #2 exit 2 — 真实 CLI 行为

取消后主机文件 `dirty=true`，`kind=run`，`run_id` 指向被取消的 run，pid 与 ticks 仍是当前 llama-server。紧接着用同一 config 再 `POST /api/runs`，Studio 得到 HTTP 202，CLI 退出码 0，没有 `limitations` 里的 recovery 提示。

验收脚本没有预先清理标记，也没有注入 `--recovery-confirm`。因为没有出现任务书里的 exit 2，所以没有执行「按提示恢复后再试一次」。

历史索引没有被这个 dirty 标记改写成 `dirty` 或 `partial`：被取消的 run 在第二次刷新时仍是 `cancelled`。完成后标记变为 `dirty=false`，`run_id` 换成完成的那次。

### 3.2 取消发生在 probe — 线束时机，终态证据成立

驱动在第一次同时看到 `running`、带 `total=40` 的 progress、以及带真实 RSS 的 resource 点时就取消。当时 `phase=probe`，`done=0`，题目仍是 `pending`。终态、`events.jsonl` 的 `user_cancelled`、历史 `cancelled`、Chrome 两页与 dirty 标记同时成立。没有为了「多做几题再取消」而重跑。

### 3.3 1/40 通过 — 冒烟分数，不是 CLI 失败

完整 run 的请求完成率是 40/40，答案通过数是 1/40（`instruction-06` 为 pass，其余 fail）。报告页把这两项分开写。本轮不把低分写成 Studio 或 CLI 失败，也不把它写成模型能力结论。

题包文件实际是 instruction 20 题加 extraction 20 题。任务说明里的「40 题 instruction」与磁盘上的 `zh-smoke.json` 分类不完全相同。Studio 跑的是该文件。

另外：RunDetail 与历史页没有 L2 那种 `internal_error` 横幅，console error 为 0。这两页的 `getDevice` 能过当前前端 schema。本轮没有单独再打开 Device 页做截图，所以不把 Device 页单独写成复测项。

## 四、证据索引

**仓库内**

- 本报告：`docs/reviews/t9-l3-acceptance.md`

**进程与构建**

- `/tmp/studio-t9/studio-l3`
- `/tmp/studio-t9/logs/bun-build-l3.log`
- `/tmp/studio-t9/logs/go-build-l3.log`
- `/tmp/studio-t9/logs/build-head.txt`
- `/tmp/studio-t9/logs/llama-server.log`
- `/tmp/studio-t9/logs/llama-server.pid`
- `/tmp/studio-t9/logs/llama-server.argv`
- `/tmp/studio-t9/logs/studio-l3.log`
- `/tmp/studio-t9/logs/studio-l3.pid`
- `/tmp/studio-t9/logs/l3-cancel-drive.log`
- `/tmp/studio-t9/logs/l3-run2.log`
- `/tmp/studio-t9/configs/zh-smoke-l3.toml`
- `/tmp/studio-t9/configs/reference-decoded.toml`

**驱动**

- `/tmp/studio-t9/l3_harness.py`
- `/tmp/studio-t9/l3_cancel_drive.py`
- `/tmp/studio-t9/l3_run2_drive.py`
- `/tmp/studio-t9/l3_browser.cjs`

**API 与主机状态**

- `/tmp/studio-t9/evidence/l3/settings-put.json`
- `/tmp/studio-t9/evidence/l3/status.json`
- `/tmp/studio-t9/evidence/l3/device-refresh-post.json`
- `/tmp/studio-t9/evidence/l3/device-sse.json`
- `/tmp/studio-t9/evidence/l3/device-get.json`
- `/tmp/studio-t9/evidence/l3/cancel-create.json`
- `/tmp/studio-t9/evidence/l3/cancel-terminal.json`
- `/tmp/studio-t9/evidence/l3/cancel-progress.json`
- `/tmp/studio-t9/evidence/l3/cancel-resource.json`
- `/tmp/studio-t9/evidence/l3/history-cancel-row.json`
- `/tmp/studio-t9/evidence/l3/resume-task.json`
- `/tmp/studio-t9/evidence/l3/resume-history-row.json`
- `/tmp/studio-t9/evidence/l3/host-state.before.json`
- `/tmp/studio-t9/evidence/l3/host-state.after-cancel.json`
- `/tmp/studio-t9/evidence/l3/host-state.after-run2.json`
- `/tmp/studio-t9/evidence/l3/host-state.restore.json`
- `/tmp/studio-t9/evidence/l3/run2-terminal.json`
- `/tmp/studio-t9/evidence/l3/run2-history-row.json`
- `/tmp/studio-t9/evidence/l3/run2-states.json`
- `/tmp/studio-t9/evidence/l3/report-create.json`
- `/tmp/studio-t9/evidence/l3/security-grep.json`
- `/tmp/studio-t9/evidence/l3/stop.json`
- `/tmp/studio-t9/evidence/l3/results-cancel.json`
- `/tmp/studio-t9/evidence/l3/results-run2.json`
- `/tmp/studio-t9/evidence/l3/artifact-manifest.txt`
- `/tmp/studio-t9/evidence/l3/browser-cancel.json`
- `/tmp/studio-t9/evidence/l3/browser-complete.json`

**落盘**

- `/tmp/studio-t9/results/20261012T164646Z-d8cb0a7efc0b43e3a785bde39b479f49`
- `/tmp/studio-t9/results/20261012T165151Z-32b494eeef654ea6a3dbfd165fc2f251`
- `/tmp/studio-t9/reports/report-20261012T165255Z/report.html`

**截图**

- `/tmp/studio-t9/shots/l3/cancel-rundetail.png`
- `/tmp/studio-t9/shots/l3/cancel-history.png`
- `/tmp/studio-t9/shots/l3/complete-rundetail.png`
- `/tmp/studio-t9/shots/l3/complete-history.png`
- `/tmp/studio-t9/shots/l3/complete-report.png`
- `/tmp/studio-t9/evidence/l3/page-cancel-rundetail.txt`
- `/tmp/studio-t9/evidence/l3/page-cancel-history.txt`
- `/tmp/studio-t9/evidence/l3/page-complete-rundetail.txt`
- `/tmp/studio-t9/evidence/l3/page-complete-history.txt`
- `/tmp/studio-t9/evidence/l3/page-complete-report.txt`

## 五、总体结论

**L3 通过。** 这是真实设备上的冒烟，不是测评结论。

通过：L3-0（AC 在线、llama-server 健康、HEAD `3eb5a0b` 的 embed 二进制、CLI 0.0.2、设备快照含已下载的 Qwen 模型），L3-1（probe 阶段取消，任务 `cancelled` / exit 130 / `user_cancelled`，历史 `cancelled`，同时主机文件留下指向该 run 的 dirty 标记），L3-2（同配置第二次 run 未被 exit 2 阻断，63.5s 完成，exit 0，历史 `complete`，RunDetail 评分表 1 pass / 39 fail，报告 HTTP 201 且 Chrome 能打开），L3-3（日志无会话 token，llama-server 以 SIGTERM 停止，主机状态文件已恢复为验收前备份）。

未修改产品代码。低分按冒烟记录，不在本轮当作失败。

## 附：证据路径存在性验证（报告落盘后自检）

自检时间：2026-10-13 00:58（UTC+8）。
自检命令：抽出本报告里以 `/tmp/studio-t9`、`/var/tmp`、`/Users` 开头的绝对路径，加上本报告 `docs/reviews/t9-l3-acceptance.md`，去重后逐一执行 `[ -e <path> ]`。

结果：**70/70 全部存在，0 缺失。** 清单副本 `/tmp/studio-t9/evidence/l3/selfcheck-paths.txt`。

缺失项：无。`git status --short` 仍只有未跟踪 `docs/reviews/`，`git diff --stat` 为空。
