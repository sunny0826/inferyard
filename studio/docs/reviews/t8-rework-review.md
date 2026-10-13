# T8 返工增量复核报告

复核：reviewer 角色，2026-10-12，纯审查轮。审查期间未修改任何被审文件、未提交 git；
唯一写操作为本报告。复核区间 `git diff e8cb5b6 571795b`（后端 `36786f5` + 前端
`571795b`，19 文件，+714/-51）。收尾 `git status --porcelain` 仅未跟踪
`docs/reviews/`（本轮实证产生的 `build/` 产物均在 .gitignore 内）。

基准：`docs/reviews/t8-full-review.md` 的 B1/S1–S9 原文；冻结契约
`docs/api-contract.md`（含 Q4/Q5 补丁）；主仓库
`/Users/guoxudong/codes/local-ai/inferyard`（只读，逐项给出 文件:行号 一级证据）。
两份返工自述（`internal/t8-rework-report.md`、`web/tests/t8-rework-report.md`）
仅作声明清单，结论一律以代码与本轮实测为准。

## 一、逐项核对表

### B1 — cancelled 判据（放行关键）：**落实**

- `internal/evidence/dirty.go:39`：`RunStatus` 的 cancelled 判据恰好为
  `stopReason == "user_cancelled"`，伪造值 `interrupted`/`cancelled` 已清除
  （全文件通读，无残留；优先级 cancelled > dirty > partial > complete 保持）。
- 主仓库一级证据（reviewer 亲自复核）：
  - 取消分支唯一映射 `(130, "user_cancelled")`：
    `src/inferyard/runtime/trial_runner.py:303-308`（CancelledError 三分支：
    safety_reason / deadline.reason / user_cancelled，无其他取消值）。
  - reason 写入事件：`runtime/trial_lifecycle.py:136-137`（`seal_stopped` →
    `run_stopped` 的 `{"reason": reason}`）。
  - `"interrupted"` 是 CLI CommandResult.status（`runtime/runner.py:218-225`，
    退出 130 的 CLI 状态字符串），确非事件 reason——前端 mock 保留它是对的。
- `dirty_test.go`（新增 69 行）用真实 reason 覆盖：
  - `user_cancelled` × dirty∈{false,true} 均 → cancelled（优先于 dirty，双测）。
  - 7 个非取消真实原因逐一断言不误判 cancelled（partial；dirty 时落 dirty）：
    `duration_window_interrupted`（trial_lifecycle.py:112）、
    `single_wall_budget_exhausted`/`trial_wall_budget_exhausted`
    （trial_profile.py:32）、`temperature_safety_threshold_reached`
    （safety.py:109）、`diagnostic_run_not_formal`（trial_runner.py:299）、
    `check_only_no_formal_cases`（trial_runner.py:301）、`tool_interrupted`
    （trial_finalization.py:123）。七个值 reviewer 逐一在主仓库定位属实。
  - 无 reason 派生：未封存/未 complete → partial；sealed+complete → complete；
    complete+dirty → dirty。
- HTTP 级闭环：`server/rework_test.go:46-91` 构造 `user_cancelled` 事件 +
  dirty 主机标记的 run，`GET /api/runs` 呈现 `cancelled`（优先级端到端成立）。
- 派生链调用侧语义不变：`aggregate.go:239` 的 `RunStatus(...)` 调用形状与
  `runs.go:399-408` 的 exit StopReason 取值（a.stop → details.stop_reason →
  last_stop_reason）均未改动（diff 核对）。

### S1 — 资源聚合：**落实**

- 半秒锚窗：`aggregate.go:156-162`，以窗内首条 metric 的 utc 锚定，500ms 窗口；
  同 metric 重复出现开启新周期（`a.resourceMetrics[metric]` 守卫）；clock_id/phase
  变化、utc 回退、utc 解析失败同样开新窗。跨 Tail 读取的合并状态保存在
  aggregate 结构体（`resource`/`resourceStart`/`resourceClock`/`resourcePhase`/
  `resourceMetrics`），注释声称的「including across reads」由测试证实。
- 跨读取合并：`appendResources`（aggregate.go:201-210）按末点 utc 替换修订；
  REST 缓存（`runs.go:352`）与 SSE 待发（`runs.go:356`）均经它合流。
- cpu ticks 差分守卫不变：同 source/同 clock/同 boot/同 pid/同 start_ticks/同
  tick 频率、hz>0、0<Δt≤2s、计数单调、前点非缺测（aggregate.go:180-191）；
  `capture != "periodic"` 提前过滤（:150-152）与原实现净效果一致（原本也不
  更新 `a.previous`）。与主仓库自身报表管线口径一致
  （`reporting/report_resources.py:19` 同样只认 periodic）。
- 缺测不补 0：`missing_reason` 非空即 v=nil（:144-146），mem/rss 仅 v≠nil 才
  赋值，temperature 直接赋 v（可 nil）；新周期从空 ResourceSample 重新开始，
  不向新周期携带旧值（测试 `missing fields carried forward` 断言）。
- 与契约 §4.5 一致性：`ResourceSample` 五字段形状未变（逐字段可 null 不变），
  「服务端聚合为 UI 就绪点」从声明变为实际；前端 `appendResources`
  （`web/src/runs/model.ts:36-40`）本就以 utc 为键整体替换——服务端每份修订
  现为完整点，S1 的覆盖丢字段与散点退化消除。
- 真实证据形状核验：telemetry.py:51-97（mem/rss 同周期相继写入）、
  resources_macos.py:185-197（collect 顺序 mem→rss→cpu→temp）、
  journal.py:235-248（utc 在 journal 写入时逐行生成，同周期行间 µs–ms 级）、
  schemas_config.py:197（`interval_ms` 默认 500）——窗内合并 + 重复 metric
  守卫在默认 500ms 周期边界抖动下仍正确（早于 500ms 的下周期的首条 mem 会因
  重复 metric 开新窗）。
- 定向测试：`TestResourceSamplingWindowMerge`（8 行 4 metric × 2 周期合成 2 点；
  逐行喂入的跨读取结果与整批 DeepEqual；缺测不继承）、
  `TestResourceSamplingWindowBoundaries`（时间间隔、重复 metric 开新点；
  同 utc 两条不同 metric 合并为 1 点）、更新的 `TestResourceCPUIdentityAndMissing`
  （null/50%/pid 变化 → null）。本轮全部复跑通过。
- 语义说明（非违规）：SSE `run.resources` 现在会对同一锚点 utc 发送修订点
  （upsert），与 §5.2「增量追加」的字面略有出入，但正是基线 S1 建议的方向
  （服务端合流为单点），前端按键合并，wire 测试断言采样点数稳定。

### S2 — schema_version > 1：**落实**（含一处经基线认可的解释）

- `dirty.go:28-30`：`Schema > 1` → 不匹配、不报错；`< 1`/缺失/非法版本/非法
  v1 字段仍 error。`TestDirtyRunSchemaVersions` 12 例全覆盖通过。
- 契约 Q4 字面「格式非法返回 internal_error」对「更新版本文件」的解读被有意
  放宽为「无可匹配标记」——即基线审查 S2 的建议原文；`scan()` 前置
  `DirtyRun(host, "")`（aggregate.go:294-296）仍保证 v1 非法标记 → 500，已知
  语义不回退。

### S3 — 损坏 run 降级：**落实**

- `index.go`：损坏 run/batch 条目 → `Skipped++` 不再置 `Error`；仅遍历失败
  （Lstat/ReadDir 非不存在错误）置 `Error` → 500。注释已修正为与行为一致。
- `aggregate.go:297-308`：summarize 失败（含截断 events.jsonl）跳过并计数，
  日志报总数（`history scan: skipped N entries`，不含敏感内容）。
- 响应形状未变：`server/rework_test.go:96-102` 断言顶层恰为
  `results_root`/`scanned_at`/`batches`/`standalone_runs` 四键，契约未加字段。
- resume 排除损坏项：`bs.Runs` 与 `validRuns` 同循环成对追加、索引对齐，
  `markResumable(bs.Runs, validRuns)`（aggregate.go:309-318）——损坏项的
  inputs 不再参与计划序/父链判断，构造上排除错位。
- HTTP 实证：`TestHistorySkipsCorruptRuns` 200；standalone 3 坏 1 好 + 取消 run、
  batch 2 坏 1 好；跳过计数 4 写入日志；取消+dirty run 呈现 cancelled。

### S8 / S9 — 设置健壮性：**落实**

- S8：`service.go:191-196` MkdirAll 0700 后对已存在目录补 `Chmod 0700`；
  文件本体仍 CreateTemp 0600 + Rename。`TestSettingsExistingDirectoryPermissions`
  （0755 → 0700、文件 0600，POSIX，Windows 跳过）通过。
- S9：`service.go:63-66` 损坏 JSON/字段类型错误 → 全量默认值 + 日志提示
  （不回显原文，测试断言），启动不再中断；启动不覆写原文件，后续保存可恢复。
  `TestCorruptSettingsUseDefaults` 三例（`{`、部分字段加类型错误、`[]`）通过。

### S5 / S6 / S7 — README 与版本：**落实，但 S6 留一处新过期（见三.1）**

- 根 README 重写为现行内容：构建/运行命令、`-session-fd` 私密管道（附可跑
  python 示例，机制经本轮等价线束实测）、Bearer + query token 范围、损坏设置
  降级与历史跳过日志说明。M0 时代的 STUDIO_URL/SSE 计数/裸 token 指引已清除
  （grep 无残留）。抽查命令全部真实可跑（见二）。
- server README 嵌入尺寸更新为 230232/5813/439：本轮经文件系统、HTTP 实取与
  verify-embed.py 三路实测逐字节吻合。
- S7：`mygo.config.ts:6` 为被跟踪树中唯一 `0.1.0`（另一处命中在 gitignore 的
  `.mygo/template-proof/` 产物）；`main.go:23-31` go:embed + 正则恰好一个
  version 匹配（多/少均 log.Fatal，失败关闭），启动前设置 `core.Version`；
  `service.go:236` Status 使用该变量，core 包单独使用默认 `dev`。
  普通 `go build` 嵌入生效（本轮实测）；运行时 `/api/status` 返回 0.1.0 与
  mygo.config.ts 一致（本轮 headless 实测）。无测试硬编码旧版本号。

### 前端（mock 与 schema）：**落实**

- `mock-runs.ts` 全部 stop_reason 为真实值：`user_cancelled`（130）、
  `duration_window_interrupted`（3）、`tool_interrupted`（4）、null（0/2）；
  history 摘要同步（cancelled→user_cancelled、dirty/partial→
  duration_window_interrupted）。`cli_status` 的 `interrupted`（130）与
  `duration_requires_new_window`（3，batch CLI status，batch_runner.py:131/148）
  正确保留——CLI status 而非事件 reason，未误判。limitations 语义未动。
  全 web/ 扫描无伪造 stop_reason 残留。
- `run-schema.ts` 与冻结镜像逐字段一致（不严不松）：
  `ResourceSample.utc` = `z.string()`（镜像 `string`，wire 测试断言空串/普通
  字符串过、null/省略拒）；`RunProgressEvent.run_id` = nullable（镜像
  `string | null`，真实触发路径 runs.go:339 发布 nil RunId）；
  `done`/`last_seq`/`total` 去除 int/nonnegative（镜像 `number`/`number|null`）；
  新增 `z.ZodType<RunProgressEvent>` 编译期绑定。`web/src/api/types.ts` 与
  `docs/api-types.ts` 本轮 `diff` 仍逐字节一致；typecheck 0 errors/0 warnings。

## 二、实证复跑记录（本轮亲跑，命令级环境前缀同基线）

| 命令 | 结果 |
| --- | --- |
| `go vet ./...` | exit 0，无诊断 |
| `go test ./... -count=1 -v` | **79 `--- PASS`（含子测试）/ 0 FAIL / 1 SKIP**（TestRealCLI，设计跳过）；与基线 48 的差值 31 恰等于本轮新增测试的 PASS 行数（dirty_test 21 + core rework 9 + server rework 1） |
| `go test -race ./... -count=1` | 4 包全 ok，无 race（CGO_ENABLED=0） |
| 定向测试（基线报告 T 命令原样） | 全部 PASS（含 7 个非取消 reason 子测试、12 个 schema 子测试、窗口合并/边界、损坏跳过 HTTP 测试） |
| `bun run typecheck` | 0 errors / 0 warnings |
| `bun run build:web` | 通过；产物 JS 230232 / CSS 5813 / HTML 439 字节 |
| `bun run web/tests/verify.mjs` 第 1 轮 | **6/6 PASS**，含新增断言（nullable run_id、S4 schema parity、真实取消/时窗/预算 reason 呈现、partial/dirty 不显示 cancelled） |
| `bun run web/tests/verify.mjs` 第 2 轮 | **6/6 PASS**（连续两轮全过） |
| `go build`（darwin 原生）+ headless 匿名管道线束 | `/api/status` version=0.1.0 == mygo.config.ts；入口 200、资产 200（230232/5813）、未认证 401、token 不落日志、SIGINT exit 0 |
| `GOOS=windows` / `GOOS=linux` 构建 | 均 exit 0 |
| `python3 scripts/verify-embed.py --binary build/inferyard-studio` | 全项通过；web/dist 自动恢复；工作树无副作用 |
| `git diff --check e8cb5b6 571795b` | 无空白错误 |

## 三、副作用扫描与新发现

**副作用扫描：干净。** 19 个变更文件全部落在 B1/S1–S9 声明范围内；冻结契约
`docs/api-contract.md`、`docs/api-types.ts`、前端镜像 `web/src/api/types.ts`
零改动；go.mod/go.sum、package.json/bun.lock 未动，无新依赖。两处有意行为
变化（均复核为合理）：(a) 主机标记非法时即使无 run 也 500（scan 前置检查，
保持「非法 → internal_error」不随 per-run 降级沉默）；(b) SSE run.resources
发送同 utc 修订点（见 S1 语义说明）。

**新发现（均为 suggestion 级，不阻塞 T9）：**

1. `internal/server/README.md:72-73` 测试计数过期：仍写 `48 passed`（test 与
   race 两行），本轮实测 79。S6 更新了嵌入尺寸但漏了随本轮新增测试漂移的计数。
2. `internal/core/README.md:45-46`「index corruption and unreadable host state
   return a fixed internal error」被 S3 部分推翻：条目级损坏现为跳过+计数
   （200），仅遍历失败与主机状态不可读仍 internal_error。返工改了 index.go
   注释但未同步此文件。
3. （既存观察，非本轮引入）mock `cli_status` 退出 0 为 `'complete'`，真实 CLI
   为 `'finished'`（runner.py:218、batch_runner.py:148）；本 diff 未触碰该段
   映射，列此备查，不影响本轮结论。

## 四、总体结论

**放行 T9。** B1（放行关键）已按真实证据修复并闭环：cancelled 判据恰好为
`user_cancelled`，伪造值清除，真实 reason 全集的定向测试与 HTTP 级
「cancelled 优先于 dirty」用例齐备，派生链调用侧语义不变，前端 mock/schema
与冻结镜像逐字段对齐。S1–S9 各项实现与测试均如声明且经本轮亲跑复现（含
race、两轮浏览器套件、嵌入与版本运行时实测）。副作用扫描干净。三项新发现
均为文档计数/措辞同步与既存 mock 观察，不阻塞 T9，建议随下一轮文档刷新
一并处理。
