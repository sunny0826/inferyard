# T1 契约审查报告（冻结关口）

审查：reviewer 角色（会话 3e8aa018-adb1-4629-bc33-d6971c670472），2026-10-12。
审查对象：`docs/api-contract.md`（378 行）+ `docs/api-types.ts`（500 行）@ `fbfab85`。
核对方法：两份契约文件逐字段互查；对 inferyard 真实契约（cli-surface、data-contract、
device-check、report-contract、run-evidence-layout、usage、schemas/*）逐条比对，并对关键
语义直接核对 CLI 源码（cli/request.py、runtime/batch_runner.py、runtime/runner.py、
runtime/trial_runner.py、evidence/journal.py、evidence/ledger.py、platforms/device_preflight.py、
reporting/report.py）；并与 T2 已交付的 internal/evidence/index.go 核对内部一致性。

## 一、Blocking（必须修改才能冻结）

**B1. `UpdateSettingsRequest` 的 null 语义前后矛盾**
`api-contract.md:139` 校验句意为接受 null（清空回「未配置」态）；但 `api-types.ts:81-83`
为 `results_root?: string; models_root?: string`（不允许 null），仅 `inferyard_path` 可置 null。
冻结后 T3 表单与 T4 validator 会按各自理解实现。二选一钉死：TS 改为 `string | null`，
或 md 明确「根目录只能改值、不能清空」。

**B2. 固定时窗负载不可 resume 的 CLI 行为描述错误（事实错误且自相矛盾）**
`api-contract.md:214` 称「固定时窗负载不可 resume 的情形由 CLI 退出 2 → 任务 failed」。
真实行为（batch_runner.py:129-130）：批次内最新中断轮次为固定时窗负载时返回
status=`duration_requires_new_window`、退出码 3。按契约 §3.3 自己的映射，退出 3 → 任务
`done` 且 `completeness="incomplete"`，不是 failed。须改为事实（退出 3 如实呈现），
或改为服务端预判（固定时窗可从冻结计划识别）→ 409 `not_resumable`。

**B3. resume 适用对象未界定：app 自己启动的 run 永远不可 resume**
CLI 硬约束（batch_runner.py:87,136-143）：resume 来源必须位于 `<批次根>/runs/` 下
（`resume_source_not_in_batch`）且是该批次计划序上最新未完成轮次
（`resume_requires_latest_unfinished_run`，退出 2）。而 `run --config` 单跑产物直接落在
`<config 冻结 output.root>/<run_id>`（runner.py:166,199），不构成批次结构——app 经
`POST /api/runs` 产出的全部是 standalone run，全部不可 resume；resume 只可能服务于外部
CLI `run --plan --output-root` 创建的批次。契约 §4.4 `resumable` 的判定语对 standalone run
（`batch_id=null`）无定义，§4.5 也未排除。必须显式写出：standalone run `resumable=false`、
resume 仅接受批次内 run；并注明「最新未完成轮次」以 CLI 为最终裁决（计划序感知而非纯
时间序，batch_runner.py:70-77 的 `_next`），服务端预判仅供 UI 置灰。

**B4. run 目录侦测根未定义（核心流程可实现性缺口）**
§4.5 说「tailer 侦测到新 run 目录」但未说在哪个根下侦测。`run --config` 的产物位置由
config 冻结的 `output.root` 决定（runner.py:119-120），不接受 `--output-root` 覆盖
（request.py:46-52）。若 config 创建时 `--results` 与 app 的 `results_root` 不同，新 run 落在
`results_root` 之外，侦测与历史索引都看不到它。契约须钉死规则（建议：服务端解析 config
得 `output.root` 作为侦测根，并说明该 run 与 `results_root` 历史索引的关系——或预校验两者
一致），连带明确 `results_root` 未配置时 `POST /api/runs` 是否 409 `settings_incomplete`。

**B5. `out_name` 正则允许 `.` / `..`（路径穿越语义错误）**
`api-contract.md:276` 与 `api-types.ts:420`：`^[A-Za-z0-9._-]+$` 声称「单个路径分段」，
但 `..` 通过校验。CLI 排他创建会把它挡成 `output_exists`（不造成覆盖），但契约的预校验
名不副实。冻结前修：排除纯点分段（如 `^(?!\.+$)[A-Za-z0-9._-]+$`，或显式禁止 `.`/`..`）。

**B6. 报告目录名 `report-<utc 时间戳>` 未固定文件系统安全格式**
`api-contract.md:260`。若按 §1 的 ISO 8601 示例（含冒号）命名，Windows NTFS 拒绝——
Windows 是 P0 构建目标。须钉死安全格式（如 `report-20261012T092841Z`）与同秒碰撞的
唯一化策略（序号后缀等；否则 CLI `--out` 排他冲突变 `output_exists`）。

## 二、Suggestion（可后改）

1. §4.4 batch 识别「含 `runs/` 子目录」与 T2 已交付实现不一致：internal/evidence/index.go:58
   以 `batch.json` 识别（CLI 批次根必写 `batch.json`，batch_runner.py:92）。建议契约改为
   「含 `batch.json` 且 `runs/` 内为 run 目录」。
2. §4.2 未写出 `SettingsResponse` 外层形状 `{ settings, effective }`（api-types.ts:74-77 有，
   md 只有成员类型）。
3. §4.5 RunTask 表将 `created_at/started_at/finished_at` 并列为 `string | null`；TS 中
   `created_at` 非空（创建即生成），md 应对齐。
4. §4.3 `POST /api/device/refresh` 的 202 响应 `{ refreshing: true }` 在 api-types.ts 无镜像类型。
5. §3.3 退出 0 的 status 列举不全：report 成功为 `rendered`，批量还有 `finished`/`resumed_subset`
   等；注明该列为示例、映射以退出码为准。
6. §4.5 `CaseProgress.state` 括注「对齐证据终态枚举」易误导：证据终态仅
   `completed/failed/cancelled/invalid`，`pending/running` 是 app 聚合态，写清。
7. §4.5 `RunProgress.total` 建议明确取自该 run inputs 中的冻结选中题集合：resume run 的
   Journal 以 `resume_case_ids` 冻结 `selected`（journal.py:85），分母天然是续测子集而非
   全 plan `case_order`，防止 T4 读错字段。
8. §4.6 报告生成：① `run_paths` 上限对齐 CLI 1..100（report.py:142，超限 EvidenceError→500），
   服务端 400 预校验；重复 run（`duplicate_report_run`→500）同理；②写明客户端断连/超时
   不中断生成、完成仍广播 `report.created`；③服务端须先创建 `reports_root` 父目录
   （CLI `--out` 要求父目录已存在）。
9. §4.6 `ReportSummary.created_at` 来源未指定：index.json 与封存均无生成时间戳，跨平台
   birthtime 不可靠；建议定为目录 mtime 或写明可取 null。
10. §4.6 `run_paths` 解析：index.json 内是报告→run 的相对来源链接（portable_index），报告
    搬移后失效；写明失效呈现。
11. §3.2 路径 containment 失败归 400 还是 404 未写；建议：越界/形态非法 400，索引中不存在 404。
12. §4.7 未声明下载并发模型：`model acquire` 不取主机锁，并行合法；写明允许并行还是排队。
13. §4.5 排队中取消的任务 `exit` 未定义（无退出码）；建议明确 `exit=null`、`finished_at` 落
    取消时刻。
14. §5.1 事件 id 跨重启碰撞：id 每进程从 0 递增，客户端带旧进程的大 Last-Event-ID 重连到
    已产出更多事件的新进程时会落入新缓冲范围被误补发。一行可修：计数器以启动种子初始化
    （仍为 uint64 十进制）；爆炸半径有限（run.state 全量替换 + sync.reset 自愈）。
15. §4.4 status 派生注明「实现属 T2 evidence 层」，但 T2 已交付的 index.go 只有
    sealed/partial 二态，status/dirty/score/resumable 派生尚未实现，实际落在 T4 扩展；
    措辞建议改为「evidence 层（T4 扩展）」。
16. §4.2 结果根变化触发索引重建期间 `GET /api/runs` 行为未定义；建议写「原子替换内存索引」。
17. settings 变更无 SSE 事件，桌面+浏览器双开时另一端设置/历史视图不刷新；P0 可接受，
    记录在案。

## 三、确认无误（含证据）

- 退出码语义 0/2/3/4/130 与 cli-surface.md:19、AGENTS.md 一致；退出 3→partial 不弹错、
  130→cancelled 不报错，符合 data-contract 取消/不完整语义。
- resume endpoint/pid 同给同省：cli/request.py:60-61 确认；§4.5 预校验 400 合理。
- model acquire URL 约束（HF/ModelScope blob、40 位 hex、单 .gguf、拒绝可变分支/query/穿越）、
  `--out` 新目录、失败目录保留、bytes/sha256 实测、无总量不伪造百分比——§4.7 全部一致。
- 报告 CSP 与禁 iframe：report-contract.md:57-60 + studio-app.md D4，§4.6 顶层打开正确。
- 默认双 bundle：cli-surface.md:165「不自动连续运行两包」；§4.5 `allow_single_bundle` 与
  队列表达双包正确对齐。
- 缺测 null 语义、`score.rate` 分母 0 为 null、「不读可缺失的 summary.json」、sealed 注释
  均与 data-contract 一致。
- 枚举：phase 七值、quality_state 四值、category 八值、execution_state 四终态、relation 五值
  均同 schema。
- DevicePreflight 透传建模与 device_preflight.py:300-380 一致；device-check 退出 2 不算错误
  与 device-check.md 状态表一致。
- ResourceSample 字段均有 sample.schema.json 真实来源（system_mem_available/service_rss
  bytes、service_cpu_ticks 差分、temperature celsius，逐项 null+missing_reason）。
- RunProgress 可重建性：plan_index/error_category/t_send_ns/t_terminal_ns 在
  request_started/request_finished 齐备；stop_reason 来自 run_stopped.data.reason；
  取消为 user_cancelled（trial_runner.py:308）。
- §8 M0 描述属实（裸 token、count 事件、/assets 不校验、32 字节 token、127.0.0.1:0）。
- md/TS 字段一致性：除 B1 与 suggestion 2/3/4 外全部逐一对应（reviewer 完成全量比对）。

覆盖性结论：P0 四域 + 设置的全部用例均有端点与事件，无缺失。唯一范围性缺口是 B3 的
resume 对象问题。

## 四、Q1–Q6 裁决建议

- **Q1（reports_root 派生）**：接受。对齐 inferyard 工作区惯例；附带 suggestion 8③
  （父目录由服务端创建）。
- **Q2（报告生成同步化）**：接受同步 + 前端 ≥120s 超时，附两条修订写入契约：客户端断连/
  超时不中断 CLI、完成仍广播 report.created；run_paths 上限 100 服务端预校验。若 T7 实测
  普遍超时，再按冻结变更流程演进。
- **Q3（双 bundle 任务模型）**：接受该粒度（每 config 一任务、按数组顺序串行）。T5 按同次
  POST /api/runs 返回的 tasks 数组客户端成组即可，无需服务端聚合实体。
- **Q4（dirty 判据推迟）**：接受。T4 落地判据时以契约修订补丁回写（走变更重审）；
  注意判写实际落在 T4 而非已收口的 T2。
- **Q5（2048 条 / 15s）**：接受为初始值。稳态事件率约 ≤5 ev/s，2048 条覆盖 ≥7 分钟回放；
  保留「T4 实测后复核」钩子。
- **Q6（query token）**：接受 P0 模型，两项强化升格为正式条款：`Referrer-Policy: no-referrer`
  写入 §2/§4.6；补「服务端访问日志不落 query 串/URL」。LAN 开关（P2）实现时必须重审
  鉴权模型，保持 §2 声明。

## 五、总体结论

**需修订后冻结。** 六条 blocking 均为小改动，但任意一条进入冻结态都会在 T3–T6 产生分裂
实现或确定性的平台/交互错误。修订落实并经 reviewer 复核 diff 后，契约可冻结；
suggestion 部分可随 T4/T7 实测分批吸收。
