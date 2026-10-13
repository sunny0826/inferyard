# T9-L2 验收报告：真实 CLI + 真实下载

验收人：T9-L2 独立验收员（未参与实现）。
验收时间：2026-10-13 00:02 – 00:15（UTC+8）。
验收对象：仓库 `inferyard-studio`，冻结快照 HEAD `571795b`。
验收期间未修改任何被验收产品文件、未提交 git。仓库内唯一写产物为本报告。
临时产物在 `/tmp/studio-t9/`。Studio 进程使用隔离 `HOME`（`/tmp/studio-t9/home-l2`），未写操作者的真实配置目录。

主仓库 `inferyard` 只读。对其的执行仅通过 wrapper 的 `--version`、`device-check --json`、`model acquire --out /tmp/studio-t9/models/...`。

## 一、验收基线与环境

| 项 | 值 |
| --- | --- |
| HEAD commit | `571795b` fix(web): align run fixtures and schemas with frozen contract |
| 工作树 | 仅未跟踪 `docs/reviews/`（验收开始时已如此；本报告写入该目录） |
| OS | macOS 26.6.2（darwin/arm64，Build 25G83） |
| Go | go1.27.2 darwin/arm64。构建命令级 `GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0`，经 `mise exec`。`go version -m /tmp/studio-t9/studio` 首行为 `go1.27.2` |
| 二进制 | `/tmp/studio-t9/studio`，16M，`go build -o` 退出码 0。模块戳记含 `+dirty`，来自既有未跟踪 `docs/reviews/`，不是产品源码改动 |
| inferyard | wrapper 实测 `inferyard 0.0.2`，退出码 0，耗时 0.32s |
| Chrome | 154.0.8037.98（本机安装，Playwright `channel:'chrome'`） |
| Node | v24.12.0（mise） |
| Python | 3.14.2（线束） |
| mise | 2026.8.2 |
| 应用版本 | `GET /api/status` 的 `version` 为 `0.1.0` |
| 下载 | Studio 子进程环境去掉 `http_proxy` / `https_proxy` / `all_proxy` 及其大写形式。inferyard `model acquire` 的 httpx 客户端为 `trust_env=False`（`model_source_download.py`）。成功文件字节数与给定 blob 一致 |
| 墙钟 | 正式轮驱动 151.3s（含 491MB 下载 136.2s）。取消路径观察到的最大 `.part` 为 2,226,935 B。未对同一 URL 重试。全程远低于 40 分钟 |

冻结契约：`docs/api-contract.md`、`docs/api-types.ts`。L1 报告 `docs/reviews/t9-l1-acceptance.md` 仅作格式与线束模式参考。线束复制到 `/tmp/studio-t9/` 后修改，未改 `/tmp/t9-l1/`。

### 线束

- `harness.py`：隔离 HOME、去掉代理、`-headless -session-fd` 启动真实二进制，REST + 原始 socket SSE。
- `l2_drive.py`：L2-0 至 L2-5 驱动。正式轮日志 `/tmp/studio-t9/logs/l2-drive-run2.log`，逐断言 `/tmp/studio-t9/evidence/results.json`（56 通过 / 2 失败，进程退出码 1）。
- `nav_test.cjs`：本机 Chrome 导航 Dashboard / Device / Models。
- `gui_test.py`：桌面窗口冒烟（加分项）。
- wrapper：`/tmp/studio-t9/bin/inferyard-wrapper`。原稿见 `evidence/wrapper-original.sh`。正式轮在调用 mise 前把 `HOME` 恢复为操作者主目录，原因见第三节（线束问题，不是产品缺陷）。

## 二、逐项结论

### L2-0 准备 — PASS

| 检查 | 命令 | 退出码 | 证据 |
| --- | --- | --- | --- |
| HEAD | `git log --oneline -1` | 0 | `571795b fix(web): align run fixtures and schemas with frozen contract` |
| 工作树 | `git status --short` | 0 | 仅 `?? docs/reviews/` |
| 构建 | `GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 mise exec -- go build -o /tmp/studio-t9/studio .` | 0 | `/tmp/studio-t9/logs/go-build.log`，二进制 `/tmp/studio-t9/studio` |
| wrapper | `/tmp/studio-t9/bin/inferyard-wrapper --version` | 0 | `inferyard 0.0.2` |
| 服务器 | 驱动内 headless 启动 | 0（`server-exit.txt` 为停止码 0） | 正式轮监听 `127.0.0.1:64263`，见 `logs/studio.log` |
| 设置 | `PUT /api/settings` | HTTP 200 | `inferyard_path` = wrapper，`results_root=/tmp/studio-t9/results`，`models_root=/tmp/studio-t9/models`。证据 `evidence/settings-put.json` |

### L2-1 CLI 探测与 device 三态 — PASS

`GET /api/status` HTTP 200：`inferyard.status=ok`，`version=0.0.2`，`path=/tmp/studio-t9/bin/inferyard-wrapper`。证据 `evidence/status.json`。

`POST /api/device/refresh` HTTP 202，`refreshing=true`。SSE `device.updated` 在 1.114s 内到达（事件 id `1791821408212014001`）。`error=null`。

快照（空模型根，刷新当时 `models` 长度为 0）：

- `kind` = `device_preflight.v1`
- `hardware.cpu_model` = `Apple M4`，`logical_cpus` = 10，`physical_cpus` = 10
- `memory_total_bytes` = 17179869184，`memory_available_bytes` = 4816748544
- `recommendation.status` = `blocked`，`reason` = `no_local_model`（本机当时无本地 GGUF，按实测记录，未把该状态写成死断言）

`GET /api/device` HTTP 200：同一 `kind`，`refreshing=false`，`captured_at=2026-10-12T16:10:09.208365Z`，`last_error=null`。证据 `evidence/device-sse.json`、`evidence/device-get.json`。

下载完成并取消之后再次 `POST /api/device/refresh`。SSE 快照的 `models` 变为 1 条，对应已落盘文件：

- path `/private/tmp/studio-t9/models/qwen25-q4km/qwen2.5-0.5b-instruct-q4_k_m.gguf`
- `status=available`，`size_bytes=491400032`，`architecture=qwen2`，`context_length=32768`，`chat_template_available=true`
- `recommendation.status` 变为 `recommended`，`reason` 为 null

取消留下的空目录没有进入 `models`。证据 `evidence/device-get-2.json`、`evidence/device-sse-2.json`。

### L2-2 真实下载成功 — PASS

`POST /api/downloads` HTTP 202。`out_name=qwen25-q4km`。URL 为任务给定的 q4_k_m blob（revision `4537f71740dd2c56a15ffa12e5fbb9f15095f20c`）。一次成功，无重试。墙钟 136.2s，低于 10 分钟。

SSE `download.progress` 134 次。`downloaded_bytes` 从 4,848,384 连续严格递增到 488,504,064（相邻样本全部严格增大）。`bytes_per_second` 在 133 次为数值（第一次窗口不足，为 null）；观测速率约 1.64e6 – 4.10e6 bytes/s。

终态 `done`。`result.bytes=491400032`。`result.sha256=74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db`（64 位十六进制）。

落盘目录 `/private/tmp/studio-t9/models/qwen25-q4km`（`/tmp` 的规范路径，位于 `models_root` 之下）：

| 文件 | 大小 |
| --- | --- |
| `qwen2.5-0.5b-instruct-q4_k_m.gguf` | 491,400,032 |
| `source.json` | 418 |

权重文件名是 blob 文件名，不是字面量 `weights.gguf`。字节数与给定 blob 一致。

`shasum -a 256` 退出码 0，摘要与 `result.sha256`、Python hashlib、`source.json` 的 `sha256` 相同。证据 `evidence/shasum.txt`、`evidence/shasum.exit`。

`source.json`：`definition=remote_model_source.v3`，`revision=4537f71740dd2c56a15ffa12e5fbb9f15095f20c`，`bytes=491400032`，`sha256` 同上。副本 `evidence/source.json`。

### L2-3 真实下载取消 — FAIL

同一次服务器会话，第二个 URL（同 revision 的 `qwen2.5-0.5b-instruct-q2_k.gguf`），`out_name=qwen25-q2k`。

| 子检查 | 结果 | 证据 |
| --- | --- | --- |
| `POST /api/downloads` | PASS，HTTP 202 | `evidence/cancel-create.json` |
| 至少 1 次 `download.progress` 后取消 | PASS。1 次，`downloaded_bytes=2226935`，`bytes_per_second=null`（仅一个采样点） | `evidence/cancel-terminal.json` |
| `POST .../cancel` | PASS，HTTP 202 | `evidence/cancel-post.json` |
| 终态 `cancelled` | PASS。`error=null`，`finished_at` 已填 | 同上 |
| 目录保留 | PASS。`/private/tmp/studio-t9/models/qwen25-q2k` 仍是目录 | `evidence/cancel-files.json` |
| `.part` 保留 | **FAIL**。取消约 1s 后目录为空，`files=[]`，总字节 0。取消瞬间任务对象仍记录 `file_name=qwen2.5-0.5b-instruct-q2_k.gguf.part` 与 2,226,935 bytes | `evidence/cancel-files.json` |
| 取消路径体量 | PASS。观察到的最大 `.part` 为 2,226,935 B，低于 50MB。文件随后被 CLI 删除，没有继续下到 415MB | 同上 |
| 同名再下 | PASS。HTTP 409，`error.code=conflict`，`details.reason=output_exists`，message「输出目录已存在，请更换目录名」 | `evidence/cancel-reuse.json` |

根因见 3.1。空目录仍触发 `output_exists`，与「目录名占用」一致；`.part` 本体没有留下。

### L2-4 真实后端 GUI / 浏览器冒烟 — FAIL

命令：`node /tmp/studio-t9/nav_test.cjs`（仓库根为 cwd，Chrome `channel:'chrome'`，headless，1280×800）。退出码 1。`evidence/nav.exit`、`evidence/nav.stdout`。

| 子检查 | 结果 |
| --- | --- |
| Dashboard 有内容，SSE「事件已连接」 | PASS |
| Device 页渲染真实快照 | **FAIL**。页面停在「正在读取设备信息…」，没有 `cpu_model` / 内存字段 |
| 无 `internal_error` 横幅 | **FAIL**。Dashboard、Device、Models 顶部均为「无法读取服务响应，请重试。/ internal_error」 |
| Models 显示两个任务与状态 | PASS。`qwen25-q4km` 成功 · done，468.64 MiB（491400032 bytes），SHA-256 与实测一致；`qwen25-q2k` 已取消 · cancelled，2.12 MiB（2226935 bytes） |
| console / pageerror | PASS，0 条。Zod 失败被前端接住，没有打到 console。`evidence/console.json` |

截图：

- `/tmp/studio-t9/shots/page-dashboard.png`（86,138 B）
- `/tmp/studio-t9/shots/page-device.png`（38,385 B）
- `/tmp/studio-t9/shots/page-models.png`（140,751 B）

页面文本：`evidence/page-dashboard.txt`、`page-device.txt`、`page-models.txt`。

根因见 3.2。REST 的快照是好的（L2-1），浏览器拒绝渲染它。

桌面 GUI（加分，不单独决定 L2-4）：`python3 /tmp/studio-t9/gui_test.py` 退出码 0。日志有 `desktop page loaded from local HTTP`，两次 DOM 采样 `title=InferYard Studio error=<nil>`，进程 SIGINT 后退出码 0，token 不在 `logs/gui.log`。截图 `/tmp/studio-t9/gui/desktop.png`（185,773 B）是 Dashboard，顶部同样是 `internal_error` 横幅，其余启动表单仍在。与浏览器是同一前端校验失败。

### L2-5 安全复测 — PASS

正式轮停止后对 `logs/studio.log`（320 B，含试跑与正式轮的监听行）检索：无会话 token、无 `token=`、无 `GET `/`POST ` 请求行、无 `modelscope`、无 `/api/`。记录 `evidence/security-grep.txt`（`token_in_log=False`，`url_line_count=0`）。

`logs/gui.log`（367 B）同样没有 token 与请求 URL。

`settings.json` 模式 `0600`，目录 `0700`：

`/tmp/studio-t9/home-l2/Library/Application Support/InferYard Studio/settings.json`

证据 `evidence/settings-stat.txt`。

## 三、问题与根因

### 3.1 取消后 `.part` 不保留 — 产品集成缺口（L2-3 FAIL）

**现象**：SSE 与任务对象在取消瞬间仍给出 `qwen2.5-0.5b-instruct-q2_k.gguf.part` 与 2,226,935 bytes。约 1 秒后该目录存在但为空。同名 `POST` 仍 409 `output_exists`。

**不是线束删除**。驱动在取消后只做 `os.walk`，没有 unlink。

**原因**：Studio 对子进程组发 SIGINT（`internal/inferyard/process_unix.go` 的 `interruptProcess`）。inferyard `model acquire` 在 `BaseException`（含键盘中断）时删除临时文件，不删除目录：

`inferyard/src/inferyard/platforms/model_source_download.py` 第 78–83 行，`except BaseException` 里对 `*.part` 与 `source.json.part` 执行 `unlink`。

Studio 契约写的是失败/取消**目录**保留、不自动删除（`docs/api-contract.md` §4.7）。Studio 没有删目录。真实 CLI 会删 `.part`。L1 的 fake CLI 不删，所以 L1 能看到 `.part`。本项验收要求「目录与 `.part` 保留」，`.part` 这一半不成立。

**复现**：

1. 用真实 wrapper 启动 Studio，`models_root` 指向空目录。
2. `POST /api/downloads`，source 为上述 q2_k blob，`out_name` 使用未占用的单段目录名。
3. 收到一次 `download.progress` 后 `POST /api/downloads/{id}/cancel`。
4. 等任务变为 `cancelled`，列出 `out_dir`。目录在，`.part` 不在。

未改产品代码。

### 3.2 Device 页 `internal_error` 横幅 — 产品缺陷（L2-4 FAIL）

**现象**：Chrome 与桌面窗口都显示「无法读取服务响应，请重试。/ internal_error」。Device 页不渲染硬件。Models 页在横幅下方仍列出下载任务。console 无错误。`GET /api/device` 为 200，快照 `kind` 与硬件字段齐全。

**原因**：用仓库里的 `deviceSchema` 对两份真实响应做 `safeParse`（只读，`evidence/zod-device.json`），两份都只失败在一处：

`snapshot.hardware.gpu.devices.0.index`：`invalid_type`，expected number，received undefined。

真实 `device-check` 的 GPU 设备字段是 `name`、`cores`、`metal_supported`、`memory_kind`、`memory_total_bytes`、`memory_free_bytes`、`missing`，没有 `index`。前端 `web/src/api/schema.ts` 把 `devices[].index` 写成必填 `z.number()`。`web/src/api/client.ts` 在 `deviceSchema.parse` / `deviceEventSchema.parse` 失败时走 `handlers.error(failure(...))`，`failure()` 把非 `ApiFailure` 收成 `internal_error`。`App.svelte` 只在解析成功时写入 `device`，所以页面停在「正在读取设备信息…」。

L1 把 fake 快照补成冻结形状后横幅消失；那份 fake 含有 schema 要求的 `index`。真实 CLI 不发这个字段，L1 覆盖不到。

下载完成后的第二次快照，`models[]` 形状能通过 schema；唯一失败点仍是 GPU `index`。所以横幅不是模型条目造成的。

**复现**：

1. 配置真实 inferyard 与 `models_root`，`POST /api/device/refresh`，确认 `GET /api/device` 200 且 `snapshot.kind=device_preflight.v1`。
2. 用带会话 token 的 URL 打开 `#/device`。
3. 顶部出现 `internal_error`，主体没有硬件字段。对 `device-get.json` 跑 `deviceSchema.safeParse` 得到上述 `index` 问题。

未改产品代码。

### 3.3 线束问题（已在正式轮排除，不是产品缺陷）

第一次驱动（00:08，日志 `logs/l2-drive.log`，副本 `attempt1/`）在 `GET /api/status` 得到 `inferyard.status=version_failed`，随后下载 503 `cli_unavailable`。隔离 `HOME=/tmp/studio-t9/home` 时，mise 不信任操作者主目录里的 `~/.config/mise/config.toml`，wrapper 退出码 1，stdout 为空。stderr 副本 `evidence/wrapper-isolated.err`。

同一 wrapper 在真实 `HOME` 下 `--version` 为 0.32s、退出码 0。Studio 的 `--version` 探测超时是 5s（`internal/core/service.go` 的 `probe`），这次失败不是超时，是 mise 立即拒绝。

正式轮只改了 `/tmp` 里的 wrapper：Studio 仍用隔离 HOME，wrapper 在 `mise exec` 之前把 `HOME` 设回操作者主目录。复测 `--version` 退出码 0。正式轮 L2-1 探测为 `ok` / `0.0.2`。未执行 `mise trust`，未改全局 mise 配置。

试跑的浏览器结果（无下载任务、Device 为 `cli_unavailable`）作废，不计入正式结论。证据保留在 `attempt1/`，没有删除。

### 3.4 观察（不单独判 FAIL）

1. 空的 `results_root` 上，两次 headless 启动的日志都有 `history scan: skipped 1 entries`。结果目录里没有 run。未继续追 `evidence.Index` 为何计 1。
2. 下载进行中的最后一次 progress 是 488,504,064 bytes；完成后 `result.bytes` 为 491,400,032。末次轮询发生在 CLI 把 `.part` 改名为最终 gguf 之前，与约 1Hz 轮询一致。
3. 取消那一次 progress 的 `bytes_per_second` 为 null，因为速率窗口至少要两个采样点。成功路径有 133 个数值速率。
4. `out_dir` 回显为 `/private/tmp/...`，与 L1 记录的 macOS `/tmp` 规范化一致。

## 四、证据索引

**仓库内**

- 本报告：`docs/reviews/t9-l2-acceptance.md`

**被测二进制与 CLI**

- `/tmp/studio-t9/studio`
- `/tmp/studio-t9/bin/inferyard-wrapper`
- `/tmp/studio-t9/evidence/wrapper-original.sh`

**驱动**

- `/tmp/studio-t9/harness.py`
- `/tmp/studio-t9/l2_drive.py`
- `/tmp/studio-t9/nav_test.cjs`
- `/tmp/studio-t9/gui_test.py`

**正式轮日志**

- `/tmp/studio-t9/logs/l2-drive-run2.log`
- `/tmp/studio-t9/logs/studio.log`
- `/tmp/studio-t9/logs/go-build.log`
- `/tmp/studio-t9/logs/gui.log`
- `/tmp/studio-t9/logs/gui-test.log`
- `/tmp/studio-t9/logs/commands.log`
- `/tmp/studio-t9/evidence/results.json`（58 条，56 通过 / 2 失败）
- `/tmp/studio-t9/evidence/server-exit.txt`

**API 证据**

- `/tmp/studio-t9/evidence/status.json`
- `/tmp/studio-t9/evidence/settings-put.json`
- `/tmp/studio-t9/evidence/settings-stat.txt`
- `/tmp/studio-t9/evidence/settings-file.json`
- `/tmp/studio-t9/evidence/device-sse.json`
- `/tmp/studio-t9/evidence/device-get.json`
- `/tmp/studio-t9/evidence/device-sse-2.json`
- `/tmp/studio-t9/evidence/device-get-2.json`
- `/tmp/studio-t9/evidence/download-progress.json`
- `/tmp/studio-t9/evidence/download-terminal.json`
- `/tmp/studio-t9/evidence/download-files.json`
- `/tmp/studio-t9/evidence/source.json`
- `/tmp/studio-t9/evidence/shasum.txt`
- `/tmp/studio-t9/evidence/cancel-terminal.json`
- `/tmp/studio-t9/evidence/cancel-files.json`
- `/tmp/studio-t9/evidence/cancel-reuse.json`
- `/tmp/studio-t9/evidence/zod-device.json`
- `/tmp/studio-t9/evidence/security-grep.txt`

**落盘**

- `/tmp/studio-t9/models/qwen25-q4km/qwen2.5-0.5b-instruct-q4_k_m.gguf`
- `/tmp/studio-t9/models/qwen25-q4km/source.json`
- `/tmp/studio-t9/models/qwen25-q2k`（空目录，取消后保留）
- `/tmp/studio-t9/home-l2/Library/Application Support/InferYard Studio/settings.json`

**截图与浏览器**

- `/tmp/studio-t9/shots/page-dashboard.png`
- `/tmp/studio-t9/shots/page-device.png`
- `/tmp/studio-t9/shots/page-models.png`
- `/tmp/studio-t9/gui/desktop.png`
- `/tmp/studio-t9/evidence/nav.stdout`
- `/tmp/studio-t9/evidence/console.json`
- `/tmp/studio-t9/evidence/page-device.txt`
- `/tmp/studio-t9/evidence/page-models.txt`

**试跑（线束失败，保留）**

- `/tmp/studio-t9/logs/l2-drive.log`
- `/tmp/studio-t9/attempt1/l2-drive.log`
- `/tmp/studio-t9/evidence/wrapper-isolated.err`

## 五、总体结论

**L2 不通过。**

通过：L2-0 准备，L2-1 真实 CLI `0.0.2` 与 `device_preflight.v1`（空目录时 `blocked` / `no_local_model`；下载后 `models` 含该 GGUF，`recommendation.status=recommended`），L2-2 真实下载（491,400,032 B，sha256 与 `shasum -a 256` 和 `source.json` 一致，136.2s），L2-5 日志无 token、无请求 URL，`settings.json` 为 0600。

未通过：

- L2-3：任务进入 `cancelled`，目录保留，同名再下 409 `output_exists`，但 `.part` 被 inferyard 在中断时删除。
- L2-4：Models 页能显示两个真实任务；Device 页不渲染快照。Chrome 与桌面窗口都有 `internal_error` 横幅。前端 schema 要求 GPU `devices[].index`，真实 `device-check` 没有该字段。console 错误数为 0。

未修改产品代码。上述两项按缺陷记录，不在本轮修复。

## 附：证据路径存在性验证（报告落盘后自检）

自检时间：2026-10-13 00:18（UTC+8）。
自检命令：从第四节代码路径中抽出全部绝对路径与 `docs/reviews/t9-l2-acceptance.md`，逐一执行 `[ -e <path> ]`。

结果：**49/49 全部存在，0 缺失。**

- 本报告 1 项：OK
- 被测二进制与 wrapper 3 项：OK
- 驱动 4 项（harness.py / l2_drive.py / nav_test.cjs / gui_test.py）：OK
- 正式轮日志与结果 8 项（l2-drive-run2.log、studio.log、go-build.log、gui.log、gui-test.log、commands.log、results.json、server-exit.txt）：OK
- API 与安全证据 18 项（status、settings、device、download、cancel、zod、security-grep 等）：OK
- 落盘 4 项（gguf、source.json、空的 qwen25-q2k 目录、0600 settings.json）：OK
- 截图与浏览器文本 8 项：OK
- 试跑保留 3 项（l2-drive.log、attempt1/l2-drive.log、wrapper-isolated.err）：OK

缺失项：无。`git status --short` 仍只有未跟踪 `docs/reviews/`，`git diff --stat` 为空。
