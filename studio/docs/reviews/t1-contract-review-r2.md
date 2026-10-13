# T1 契约冻结复核报告（第二轮）

复核：reviewer 角色，2026-10-12。
复核对象：`git diff fbfab85 4417efb -- docs/api-contract.md docs/api-types.ts`
（提交 `4417efb`「docs: revise API contract per T1 freeze review」，仅含上述两文件，
+151/-75 行；diff 全文 482 行已逐行读毕）。
复核基准：第一轮报告 `docs/reviews/t1-contract-review.md`（B1–B6、S1–S17、Q1–Q6）。
事实抽检：主仓库 inferyard 源码（batch_runner.py、runner.py、cli/request.py、
evidence/journal.py、reporting/report.py）关键行二次确认，详见各条。
行号引用：无特殊说明时为 `4417efb` 工作树终态行号。

## 一、结论

**可冻结。** B1–B6 全部真实落实且与 inferyard 事实一致；S1–S17 全部吸收（含等价措辞）；
Q1–Q6 裁决均写入正文落点，§9 已改为裁决记录留档；未发现超范围改动或新引入的
md/TS 不一致；`docs/api-types.ts` 通过 `tsc --noEmit --strict`（exit 0）。

## 二、B1–B6 落实核对

| # | 裁决要求 | 结论 | 证据 |
| --- | --- | --- | --- |
| B1 | 根目录「只改值不清空」md/TS 一致钉死 | 落实 | md:145-146「只能改值、不能清空——不接受 null，空串同样非法；仅 inferyard_path 可置 null」；api-types.ts:82-86 docstring 同义钉死，`results_root?: string` / `models_root?: string` 保持不变（不允许 null），仅 `inferyard_path?: string \| null`。两侧语义完全封闭 |
| B2 | 固定时窗退出 3 → done + incomplete | 落实 | md:232-234「CLI 返回 status=`duration_requires_new_window`、退出码 3，按 §3.3 映射任务 done 且 `exit.completeness="incomplete"`……（不是 failed，也不是 409）」。事实复核：batch_runner.py:129-130 `return __result(..., "duration_requires_new_window", 3, pending)`，与契约一致；§3.3 映射表（md:78-82）退出 3 → done/incomplete 不变，自洽 |
| B3 | standalone run 恒不可 resume；CLI 计划序裁决 vs 服务端 UI 预判边界 | 落实 | §4.4 md:191「服务端预判（仅供 UI 置灰）……standalone run（batch_id=null，run --config 产物）恒为 false。最终以 CLI 裁决为准（§4.5）」；§4.5 md:227-231「resume 仅接受批次内 run——来源必须位于 <批次根>/runs/ 下且未完成；standalone run……一律 409 not_resumable……计划序上最新未完成轮次……以 CLI 为最终裁决」；§3.2 md:65 `not_resumable` 括注同步扩展；TS 同步：RunSummary.resumable 注释（ts:241-245）、ResumeRunRequest.from_run 注释（ts:387-392）。事实复核：batch_runner.py:87 `root = request.from_run.parent.parent`（批次内约束）、:70-77 `_next` 按计划 trials 序取各 trial 最新 attempt（计划序），与契约表述一致 |
| B4 | 侦测根 = config 冻结 output.root；越界 409 | 落实 | §4.5 md:209-213「results_root 未配置 → 409 settings_incomplete。服务端解析 config 得冻结的 output.root，作为该任务 run 目录的侦测根（run --config 产物落在 <output.root>/<run_id>，不接受 --output-root 覆盖）；预校验……否则 409 output_root_outside_results」；§3.2 md:65 409 表新增该 reason；TS `ConflictReason` 新增 `"output_root_outside_results"`（ts:24）；RunTask.run_id 注释（md:257、ts:286）同步。事实复核：request.py:46-52 `output_root` 仅 rerun/experiment/fit/community 执行接受，普通 `run --config` 拒绝；runner.py:119-120 的 output_root 赋值位于 rerun 路径，不影响 app 的 `run --config` 路径。契约表述属实 |
| B5 | `^(?!\.+$)[A-Za-z0-9._-]+$` 两处一致 | 落实 | md:311 与 api-types.ts:457 逐字符一致，均附「显式禁止 `.`/`..` 等纯点分段」 |
| B6 | 报告目录名无冒号安全格式 + 碰撞后缀 | 落实 | md:289-291「**report-YYYYMMDDTHHMMSSZ**（UTC，文件系统安全、无冒号；同秒碰撞追加序号后缀唯一化，如 `report-20261012T092841Z-2`）」；TS report_id 注释（ts:395）同步 |

## 三、S1–S17 落实核对

全部落实。按主题合并列示（编号为第一轮报告序号）：

- **S1**（batch 识别）：md:168-169 改为「含 `batch.json` 且 `runs/` 子目录内为 run 目录」，与 T2 index.go:58 及 CLI 批次根必写 batch.json（batch_runner.py:92 resume 路径 `read_json(root / "batch.json")` 可证）一致。
- **S2**（SettingsResponse 外层形状）：md:128 `SettingsResponse = { settings: UserSettings, effective: EffectiveSettings }`。
- **S3**（created_at 非空）：md:258 `created_at | string | 创建即生成，非空`，started_at/finished_at 拆行为 `string | null`；TS 侧 `created_at: string` 原有类型不变、补注释（ts:289）。
- **S4**（refresh 202 镜像类型）：md:157 `DeviceRefreshResponse = { "refreshing": true }`；ts:205-208 新增 `DeviceRefreshResponse` 接口。
- **S5**（§3.3 status 列为示例）：md:84-86 注「该列为**示例**……**映射以退出码为准**」。
- **S6**（CaseProgress.state 括注）：md:269-270「`pending`/`running` 为 app 聚合态；证据终态仅四值」；ts:294-296 CaseState 注释同步。
- **S7**（RunProgress.total resume 分母）：md:264-266「resume run 的 Journal 以 `resume_case_ids` 冻结续测子集，分母天然是续测子集而非全 plan `case_order`」；ts:330-334 注释同步。事实复核：journal.py:85 `selected = list(resume_case_ids) if resume_case_ids else list(trial["case_order"])`，一致。
- **S8**（报告生成三点）：① md:286-288「**1..100 个**，对齐 CLI 上限……含重复 run……→ 400 预校验（避免 `duplicate_report_run` 等落入 500）」——事实复核：report.py:142 `len(roots) > 100` 抛错、:160 `duplicate_report_run`，一致；② md:293「客户端断连/超时**不中断**生成，完成后仍广播 `report.created`」，ts:518 ReportCreatedEvent 注释同步；③ md:288「服务端先创建 `reports_root` 父目录」。
- **S9**（created_at 来源）：md:281-282「取报告**目录 mtime**……取不到时为 `null`」；ts:398 注释同步。
- **S10**（run_paths 失效呈现）：md:282-284「对应项为 `null`，UI 呈现『来源不可达』占位（不报错、不影响打开报告）」；ts:401-405 类型相应改为 `run_paths: (string | null)[]`（类型变化是落实 S10 的必然结果，md/TS 一致，属授权范围）。
- **S11**（containment 400 vs 404）：§3.2 md:63（400 增列「路径 containment 越界/形态非法（`from_run`、`run_paths`、`/reports/**` 静态路径等）」）、md:64（404 增列「静态文件不存在」）；§4.5 from_run「越界 → 400」（md:222）；§4.6「越界/形态非法 → 400，文件不存在 → 404（§3.2）」（md:297-298）。
- **S12**（下载并发模型）：md:320-321「`model acquire` 不取 inferyard 主机锁，多个下载任务**允许并行**，与 run 单槽队列互不阻塞」。
- **S13**（排队中取消）：md:242-243「排队中直接 `cancelled`（无退出码，`exit=null`，`finished_at` 落取消时刻）」；RunTask 表 md:259-260 同步；ts:290-293 注释同步。
- **S14**（事件 id 启动种子）：md:345-346「计数器以启动种子初始化（进程启动时派生，避免重启后 id 从 0 回退……）」；ts:473 注释同步；§5.1 断线重连段原「（如服务端重启 id 归零）」括注随之删除，前后一致。
- **S15**（T4 扩展措辞）：md:193「实现属 evidence 层（T4 扩展）」；ts:212 注释同步。
- **S16**（索引重建原子替换）：md:147-148「重建期间 `GET /api/runs` 继续服务旧索引，完成后**原子替换内存索引**」。
- **S17**（settings 无 SSE 记录在案）：md:148-150「设置变更不广播 SSE 事件（P0 已接受限制）：桌面+浏览器双开时，另一端的设置/历史视图需手动刷新」。

## 四、Q1–Q6 落实

§9 已由「未决问题（提交 reviewer）」改为「裁决记录（2026-10-12 冻结审查）」（md:402-417），
声明「原未决问题 Q1–Q6 已经 coordinator 裁决并落实于正文，本节留档」，逐条指向正文落点。
正文落点逐一核对：

- **Q1** → §4.2 md:141-142「Q1 裁决：接受派生而非第四项设置」；附带父目录创建落在 §4.6 md:288。
- **Q2** → §4.6 md:293-295「Q2 裁决：接受同步 + 前端长超时；若 T7 实测普遍超时，按冻结变更流程演进」；断连不中断与上限预校验两条强化均已写入同节正文。
- **Q3** → §4.5 md:214-216「同次响应的 `tasks` 数组即双包成组依据，T5 进度视图按它客户端成组，无服务端聚合实体（Q3 裁决）」。
- **Q4** → §4.4 md:195「`dirty` 的精确判据留待 T4 落地，届时以契约修订补丁回写、走变更重审（Q4 裁决）」。
- **Q5** → §5.1 md:348-349「≥2048 条；初始值，稳态事件率约 ≤5 ev/s，T4 实测后复核——Q5 裁决」。
- **Q6** → §2 md:17-18「全部响应携带 `Referrer-Policy: no-referrer`；服务端访问日志不记录 query 串/URL」+ md:21「LAN 开关实现时必须重审鉴权模型，保持本节声明」；§4.6 md:295-296 报告响应携带 no-referrer 同步。两项强化均升格为正式条款。

## 五、意外改动与新引入问题

**无。** diff 全部 hunk 均可映射至 B/S/Q 条目或版本行 housekeeping（md 头「v0 草案 →
v0 修订待复核」、ts 头加版本行）。两处值得说明但均判定为非问题：

1. `ReportSummary.run_paths` 类型由 `string[]` 改为 `(string | null)[]`（ts:405）——S10
   「写明失效呈现」的类型镜像必然结果，md:282-284 语义一致，属授权范围。
2. `ApiError.limitations` 注释「固定原因原样透传 → 固定原因数组原样透传」（ts:30）——
   措辞精确化，无语义变化。

md/TS 交叉一致性：本轮复核对全部变更字段做了两侧互查（UpdateSettingsRequest、
ConflictReason、DeviceRefreshResponse、RunSummary.resumable、RunTask、RunProgress.total、
CaseState、ResumeRunRequest、ReportSummary、CreateReportRequest、out_name 正则、SSE id），
无新增不一致。TS 通过 `tsc --noEmit --strict`。

冻前可改可不改的 nit（不阻塞冻结，记录备查）：§4.2 md:145「提供的路径须为非空绝对路径」
与紧随的「仅 `inferyard_path` 可置 `null`」字面上存在轻微张力（置 null 时并非非空绝对路径），
但例外句紧邻且与 TS 类型完全同构，T3/T4 无分裂实现风险。

## 六、冻结行政事项

复核通过即满足第一轮「修订落实并经 reviewer 复核 diff 后，契约可冻结」的条件。
建议 writer/coordinator 在执行冻结时将两文件版本行由「v0 修订待复核」改为「v0 已冻结
（2026-10-12）」——这是冻结动作本身的标记，不属于契约内容修订，无需再走一轮复核。
