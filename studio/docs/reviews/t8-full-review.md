# T8 全量审查报告（冻结关口）

审查：reviewer 角色，2026-10-12，纯审查轮。审查期间未修改任何被审文件、未提交 git；
唯一写操作为本报告。冻结快照 HEAD = `e8cb5b6`（`git log --oneline` 确认；
工作树全程仅未跟踪 `docs/reviews/`，审查收尾 `git status --porcelain` 复核无变化）。

## 一、审查范围与方法

**范围**：`main.go`、`internal/**`（core/server/evidence/inferyard）、`web/**`（Svelte 前端
与 zod schema、mock、浏览器验收线束）、`scripts/`、`docs/`（冻结契约、M0 记录、
既往 T1 审查）、构建配置（`vite.config.ts`、`mygo.config.ts`、`package.json`、
`scripts/build-all.sh`、`scripts/verify-embed.py`）。全部 Go 源码（约 2900 行含测试）与
全部前端源码（约 1900 行）逐行读毕；测试文件同读。

**基准**：冻结契约 `docs/api-contract.md` + `docs/api-types.ts`（v0 冻结 @ 6a07b19 +
Q4/Q5 补丁 @ 3fc44de）；计划文档 `inferyard/docs/plans/studio-app.md`；inferyard 主仓库
（只读事实来源，逐项给出 文件:行号 一级证据）；`docs/reviews/t1-contract-review[-r2].md`。

**方法**：

1. **系统性契约核对**：§4 路由表 18 条逐条对 `internal/server/server.go:130-228` 分派；
   §5.2 事件目录 8 条逐条对 `Bus.Publish` 全部调用点（grep 全仓）；§3 错误模型 8 个 code
   的 HTTP 映射对 `core/service.go:103-105`；7 个 ConflictReason 对全部产生点与前端
   `ErrorNotice.svelte:4-12`。
2. **字段集脚本 diff**（一次性脚本 `/tmp/t8-field-diff.py`，审查后可删）：正则提取
   `internal/core/types.go` 全部 struct 的 `json:"..."` tag 与 `docs/api-types.ts` 全部
   interface 字段，按同名类型逐字段（含顺序）比对。**24 个共享类型零差异**；TS-only
   类型 29 个逐一人工归位（DevicePreflight 透传族——Go 侧 `json.RawMessage` 原样透传属
   契约设计；响应包装族——server.go map 字面量字段名逐一核对；SSE 事件负载——8 处
   Publish 的 map 键逐一核对；`UpdateSettingsRequest`——Go 侧 map + 显式键白名单）。
3. **跨仓库事实核验**：对 Studio 依赖的 inferyard 行为在主仓库取一级证据
   （lock/host_files/runner/trial_runner/trial_lifecycle/trial_summary/journal/storage/
   batch_state/batch_runner/toml_writer/service_binding/report/report_common/sealed_report/
   model_source*/device_preflight/telemetry/sample.schema 等），关键条目 reviewer 本人
   二次复核（取消原因、完整性条件、CLI status 与事件 reason 的区别）。
4. **实证**（命令级 `GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0`；
   bun 用 `--registry=https://registry.npmmirror.com`）：
   - `go vet ./...` 0；`go test ./...` 全过，`-v` 计 `--- PASS` = **48**（与
     internal/server/README.md 声明一致）；`--- SKIP` 仅 TestRealCLI（默认跳过，设计如此）。
   - `go test -race ./...`（CGO_ENABLED=0）通过——README 的 race 声明在本机复现属实。
   - 三平台构建（darwin/arm64、windows/amd64、linux/amd64）均 exit 0。
   - `python3 scripts/verify-embed.py --binary /tmp/t8-darwin`：嵌入首页/资产 200、
     未认证 401、**token 不出现在日志**、进程 exit 0；工作树无副作用。
   - `bun run typecheck`（tsc --strict + svelte-check）0 errors / 0 warnings；
     `bun run build` 生产构建通过（JS 230.32 kB / CSS 5.81 kB）。
   - `bun run test:web`（本机 Chrome，web/tests/verify.mjs）：T3、T5、T6 mock、T5 wire、
     T6 wire、REST/SSE 线束 **6 套件全部 PASS**，无页面异常。
   - `INFERYARD_SMOKE_ROOT=… go test ./internal/inferyard -run TestRealCLI -v`：对真实
     inferyard 0.0.2 执行 `--version` 与 `device-check --json` 两条授权只读命令，
     解析全部通过（version parsed / device_preflight.v1 parsed）。

## 二、分领域结论

### 1. 契约符合性（系统性）——通过（除 B1 所涉条款）

- 18 条 REST 路由全部实现，方法/路径/成功码（200/201/202）与响应包装字段
  （`tasks`/`task`/`report`/`reports_root`/`reports`/`refreshing`）与契约一致；
  未注册路径 404 `not_found`。`PUT /api/settings` 未知键、null/空串/相对路径均 400。
- 8 类 SSE 事件全部实现且帧格式（`id`/`event`/单行 `data`）正确；M0 `count` 事件
  全仓无残留。事件语义（`.state` 全量替换、`run.progress` 全量替换、
  `run.resources`/`download.progress` 增量）与发布点节流（progress ≤2Hz、
  resources ≈1Hz、心跳 15s、回放缓冲 2048、启动种子 id）与 §5.1/§5.2 一致；
  `sync.reset` 数据恰为 `{"reason":"buffer_gap"}`。
- 错误模型：8 code × HTTP 状态映射逐一对齐 §3.2；`message` 为固定文案不回显
  stderr/异常；`limitations` 经 `inferyard.Parse` 从 stdout JSON 原样透传；
  409 细分 7 个 reason 均有产生点且前端有对应文案。
- Go struct ↔ `docs/api-types.ts`：24 个共享类型字段集 diff **零差异**（方法见上）；
  `web/src/api/types.ts` 与 `docs/api-types.ts` 逐字节一致（`diff` 验证）。
- zod schema 与契约类型抽查（三个 schema 文件全读）：错误/设置/设备/历史/任务/进度/
  资源/报告/下载 schema 字段与枚举逐一对齐；`out_name` 前端正则与契约逐字符一致
  （`assets-schema.ts:4`），服务端等价实现（`downloads.go:20,64`）。两处前端 schema
  严于契约的隐患见 S4。

### 2. Q4/Q5 补丁复核（3fc44de）——通过

- `git show 3fc44de --stat`：仅 `docs/api-contract.md` 单文件（+18/-8）；
  `git diff 6a07b19..3fc44de -- docs/api-types.ts` 为空——「无字段/语义变更」属实。
  （区间 diff 中 `web/src/api/types.ts` 的变化来自 T5/T6 的镜像同步，非本补丁；
  终态镜像与冻结版逐字节一致。）
- **Q4 dirty 判据措辞 vs `internal/evidence/dirty.go` 逐项一致**：
  `schema_version=1`/`dirty=true`/`kind="run"`/`run_id` 匹配 → dirty（dirty.go:28-31）；
  文件缺失 → 不 dirty（:13-15）；不可读/JSON 非法/格式非法 → error，经
  `aggregate.go:253-257` 映射 `internal_error`，不推断 clean；clean/后续任务替换后不再
  dirty——主仓库 `lock.py:129-131` clean 显式写 `dirty:false` 保留旧 run_id、
  `lock.py:114-127` dirty 全量覆写单 run_id 标记，语义吻合；「Unix `/var/tmp`」
  （host_unix.go:5）与「Windows 系统 Common AppData」（host_windows.go:13，CSIDL 0x23）
  均与主仓库 `lock.py:24-28`、`windows_host_paths.py:8-18` 一致；
  `RunStatus` 中 cancelled 先于 dirty（dirty.go:36-41）——但 cancelled 判据本身失效，
  见 **B1**。
- **Q5**：§5.1「初始值经 T4 实测复核维持不变」与 §9 记录收尾一致；
  `internal/server/README.md` Q5 数据（500.136ms 最小 progress 间隔、15.001922s 空闲
  心跳、10000→2048 保留/2047 重放、越界 sync.reset）与
  `TestFixtureRunSSE`/`TestHeartbeatReplayAndReset`/`TestEventReplayLoad` 三测试的
  断言一一对应，本轮全量测试复跑通过。

### 3. 安全面——通过

- token：`server.go:36-41` 每进程 crypto/rand 32 字节 hex（64 字符），不持久化；
  `:83` `subtle.ConstantTimeCompare`；server_test.go:42 验证缺/错/裸 token 401、
  Bearer 200、`/api/status?token=` 401（query 不越界到 REST）。
- query token 白名单：`server.go:80` 仅 `/api/events`、`/`、`/index.html`、`/reports/**`；
  `/assets/**` 公开对齐契约与 M0。全部响应 `Referrer-Policy: no-referrer`（:87）。
- 日志：全仓 grep——生产代码无任何请求级日志；`main.go:42` 只记监听地址（无 token/URL）；
  token 只经 `-session-fd` 私有管道传出；verify-embed 实证「token 不出现在日志」。
- 绑定：`server.go:43-49` 仅接受字面 `127.0.0.1`（0.0.0.0/LAN/[::]/localhost 均拒绝，
  有测试）；`main.go:35` 调用点硬编码，无 LAN 开关。
- 报告静态 containment：`reports.go:164-193` 逐段拒空/`.`/`..`/`\`/NUL +
  `Resolve` 双重（报告目录内 + reports_root 内）+ EvalSymlinks 解析 + 必须常规文件；
  越界/形态非法 400、不存在 404（server_test.go:230 覆盖 `..` 用例）；
  core_test.go:203-223 symlink 逃逸用例通过。
- `out_name`：`downloads.go:20,64` 正则 + 纯点分段拒绝（等价契约负向先行断言）+
  Resolve 二次 containment；目录已存在（含失败保留目录与在途任务）→ 409 output_exists。
- 设置文件：`service.go:186-205` 目录 MkdirAll 0700 + CreateTemp(0600) + Rename 原子替换；
  已存在目录权限不矫正见 S8。
- 下载源 URL：`downloads.go:23-62` https + 两 host 白名单 + 40 hex revision + `.gguf` +
  逐段解码校验（禁 query/fragment/userinfo/控制字符/dot 段），`?token=` 掺入用例有测试。

### 4. 证据完整性——通过

- 生产代码对证据目录**零写入**（grep 全部写调用：仅 settings 临时文件+Rename、
  `reports_root`/`models_root` 的 MkdirAll；其余写调用均在测试临时目录）。
- 不覆盖原始 run/报告：报告写入新建的 `report-YYYYMMDDTHHMMSSZ[-N]` 唯一目录
  （reports.go:130-141，reportMu 串行）；下载失败/取消目录保留无删除路径
  （downloads.go 全文无 Remove）；evidence 包注释与实现均为只读（Lstat/ReadDir/ReadFile）。
- 事件 tail 游标只提交完整校验行（tail.go:31-85），截断检测 ErrTruncated；
  不读可缺失的 summary.json（grep 证实全仓无引用，契约 §4.4 落实）。

### 5. 前端正确性——通过（两处隐患见 S4，一处退化见 S1）

- null 缺测渲染：RunDetail 的 `total`/`phase`/质量/耗时/类别均 `?? '未知/缺测'` 不补 0；
  Models 速率 null →「未知」且明示「总量未知」；ResourceChart `spanGaps:false` +
  「缺测不补零」；RunHistory score null→「证据不足」、rate null→「无有效题」不显示 0%。
- 状态优先级：`runs/model.ts:5-8` cancelled>dirty>partial>complete 聚合正确（服务端
  cancelled 判据失效属 B1，前端逻辑本身正确）。
- standalone resume 禁用：RunHistory.svelte:14 `batch_id === null` 即禁用并提示，
  即使服务端异常下发 `resumable=true`（README 声明属实）；409 not_resumable 后禁用来源。
- SSE `sync.reset` → App `rebuild()` 全量 REST 重建（device/settings/history/tasks/
  detail/reports/downloads），版本计数器防过期响应，重建窗口期事件先缓冲后重放
  （store.svelte.ts:18-49）；原生 EventSource 持有 Last-Event-ID。
- 报告打开：`target="_blank" rel="noopener noreferrer"` 顶层导航，无 iframe；
  180s 客户端超时 ≥ 契约 120s 下限。

### 6. README 声明 vs 现实——基本一致（两处过期见 S5/S6）

- internal/server/README.md：路由/事件映射表与代码一致；48 passed、race、三平台
  build、Q5 数据、嵌入验证均**本轮复现属实**；仅嵌入资产尺寸为 T4 时点旧值（S6）。
- internal/core/README.md：原子 settings、双根校验、resume 计划序父链、CPU 差分判据、
  2Hz/1Hz/600 点、Q4 判据与引用（lock.py/batch_state/batch_runner/trial_summary）
  全部核实属实；「selection.payload.case_ids 含 resume 子集」经 journal.py:85 证实。
- web/README.md：types.ts 未修改复制属实；typecheck/build/浏览器线束声明本轮复现；
  「standalone resume 即使服务端误发 resumable=true 也禁用」属实。
- 根 README 为 M0 时代内容，headless 取 token 指引与现行安全模型冲突（S5）。

### 7. 代码健康——良好

- 规模：全部 Go 文件 ≤437 行、Svelte/TS 逻辑文件 ≤537 行中仅 `web/src/api/types.ts`
  537 行超限——该文件是冻结契约的逐字节镜像（必须与 `docs/api-types.ts` 保持一致），
  属受控例外，不计缺陷。
- 并发：锁顺序一致（s.mu → bus.mu，无反转）；SSE 连接 `defer unsubscribe()` +
  timer 清理 + 5s 写超时探死；runQueue 单槽串行；取消竞态（dequeue 与 cancel 交错）
  由 `executeRun` 启动段显式处理；`Subscribe` 先于 replay 注册避免漏事件。
- 子进程：进程组 SIGINT → 2s 宽限 → SIGKILL，`cmd.Wait` 回收无僵尸；
  取消与已完成竞争取已完成优先（client.go:117-119）。
- 错误处理：无吞没（`Publish` 返回值唯一忽略处 runs.go:203 仅可能 marshal 失败，
  发布值恒可序列化）；exit 映射 0/2/3/4/130 全覆盖且有测试。
- 记录备查：仅测试引用的 API 面（S11）；手写 config TOML 解析的受控假设（S12）。

## 三、发现清单

### Blocking

**B1 — 历史 `cancelled` 状态对真实证据系统性失效（违反冻结契约 §4.4/§3.3 呈现语义）**

- 位置：`internal/evidence/dirty.go:36-38`（`RunStatus`）；派生链
  `internal/core/aggregate.go:183-203`。
- 证据：
  - Studio 判 cancelled 仅认 `stopReason == "interrupted" || == "cancelled"`（dirty.go:36）。
  - 主仓库 run_stopped 事件的取消原因实为 **`"user_cancelled"`**：
    `src/inferyard/runtime/trial_runner.py:308` 取消分支 `(130, "user_cancelled")` →
    `src/inferyard/runtime/trial_lifecycle.py:137` 写入事件 `{"reason": reason}`。
    reviewer 已二次复核：grep 主仓库 runtime/evidence，`"interrupted"` 仅出现于
    CLI CommandResult.status（runner.py:221，任务级映射，Studio 任务侧按退出码使用正确）
    与 duration_closed 原因（duration_driver.py:32，另一事件类型），均非 run_stopped。
  - Studio 全仓 grep 无 `"user_cancelled"`；历史扫描纯证据驱动，不接触退出码。
- 影响：任何被取消 run 的历史 `status` 永远落到 `partial`（或 `dirty`），**永不呈现
  `cancelled`**；契约 §4.4 固定优先级「cancelled ＞ dirty ＞ partial ＞ complete」
  系统性失效；若 cancelled+dirty 并存，呈现 dirty 而非 cancelled，优先级结果亦错。
  §3.3「130 → 历史状态 cancelled，不报错」无法满足。
- 测试缺口（漏检根因）：core 无 `RunStatus` 单测；前端 T5「cancelled history」断言跑在
  mock 上，而 mock 既用 `'interrupted'`（`web/src/api/mock-runs.ts:10,48`）又用退出码
  推历史状态（:37）——两处均与真实服务端/证据行为不同，恰好掩盖本缺陷。
- 修复方向：`RunStatus` 取消集合补 `"user_cancelled"`；新增 `RunStatus` 单测覆盖
  user_cancelled→cancelled、cancelled+dirty→cancelled、dirty→dirty、未封存→partial；
  mock 的 stop_reason 同步改为真实值以防再次掩盖。

### Suggestion

**S1 — 资源采样点未按「UI 就绪」聚合；前端同 utc 覆盖丢字段，真实证据下曲线退化为散点**
（`internal/core/aggregate.go:131-174`；`web/src/runs/model.ts:37-41`；
`web/src/components/ResourceChart.svelte:14`）。真实 memory.jsonl 每行单 metric
（telemetry.py:51-97），服务端逐行发点 → 四点中三字段恒 null；前端以 utc 字符串为键
**整体替换**，同 utc 不同 metric 互相覆盖；不同 utc 时四 series 各自稀疏，
`spanGaps:false` 下无连线只剩孤点。契约 §4.5「服务端聚合为 UI 就绪点」意图未达
（类型层面逐项可 null，无 wire 违规，故不定 blocking）。mock 发聚合点
（mock-runs.ts:85）再次与真实服务端行为不一致，线束无法暴露。建议服务端按采样周期
合流为单点，或前端按时间窗做字段级合并。

**S2 — `DirtyRun` 对 `schema_version != 1` 一律报错，历史端点跨版本脆弱**
（`internal/evidence/dirty.go:28-30` → `aggregate.go:253-257`）。符合契约字面
（「格式非法返回 internal_error」），与主仓库当前 LOCK_FORMAT_VERSION=1（lock.py:31）
一致；但主仓库未来升级版本会令 `GET /api/runs` 整体 500。建议 `schema_version > 1`
按「无可匹配标记」处理（仅否认已知 v1 之外语义，不把未知判为故障）。

**S3 — 单个损坏 run 令 `GET /api/runs` 整体 500；index.go 注释与行为不符**
（`internal/evidence/index.go:38-40` 注释自称「counted, not fatal」，实际
`index.Error` 经 `aggregate.go:241-243,253-257` 升级 internal_error；截断的
events.jsonl 同路径）。契约 §3.2 允许 500，但 per-run 降级（跳过并计数或该 run
标错误占位）更符合「首页可用」目标；至少修正注释。

**S4 — 前端 zod 两处严于契约，存在丢事件隐患**
（`web/src/api/run-schema.ts:40` `run_id: z.string()` vs 契约 `string | null`
（api-types.ts:488），当前服务端无触发路径（runs.go:308-320 先侦测后发布）；
`run-schema.ts:35` `utc: z.string().datetime({offset:true})` 遇缺 utc 行则该
run.resources 事件整体 parse 失败被丢弃）。建议与契约镜像对齐放宽。

**S5 — 根 `README.md` 为 M0 时代内容，headless 取 token 指引已失效**（仍写「SSE 计数」、
「打开日志中的 STUDIO_URL」——token 现永不落日志，无 `-session-fd` 的 headless 无法
获得带 token 的 URL；「`Authorization: <token>`」未更新为 Bearer，裸 token 实测 401）。
建议 T9/T10 前刷新。

**S6 — `internal/server/README.md:81` 嵌入资产尺寸过期**（142953/2929 bytes 为 T4 时点；
本轮实测 230328/5813）。其余验证声明复现属实。

**S7 — 版本号双写无单一来源**（`internal/core/service.go:228` `"0.1.0"` 与
`mygo.config.ts:6`）。发版漂移风险。

**S8 — settings 目录已存在时权限不矫正**（`internal/core/service.go:186`
`MkdirAll(...,0700)` 仅对新目录生效；文件本体经 CreateTemp 恒 0600，无问题）。
建议对现存目录补 chmod 或文档声明。

**S9 — 损坏 settings.json 直接令进程启动失败**（`internal/core/service.go:57-64` →
main `log.Fatal`）。建议降级为默认设置 + 服务端日志提示。

**S10 — 契约 §4.6 一处理由性表述与主仓库事实不符（契约勘误建议，无行为冲突）**：
「CLI --out 要求父目录已存在」——主仓库 `reporting/report_common.py:83` 自动创建父
目录（`--out` 本体必须不存在，已存在退 4）。Studio 预创建 + 唯一名循环 +
reportMu 串行（reports.go:124-141）行为正确且契约一致；下轮契约维护时勘误理由句即可。

**S11 — 仅测试引用的 API 面（死代码观察）**：`evidence.PollDownload`（生产未用，
core/downloads.go 自行轮询 .part）、`inferyard.Locate`、`inferyard.Client.DeviceCheck`
（生产未用，service.go 自行 probe/exec）。均有测试且构成 T2 集成 API 面，可留可删；
保留建议包注释说明定位。

**S12 — 手写 config TOML 解析为受控脆弱点（记录备查）**
（`internal/core/runs.go:28-88`）：假设「标量单行、JSON 编码值」已与主仓库
`config/toml_writer.py:20-30` 现状核实一致（字符串 JSON 双引号、数字/布尔裸写、
dict 成 `[a.b]` 子表、同 key 不重复；生成产物路径为绝对，相对值双方均按 config 目录
解析，loader.py:153-155 一致）；writer 形态未来变化（引号 section、多行数组）将使
admission 全部 invalid_request。属已知受控假设，建议在计划文档记录为演进观察点。

## 四、总体结论

**需修复后过 T9。** 契约符合性、Q4/Q5 补丁、安全面、证据完整性、前端行为、README
与代码健康七个领域均通过系统性核对与实证复跑；唯一 blocking 为 **B1**——真实证据的
取消原因 `user_cancelled` 未纳入 `RunStatus` 判据，冻结契约 §4.4 的
cancelled 优先级对真实数据 100% 失效。修复面小（一个判据集合 + 单测 + mock 同步），
但属冻结契约呈现语义的正确性缺陷，必须在 T9 分级验收前修复并复核；
S1–S12 不阻塞 T9，建议随修复轮一并评估（S1、S5 建议在 T10 打包前处理）。
修复后 reviewer 仅需对 diff 做增量复核即可放行 T9。
