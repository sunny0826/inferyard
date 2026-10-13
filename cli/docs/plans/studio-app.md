# InferYard Studio（暂名）：桌面/Web 双端面板

当前 monorepo 复跑目录：CLI 的 `mise exec -- uv run --frozen ...` 在 `cli/` 内执行，Studio 的前端与 Go 检查在 `studio/` 内执行。下文历史记录中的仓库根指迁移前位置。

2026-10-12 与用户讨论定稿。独立新仓库（建议名 `inferyard-studio`，与本仓库同级），
以 inferyard CLI 为唯一执行引擎与事实来源，自身不实现测评逻辑。本文档记录功能范围、
backlog 与架构设计；**尚未开始开发**，开发前需用户另行授权。

## 定位与前提

- 最终面向外部用户；第一版自用，向导化与分发投入推迟。
- 四个功能域：benchmark 运行情况、本机信息、报告、模型下载。
- inferyard CLI 无 daemon/HTTP 模式：stdout 仅结束时输出单个 JSON，进度只存在于
  证据目录的事件流。app 以子进程调用 CLI 并 tail 证据目录。
- 技术栈：MyGo（桌面壳）+ Go 标准库 net/http（web 端），无第三方 web 框架。
  经核实 MyGo 是纯桌面框架，其 HTTP 能力只服务本进程 webview，浏览器端必须自建
  HTTP 服务（标准库即可）。

## 功能范围（2026-10-12 确认）

### P0（第一版）

**运行情况**（app 是控制器，可启动/取消 run）：

- 历史列表：扫描结果根，按批次/run 展示状态（complete/partial/cancelled/dirty）、
  模型、引擎、bundle、总分、时间。
- 启动 run：选择已绑定 config；默认范围 zh-core + zh-svg-pelican 双 bundle，
  单 bundle 须用户显式选择。
- 实时进度：阶段（probe/warmup/baseline/formal/scoring/...）、第几题/共几题、
  逐题状态与得分；数据源为 `events.jsonl` 同步追加事件（seq 严格递增）。
- 资源曲线：内存/CPU 实时曲线（`memory.jsonl` 每秒采样）。
- 取消（对子进程发 SIGINT，退出 130）与 resume（续测未执行项，绑定同批次根）。
- 完成/失败桌面通知。

**本机信息**：

- `device-check --json` 快照：硬件、本地 GGUF 列表（大小/SHA256/chat_template）、
  推荐配置与阻断原因。
- 启动时自动刷新一次 + 手动刷新。

**报告**：

- 报告列表（扫描已有报告目录）、勾选一个至多个 run 生成报告、
  查看自包含 `report.html`（顶层打开，不重做数据化 UI）。

**模型下载**：

- 粘贴 URL 下载：HF/ModelScope、固定 40 位 revision、单个 .gguf（CLI 硬约束）。
- 进度：轮询 `--out` 下 `文件名.part` 大小；无总量时显示已下载量与速率，不做假百分比。
- 取消下载；下载列表（进行中/完成/失败；失败目录保留并提示换新目录）。
- 下载完成后在本机模型列表可见。

**设置**：

- inferyard 二进制路径（默认自动探测 PATH，可手动覆盖）、结果根目录、模型根目录。

### P1

- 两 run 对比（compare，严格同计划/同资产/同主机）与核验状态展示（verify）。
- observer 服务进程监测（复用 `observer/` 的 snapshot：哪个推理服务在跑、CPU/内存）；
  注意其 `--bench-run` 只支持旧 layout1，不做 run 绑定，只做进程枚举。
- 内置推荐模型清单（HF + ModelScope 双源，内容需策划）。
- 报告列表用 `index.json` 显示分数摘要，不开 HTML 也能扫一眼。

### P2

- config 准备流程向导化（init → device-check → create → bind）；第一版仅引导文案。
- export / public package。
- memory-check 页。
- 托盘常驻 / 开机启动 / LAN 访问开关（显式、默认关）/ 自动更新 / 多语言。

### Backlog（记录备查，暂不排期）

- engine-fit 引擎适配诊断的展示与触发。
- overhead 开销对照。
- extension 扩展协议（freeze/run/replay）。
- 公开包核验向导。
- 模型文件管理（删除/移动；第一版最多"在文件管理器中显示"，不做删除）。

## 架构设计

### 总体结构

单 Go 进程、两种入口、一套前端：

```
┌─ MyGo 桌面壳：窗口/菜单/托盘/打包/自动更新
│  窗口 URL = http://127.0.0.1:<随机端口>/?token=<会话令牌>
├─ net/http 服务（同进程）：embed 前端 + REST + SSE；默认只绑 127.0.0.1
├─ core 领域服务：Runs / Device / Reports / Downloads / Settings / EventBus
└─ inferyard 集成层：子进程执行 CLI + tail 证据目录 + 轮询 .part
```

传输统一为 HTTP：客户端→服务端用 REST，服务端→客户端推送用 SSE（全部推送均为
服务端单向，SSE 标准库可实现，不需要 WebSocket）。不使用 `mygo.Bind` 桥——桌面端与
浏览器走同一条传输，前端无 transport 抽象层。浏览器访问同一地址即为 web 端。

### 模块划分

| 模块 | 职责 |
| --- | --- |
| `main.go` | MyGo 入口：起 server → 开窗口指向它；桌面通知挂在事件总线上 |
| `internal/inferyard/` | CLI 契约层：二进制探测、命令构造、子进程生命周期（启动/信号取消/退出码 0/2/3/4/130 映射）、stdout JSON 解析；唯一知道 CLI 细节的地方 |
| `internal/evidence/` | 证据目录只读层：扫描结果根建索引（batch/run/报告）、`events.jsonl`/`memory.jsonl` 按 seq/offset 增量 tail、`*.part` 大小轮询 |
| `internal/core/` | 领域服务：RunService（串行队列、启动/取消/resume、进度聚合）、DeviceService（快照缓存）、ReportService（索引/生成/路径解析）、DownloadService（任务表/进度/取消）、Settings（well-known dir 下的 JSON）、EventBus（内部 pub/sub） |
| `internal/server/` | net/http：路由、会话 token 鉴权、REST handlers、SSE hub、`embed` 前端产物、`/reports/{id}/` 静态报告服务 |
| `web/` | 前端工程：Vite + TypeScript + Svelte（建议），图表用 uPlot；页面：Dashboard / Runs / RunDetail / Reports / Models / Device / Settings |

### 状态归属

- 证据目录是唯一事实来源；app 内存中的任务态与索引重启即丢，扫描结果根重建。
- app 自有状态只有设置（inferyard 路径、结果根、模型根、窗口偏好），一个 JSON 文件，
  不引入数据库。
- inferyard 本身有主机锁，run 串行执行；core 用单槽队列表达，不做并行 run。

### 关键数据流

**启动 run 到实时进度**：`POST /api/runs` → RunService 入队 → inferyard 层起子进程
→ tailer 在 output-root 下侦测新 run 目录 → 按 seq 增量读 `events.jsonl`，聚合成
进度（phase、done/total——total 取自 `inputs.json`、最近得分）→ EventBus → SSE →
前端；`memory.jsonl` 驱动曲线环形缓冲。子进程退出后读最终 JSON，落索引，
推 `run.finished` + 桌面通知。

**取消/resume**：取消 = 向子进程发 SIGINT（130，证据为 partial/dirty，界面如实呈现）；
resume = `resume --from-run`，新 run 目录出现在同一批次根下，复用同一 tail 通路。

**报告**：勾选 run → `report --runs ... --out NEW` → 登记索引 →
`/reports/{id}/report.html` 静态 serve，webview 新窗口或浏览器新标签顶层打开
（报告 CSP 严格，不 iframe 嵌入自家页面）。

**下载**：`model acquire --source --out NEW` → 每秒轮询 `.part` 大小 → SSE 推送
已下载量与速率 → 结束读最终 JSON（bytes/sha256 实测值）→ DeviceService 失效缓存，
模型列表刷新可见。

### 关键决策与理由

- D1 单进程双入口、HTTP 为唯一传输：双端行为一致、无双绑定维护；代价是桌面端依赖
  本地端口，用随机端口 + 每会话 token 缓解（SSE 用 query token，REST 用 header）。
- D2 CLI 子进程是唯一执行面：无 daemon 可用；进度=tail 文件，粒度秒级，UI 事件驱动
  刷新即可。
- D3 证据目录为唯一事实源：app 索引可重建，永不复制 run 数据，遵守"不覆盖原始证据"。
- D4 报告不重做 UI：自包含 HTML 已含机器档案/成绩/曲线/逐题检索；app 只做索引与摘要。
- D5 下载进度诚实呈现：CLI 无总量与进度输出，UI 显示已下载量/速率，不伪造百分比。
- D6 约束编码位置：CLI 契约只在 `internal/inferyard`；"不启动/停止模型服务"
  "默认双 bundle""--out 必须新目录"等规则在 core 层校验，UI 不提供绕过入口。
- D7 错误映射：退出码 2/3/4/130 → UI 语义（输入阻断/不完整/工具错误/已取消），
  limitations 的固定原因原样展示，不回显原始异常。
- D8 安全：127.0.0.1 + 随机端口 + 会话 token；LAN 暴露是 P2 的显式开关。

### 里程碑（P0 范围）

- M0 spike：MyGo 窗口加载本地 HTTP、embed 前端、SSE 打通、三平台打包验证。
- M1：设置 + inferyard 二进制探测 + Device 页（只读链路）。
- M2：RunService 全链路（启动/进度/曲线/取消）+ Dashboard/RunDetail。
- M3：resume + 历史索引完善。
- M4：Reports（列表/生成/查看）。
- M5：Downloads（进度/取消/列表）。
- M6：桌面通知、打磨、三平台打包。

## 开放项

- 仓库名与位置（建议：同级独立仓库 `inferyard-studio`）。
- 前端框架确认（建议 Svelte；可接受 vanilla TS 或 React）。
- LAN 暴露的鉴权模型（P2 再细化）。
- 面向外部用户分发时的签名/公证/自动更新投入（P2 再议）。

## 任务拆分（2026-10-12，待用户 review 后派发）

### 执行状态

- 2026-10-12：用户确认保持 worker+ 现状，授权派发。协调者已建仓库目录
  `/Users/guoxudong/codes/local-ai/inferyard-studio`（git init，分支 main）。
- T0 已派发：operation `studio-t0-scaffold-spike`，会话 fb907ac6-6f26-4a9d-88f5-c337801e3e38
  （implementer / worker+ 角色，本机纯会话——新仓库尚未进 Lody 项目目录，任务包用绝对路径约束）。
  后续任务待 T0 完成后按依赖派发。
- 2026-10-12：T0 首次派发受阻（纯会话沙箱不含目标目录、禁连本机代理）；用户完成 Lody 项目登记
  （local-project-0cb4f9d15a44759cc778f0f5）并开放网络权限后，T0 以 workContext 重派：
  operation `studio-t0-scaffold-spike-r2`，会话 86ba586a-91c2-41d6-8117-4068ffc14b7c。
- 2026-10-12：r2 受阻（会话 PATH 为 Go 1.24.2，MyGo v0.4.5 要求 ≥1.27.1，工具链直连下载
  unexpected EOF）。协调者核实本机 mise 有 1.27.1、模块缓存已有 mygo@v0.4.5，并用
  GOPROXY=goproxy.cn 预热 go1.27.2 工具链、验证 `mygo doctor` 通过。T0 第三次派发：
  operation `studio-t0-scaffold-spike-r3`，会话 4c7fecb6-2cd1-4bb5-816b-baf9c2cb11f2，
  任务包含环境现状与镜像用法（固定 @v0.4.5、GOPROXY 镜像、npmmirror、代理兜底）。
- 2026-10-12：r2 会话由镜像规则消息唤醒并完成 T0（提交 9c45a12/c060685，六项条件通过）；
  协调者取消已被取代的 r3（其未提交的独立复验证据有价值，并入提交 c0680cd）。协调者亲验：
  go vet/go test 通过、三平台构建退出 0、headless 冒烟（页面 200、无 token 404/401、
  SSE 计数到达）。**T0 完成**，解除对后续任务的阻塞。
- 2026-10-12：T1/T2 并行派发。T1（writer，显式 Kimi Code k3-256k max）：operation
  `studio-t1-api-contract`，会话 5f6fe088-9446-464c-ab3a-b455927d8549。T2（implementer /
  worker+）：operation `studio-t2-cli-evidence`，会话 11421e43-3292-4c0e-9df6-4082d0ee5456。
  两者均绑定 inferyard-studio workContext，文件所有权互不交集（T1 只写 docs/ 两个契约文件，
  T2 只写 internal/inferyard 与 internal/evidence）。
- 2026-10-12：T2 完成回执（提交 c6d593b），但协调者亲验发现 `TestCancellation/false` 在
  `-race` 下约 40% 失败（组 SIGINT 的 trap 未跑完即被宽限 SIGKILL，测试 GracePeriod=100ms）。
  T2 其余部分验收通过（vet/全量测试/交叉编译/真实只读 CLI 解析）。返工派发：
  operation `studio-t2-rework-cancellation`，会话 428699dd-0939-4c25-957f-668fb045e898，
  验收口径 `-run TestCancellation -count=20` 全绿 + 全量 race 测试全绿。
- 2026-10-12：T2 返工完成（提交 9834a0f）。实测根因：bash 等待前台子进程退出才执行 trap
  的信号时序（绕开客户端直发组 SIGINT 仍复现 >2.1s 无响应），非宽限不足；修复为专用 Go
  测试子进程注册信号处理器后再报就绪，生产实现与默认 2s 宽限不变。协调者复验
  `-count=20` 与全量 race 全绿，**T2 正式收口**。
- 2026-10-12：T1 完成（提交 fbfab85：docs/api-contract.md 378 行 + docs/api-types.ts
  500 行，覆盖矩阵齐全，§9 留六个未决问题）。协调者已通读契约。随后并行派发：
  契约审查（reviewer，冻结关口，快照 fbfab85）operation `studio-t1-review`，
  会话 3e8aa018-adb1-4629-bc33-d6971c670472；T3 前端骨架（implementer-B / worker+，
  基于草案开工）operation `studio-t3-frontend-skeleton`，
  会话 75f51ef2-4298-43bb-9854-3f8724e2bc33。
- 2026-10-12：T3 完成（提交 6d85865：Svelte 5 + TS 骨架、API/SSE client、mock 模式、
  Device/Settings 页、Playwright-本机 Chrome 验证脚本）。依赖决策记录：TypeScript 固定 ~6
  （svelte-check 兼容），新增 zod（边界解析）与 @playwright/test（仅本机 Chrome，不下载浏览器）。
- 2026-10-12：T1 契约审查（reviewer，会话 3e8aa018）结论「需修订后冻结」：6 blocking
  （B1 设置 null 语义矛盾、B2 固定时窗 resume 退出码事实错误、B3 standalone run 不可
  resume 未界定、B4 run 侦测根未定义、B5 out_name 允许 `..`、B6 报告目录名 Windows 不安全）
  + 17 suggestion + Q1–Q6 裁决建议。报告存档 docs/reviews/t1-contract-review.md。
  coordinator 裁决全部接受；B1 钉死「只改值不清空」（对齐 T3 实现），B4 钉死
  「解析 config 的 output.root 为侦测根 + 预校验位于 results_root 内」。
  **B3 产品事实记录：app 经 run --config 启动的 run 均不可 resume，resume 只服务外部
  CLI 批次（run --plan --output-root）**——P0 resume 功能的实际适用面。
  修订派发 writer：operation `studio-t1-revision`，会话 e17d884c-4872-4d37-b596-24c382ce1999。
- 2026-10-12：T1 复核结论**可冻结**（报告 docs/reviews/t1-contract-review-r2.md：B1–B6/S1–S17/Q1–Q6
  全部落实、diff 482 行逐行核对无意外改动、md/TS 一致、tsc 通过；两处备查不阻塞）。
  协调者执行冻结：版本行改「v0 已冻结」，提交 6a07b19。**契约 v0 正式冻结**——
  后续变更须 coordinator 仲裁 + reviewer 重审。T4 派发（implementer-A / worker+）：
  operation `studio-t4-core-server`，会话 f1b55629-522a-489a-b000-21fb82269cf1——
  internal/core 五服务+EventBus + internal/server 全端面，含 Q4 dirty 判据落地
  （补丁建议回写）与 Q5 缓冲参数实测两个冻结后钩子。
- 2026-10-12：T4 完成（提交 aecaf9c，19 文件 +3245/-187，无新增依赖）。协调者亲验通过：
  提交边界仅 internal/+main.go+scripts、冻结契约与 web/ 零改动；vet/全量测试/race/两平台
  交叉编译复跑全绿；鉴权/fixture-SSE/报告静态测试存在且通过；main.go 无 token 日志；
  dirty 判据实证在 internal/evidence/dirty.go（主机状态文件四条件匹配）；真机 MyGo 窗口
  截图确认 Device 页渲染+SSE 连接。Q4 补丁建议与 Q5 实测（2048/15s/2Hz 维持）待 T8 前
  随契约修订窗口统一仲裁。T5 派发（implementer-B）：operation `studio-t5-runs-pages`，
  会话 c91a6d9c-c515-4053-8c7b-7703a29f52ee。
- 2026-10-12：T5 完成（提交 f876da3，新增依赖 uplot@1.6.32 已声明）。协调者亲验通过：
  提交边界仅 web/+package.json+bun.lock；冻结契约零改动；`web/src/api/types.ts` 已同步为
  冻结镜像逐字副本（diff 为空，修正了 T3 的旧副本）；契约实有 `service_cpu_percent`
  （ticks 差分在后端，T5 任务卡描述误差已记录）；tsc strict/svelte-check/build 复跑全绿；
  Chrome 验收脚本从仓库根 `bun run web/tests/verify.mjs` 复跑全 PASS（含 T5 历史层级/
  优先级/启动/取消/resume/退出码/SSE 资源断点 + T3 回归 + wire fixture）。
  **注意**：该脚本从 `web/` 目录运行会因 vite 根配置丢失而失败（App.svelte 解析错误），
  正确调用方式已确认——T6 任务须把确切命令写入 web/README.md。T6 派发（implementer-B）：
  operation `studio-t6-reports-downloads`，会话 4dc9ca89-7805-4c9b-a36b-adfb56dc6d04。
- 2026-10-12：T6 完成回执（提交 b158e24），但协调者亲验**抓到一个真实 flaky 缺陷**：
  `verify-assets-wire.mjs` 释放报告 POST 后的 `getByRole('status')` 断言约 60% 失败
  （协调者 5 连跑 3 败 2 过），根因方向为 Reports.svelte 双 `role="status"` 元素瞬时共存
  窗口被拉长至秒级（机制待 implementer 根因）。其余亲验通过：边界仅 web/、契约零改动、
  无 iframe、无伪造百分比（总量未知明示）、typecheck/build 全绿。T6 返工派发：
  operation `studio-t6-rework-status-race`，会话 bb01e862-5274-4940-a2ab-54d169274d3e，
  验收口径 = 根因证据 + 仓库根 10 连跑全 PASS。
- 2026-10-12：T6 返工完成（提交 cf20f6d）。根因实测修正了协调者的假设：双 status 窗口仅
  1.8–3.2ms，Playwright 1.64 将 strict mode 违例判为不可重试（断言 5ms 即失败，5000ms
  是上限而非实际时长）。修复为单一持久 status 区域（结构性消除竞态，无 sleep/重试）+
  挂起刷新 GET 的确定性回归（旧组件稳定失败、新组件通过）。协调者亲验：提交仅两个
  web/ 文件；typecheck/build 全绿；验收脚本亲跑 **10/10 全 PASS**（每轮 6 段）。
  **T6 正式收口，前端轨道完成**。进入 T7 联调（协调者）。
- 2026-10-12：**T7 联调完成**（协调者亲办）。fixture 沙盘（/tmp/studio-t7）+ 真实 embed
  二进制 + 真实前端全链路验证：401 鉴权、settings 读写+reports_root 派生、fixture run
  真实索引（standalone `resumable=false`、UI 显示「独立运行不支持续测」——B3 端到端
  生效）、SSE 连接与 Last-Event-ID 重连 200、报告/下载空列表、CLI 缺失时
  `cli_unavailable` 如实报错、假 config 路径 400、device null 快照；真实 Chrome 驱动
  真实页面渲染真实数据（zod 边界与 Go 形状无失配）；MyGo 真机窗口冒烟通过（截图
  /tmp/studio-t7/gui/desktop.png）。**发现 1 个整容级缺陷**：favicon.ico 浏览器自动请求
  撞鉴权墙产生 console 401 噪音（根因查明，无功能影响）。测试写入的 settings.json
  已从用户配置目录清除。两个后续小任务并行派发：favicon 修复（implementer-B，
  operation `studio-t7-favicon-fix`，会话 44c5b940-df9e-4c62-9d7c-4d66864b0cbb，
  方案 data: URI 图标）与 Q4/Q5 契约补丁回写（writer，operation
  `studio-contract-patch-q4q5`，会话 080af13b-b610-49a4-9732-853195f5280e——
  Q4 dirty 判据措辞落 §4.4、Q5 实测维持参数记录、版本行加补丁注记，无字段变更）。
  两者落地后派 T8 全量审查（冻结 HEAD，含补丁复核）。
- 2026-10-12：两项收尾落地并亲验通过——Q4/Q5 契约补丁 `3fc44de`（dirty 判据措辞与
  dirty.go 逐项一致、Q5 维持参数记录、版本行补丁注记；TS 镜像无需改动）；favicon 修复
  `e8cb5b6`（一行 data: URI 图标；协调者重建 embed 二进制复跑五路由探针，console 401
  噪音归零，B 自验 3 连跑 PASS）。**T8 冻结快照 = e8cb5b6**。T8 全量审查派发（reviewer）：
  operation `studio-t8-full-review`，会话 ac7027d3-750d-4474-baa6-f994d1a2c2a9——
  契约符合性系统比对、Q4/Q5 补丁复核、安全面、证据完整性、前端正确性、README 声明
  核对、代码健康；报告写 docs/reviews/t8-full-review.md。审查期间冻结被审范围。
- 2026-10-12：T8 完成，结论「需修复后过 T9」：1 blocking + 12 suggestion
  （docs/reviews/t8-full-review.md）。**B1 经协调者主仓库二次核实成立**：真实
  run_stopped 取消原因是 `user_cancelled`（trial_runner.py:296-310 的 CancelledError
  分支：安全停止/预算耗尽各归退出 3，否则 (130,user_cancelled)），dirty.go 判据里的
  `interrupted`/`cancelled` 是伪造值，真实证据中不存在——cancelled 优先级对真实数据
  100% 失效。reason 全集：user_cancelled / duration_window_interrupted /
  single|trial_wall_budget_exhausted / diagnostic_run_not_formal /
  check_only_no_formal_cases / tool_interrupted / 安全硬件值，仅 user_cancelled 可映射
  cancelled。双轨返工并行派发：后端（A，operation `studio-t8-rework-backend`，会话
  bfec5306-1184-44c6-b2be-424ffdcb63ad——B1 判据+单测、S1 服务端按采样周期聚合、
  S2/S3/S5/S6/S7/S8/S9）与前端（B，operation `studio-t8-rework-frontend`，会话
  f521bc42-a7bc-42fd-93a6-78e438a14a4d——mock 换真实 reason 值、S4 zod 对齐镜像）。
- 2026-10-12：双轨返工完成（后端 `36786f5`、前端 `571795b`）。协调者亲验通过：边界正确
  （A 仅 internal/+main.go+根 README、B 仅 web/，契约零改动）；B1 判据已钉死为仅
  `user_cancelled` 且单测覆盖真实 reason 全集；S1 半秒锚窗字段级合流+cpu ticks 同源同
  boot 守卫属实；vet/全量/race/交叉编译复跑全绿；前端 typecheck/build 全绿、验收 3 连跑
  6/6 PASS。增量复核派发（reviewer）：operation `studio-t8-rework-review`，会话
  e52bb398-2d25-4f85-b4bc-525d29d892d4，复核 diff e8cb5b6..571795b，裁决放行 T9/再修。
- 2026-10-12：增量复核结论**放行 T9**（docs/reviews/t8-rework-review.md：B1 第一手证据
  落实、S1–S9 全落实、亲跑回归全绿、副作用扫描干净）。新增 3 条 suggestion 级瑕疵记入
  T10 前卫生轮：① server README 测试计数 48→79 未同步；② core README 一处 S3 旧措辞；
  ③ mock 退出 0 的 cli_status 'complete' 与真实 'finished' 不符（展示性，契约按退出码
  映射，非阻塞）。T9-L1 fixture 全路径验收派发（verifier/grok-4.7）：operation
  `studio-t9-l1-acceptance`，会话 3ed43471-65be-4b49-b45a-e79361c18bf3，冻结快照
  571795b。**L2（真实只读 CLI+小型真实下载）与 L3（真实 run）等待用户显式授权**。
- 2026-10-12：**用户显式授权 L2+L3**。协调者侦查结论：本机无存活模型服务（5000 端口是
  AirPlay）；`llama-server` 已装（/opt/homebrew/bin）；SmolLM2-135M GGUF（105MB）已在
  当日 e2e 验证目录，L3 直接复用不再下载；真实 CLI 调用形态 `mise exec -- uv run
  --frozen inferyard`（cwd=主仓库根，将包为 /tmp wrapper 供 settings.inferyard_path）。
  执行序列：L1 通过 → L2（真实 device-check + Studio 真实下载一个小型 GGUF，走镜像，
  落 /tmp 沙盘）→ L3（ verifier 启动专用 llama-server+SmolLM2，Studio 驱动冒烟 run：
  进行中取消验 B1 端到端 + 一个短完成 run；墙钟上限 10 分钟、zh-smoke 量级、AC 在线
  检查、温度停止条件；全部产物落 /tmp/studio-t9，明确标注冒烟不属测评结论；结束后
  停服务并报告证据路径）。
- 2026-10-13：**T9-L1 通过**（报告 docs/reviews/t9-l1-acceptance.md）。首轮输出因传输中断
  未落盘，报告补交后协调者亲验：results.json 144/144 逐条统计属实、run.log 末尾
  TOTAL 144/144、三平台构建产物与 GUI 截图存在且干净（五页面导航、双 bundle 表单、
  无错误横幅）。验收期间两起用户可见现象（GUI internal_error 横幅、RunDetail 不渲染）
  均定位为 fake fixture 不满足冻结 device_preflight.v1/枚举形状，**非产品缺陷**；
  线束 bug 16 项全部记录；产品侧观察 4 条（校验顺序、/private/tmp 规范化、前端严格
  校验、T8 挂账维持）均为契约内合理行为。HEAD 保持 571795b。
- 2026-10-13：L2 任务包侦查完成并派发（verifier/grok-4.7 新会话 50836fe5-bc23-45cb-
  93c6-5e745fdc0706，operation `studio-t9-l2-acceptance`）。关键事实：CLI 下载
  `httpx.Client(trust_env=False)` 代理环境变量无效，huggingface.co 直连不可行 →
  下载源选 ModelScope 直连（已验证 blob hash 4537f717… 经 API 302→国内 CDN）；
  冻结 URL 形状为 `/{owner}/{repo}/blob/<40hex>/<file>.gguf`（两端校验一致）；
  wrapper /tmp/studio-t9/bin/inferyard-wrapper 已建并验证（--version=0.0.2、
  device-check --json 信封 details.kind=device_preflight.v1）。验收项：真实 CLI 探测
  +device refresh、真实下载成功（q4_k_m 491MB，sha256 实测核对）、下载中途取消
  （q2_k，.part 保留+同名 409）、真实后端 GUI/Chrome 冒烟、token 不落日志。
- 2026-10-13：L2 验收返回 **56/58，两项 FAIL**（报告 docs/reviews/t9-l2-acceptance.md）。
  协调者逐项亲验根因后裁决：
  **L2-3（.part 不保留）改判 PASS（契约口径）**——契约 §4.7 原文仅承诺「失败/取消
  **目录**保留，不自动删除」，未承诺 .part 保留；真实 CLI model_source_download.py:78-83
  在 BaseException 时主动 unlink .part（中断清理临时文件是主仓库既有设计）；Studio 行为
  （目录保留、同名 409、downloaded_bytes 停最后观测值）与契约一致。FAIL 根源是协调者
  任务包把 L1 报告中基于 fake CLI 的观察性括号「.part 存在」误升为契约断言。
  **教训：任务包引用契约口径必须引用原文，不得从验收观察二次推导**。挂账：契约 §4.7
  下轮维护窗口补澄清句「.part 生命周期归 CLI，取消后由 CLI 清理」（与 S10 同窗口）。
  **L2-4（Device 页 internal_error）确认产品缺陷（前端）**——主仓库 device_preflight.py
  实证：`index` 仅 NVIDIA 分支下发（:72，CUDA 选卡语义），Darwin 分支 apple_gpu 形状为
  {name,cores,metal_supported,memory_kind,...} 无 index；主仓库自家 recommend() :167
  均以 `"index" in g` 按条件存在消费。前端 schema.ts:19 把 `devices[].index` 写死必填
  `z.number()`——S4 同类（zod 严于真实形状）的漏网处。唯一失败点（zod-device.json 证实
  两份真实快照均仅此一处）。修复 = index 改可选，前端返工派发（implementer-B）：
  operation `studio-t9-l2-rework-device-schema`，验收口径 = L2 真实证据 safeParse 通过
  + 真实后端 Device 页渲染复验 + 仓库根 verify.mjs 回归全 PASS。
  L2 通过项记录：真实 CLI 0.0.2 探测、真实 device_preflight.v1 快照（空 models_root 时
  blocked/no_local_model → 下载后 recommended）、真实下载 q4_k_m 491,400,032 B/136.2s、
  sha256 与 shasum 实测一致、source.json v3 形状、token 不落日志、settings 0600。
- 2026-10-13：**L2 正式收口**。契约维护补丁 `3eb5a0b`（DeviceGpuDevice.index 可选 +
  §4.6 勘误 + §4.7 .part 澄清 + 版本行）经 reviewer 复核**通过**
  （docs/reviews/contract-patch-2-review.md：三处事实行号全命中、纯放宽/澄清、无意外改动、
  镜像逐字节一致恢复）。协调者亲验：diff 与自述一致、镜像 diff 为空、tsc PASS。
  **当前 HEAD = 3eb5a0b，契约含两轮补丁，L2 全部验收项关闭**。
- 2026-10-13：L3 资产侦查完成（全部亲验）：zh-smoke bundle 在仓库 bundles/（40 题
  instruction）；e2e prism llama-server sha256 `1ad69eba…` 与参考配置钉值一致；
  SmolLM2-135M GGUF sha256 `ed5fa30c…` 一致；chat template 从 GGUF 内嵌提取至
  /tmp/studio-t9/assets/chat-template.jinja（sha256 `872be49d…`，与参考配置一致，
  证实即当日所用）；engine-sha256.json 库 manifest 36 条目已重新生成（bench-ops-smoke
  目录已被清理，原引用失效）；process_start_ticks 工具 = inferyard.platforms.identity。
  AC 在线已确认。
- 2026-10-13：**L3 通过，T9 三级验收全部收官**（报告 docs/reviews/t9-l3-acceptance.md，
  70/70 证据自检）。协调者亲验：取消 run 历史行 `status=cancelled/stop_reason=user_cancelled/
  sealed/incomplete/score=null/resumable=false`（**B1 修复端到端生效**——真实取消事件
  经事件流→历史派生→UI 徽章全链路正确）；完成 run `status=complete/plan_finished/
  score={1/40}`；历史页截图确认 cancelled 徽章+B3「独立运行不支持续测」；llama-server
  与 Studio 进程已停、`/var/tmp/inferyard-host.state.json` 恢复验收前备份（sha256 一致）。
  观察项如实记录：① 同进程 dirty 标记下 run#2 未被 exit 2 阻断（同服务同 pid/ticks 的
  真实 CLI 行为）；② 取消发生在 probe 阶段（done=0/40），终态证据链完整不受影响；
  ③ zh-smoke 实为 instruction 20+extraction 20（任务包措辞不精确，verifier 纠正）。
  冒烟分数 1/40 不判失败（AGENTS.md：错误答案不是 CLI 失败）。
  L3 资产遗产（/tmp/studio-t9/assets/：GGUF 提取的 chat-template.jinja、
  engine-sha256.json）与 configs/zh-smoke-l3.toml 保留备查。
- 2026-10-13：**T10 完成，项目 P0 交付**（HEAD=`d99b3a8`）。双轨：B 卫生 `04e6705`
  （mock cli_status 对齐真实值 finished/blocked 等五态+断言，协调者亲验边界仅 web/）；
  A 主任务 `d99b3a8`（三平台打包 dist/inferyard-studio-v0.1.0-{darwin-arm64,linux-amd64,
  windows-amd64}，darwin 附未签名 .app；EventBus 通知订阅覆盖 run/下载六种终态+报告完成，
  headless 不通知、失败降级记日志；卫生三项：server README 计数实测 81 PASS、core README
  S3 措辞、S11 包注释）。协调者亲验：提交边界全在各自所有权、dist 三平台产物齐、
  desktop+server 包测试复跑 PASS、工作树干净。
  **唯一未通过项（如实记录，非产品缺陷）**：macOS 通知实际到达——系统返回「用户不允许
  通知」，未观察到首次权限弹窗（未签名 .app 的 macOS 权限模型行为）。通知代码路径正确，
  到达性需用户在系统设置授权或后续签名——记入交付说明，不阻塞收口。
- 2026-10-13：**仓库已上线**：用户授权创建并推送 https://github.com/sunny0826/inferyard-studio
  （public，跟随主仓库可见性；owner 账号 sunny0826 keyring 认证）。推送前扫描：无 token/密钥
  （`sk-` 命中为 CSS 误报）；4 处本机路径（/Users/guoxudong，均在 M0 证据日志与 README
  验收记录——证据真实性优先保留，待用户决定是否清理）；`docs/reviews/` 协调者存档与
  `dist/` 产物未入 git（依 .gitignore 与未跟踪状态）。远端 HEAD=本地 `d99b3a8`，21 提交。
- 延期挂账：**S10** ~~契约 §4.6 理由句勘误~~ **已解决（3eb5a0b）**；
  **§4.7 .part 澄清** ~~下轮维护窗口~~ **已解决（3eb5a0b）**；
  **S11**（evidence.PollDownload / inferyard.Locate / Client.DeviceCheck 仅测试引用，
  保留并补包注释定位说明——随后端返工轮顺带或后续卫生轮）；**S12**（runs.go 手写 TOML
  解析的受控假设：标量单行+JSON 编码值，已核实与主仓库 toml_writer.py 现状一致；
  记为演进观察点——主仓库 writer 形态变化时需同步）；**reviewer 复核新增 3 条**
  （server README 测试计数 48→79 过期、core README 一处 S3 旧措辞、mock 退出 0 的
  cli_status 'complete' vs 真实 'finished'——T10 前卫生轮）。

角色来自 `.agent-conductor/models.json`（快照 2026-10-12）：coordinator=主 AI（inherit）、
implementer=角色 f96d8f99、reviewer=角色 5909b2e4、verifier=grok-4.7（本机显式）、
writer=kimi-code/k3-256k max（本机显式）。implementer 开两个并行会话：A（Go 后端）、
B（前端）。

### 阶段与依赖

```
T0 仓库+spike (A)  ──阻塞全部──┐
                               ├─ T2 inferyard+evidence 层 (A) ─┐
T1 API 契约 (writer) ─草案先行──┼─ T3 前端骨架+Device/Settings (B) ─ T5 Runs 页面 (B) ─ T6 报告/下载页 (B)
                               └─ 冻结后 ──→ T4 core+server (A) ──┘
                              ↓
                    T7 集成联调 (coordinator) → T8 全量审查 (reviewer) → T9 分级验收 (verifier) → T10 打包+通知 (A)
```

关键路径：T0 → T2 → T4 → T7 → T8 → T9。前端轨（T1草→T3→T5→T6）与之并行。

### 任务卡

**T0 仓库与 M0 spike**（implementer-A；协调者先建空仓库目录 + git init）
- 交付物：`inferyard-studio` 骨架（mygo web 模板）、构建脚本（go build + vite build + embed）、spike 验证记录。
- 交付标准：①窗口加载本地 HTTP（随机端口+token 注入）②同地址浏览器可达 ③SSE 演示事件桌面+浏览器双端到达 ④前端 embed 进单二进制 ⑤darwin/arm64、windows/amd64、linux/amd64 交叉构建成功（不签名）⑥记录 Go/Bun 版本与结论。阻塞一切。

**T1 API 契约**（writer；与 T2 并行；草案先行供 T3，冻结后 T3 必须对齐）
- 交付物：`docs/api-contract.md`（REST 路由与载荷、SSE 事件类型与载荷、错误模型、token 约定）、Go/TS 类型镜像清单。
- 交付标准：覆盖 P0 四个功能域全部用例；退出码→错误语义映射完整；reviewer 审查通过；冻结后变更须重开审查。阻塞 T4/T5/T6 联调。

**T2 inferyard 集成层 + evidence 层**（implementer-A）
- 交付物：`internal/inferyard`（探测/执行/取消/解析/退出码）+ `internal/evidence`（索引扫描、events/memory 增量 tail、.part 轮询）+ 单元测试与 fixture 证据目录。
- 交付标准：真实只读命令（`--version`、`device-check --json`）解析正确；退出码 0/2/3/4/130 映射测试；SIGINT 取消测试；fixture 下 seq 增量 tail（追加/截断/断点续读）；测试全绿。

**T3 前端骨架 + Device/Settings 页**（implementer-B；基于 T1 草案）
- 交付物：`web/` 骨架（路由、REST client、SSE client、mock 开关）、Device 页、Settings 页。
- 交付标准：mock 模式渲染与交互正确；device_preflight.v1 分区展示；设置读写走契约；vite build 通过。

**T4 core 服务层 + server**（implementer-A；前置 T1 冻结 + T2）
- 交付物：`internal/core` 五服务 + EventBus、`internal/server`（REST/SSE/token/embed/报告静态服务）。
- 交付标准：契约逐条有实现；鉴权与 127.0.0.1 绑定测试；用 fixture run 目录驱动"启动→进度→完成"全链路 SSE 演示（不起真实 run）；报告目录静态服务可开 report.html。

**T5 Runs/Dashboard/RunDetail 页**（implementer-B；前置 T1 + T3）
- 交付物：历史列表、启动/取消/resume 控制、实时进度视图、资源曲线（uPlot）。
- 交付标准：mock 全流程正确；曲线随 SSE 更新；取消/resume 状态转换（含 partial/dirty）如实呈现。

**T6 Reports + Models/Downloads 页**（implementer-B；前置 T5）
- 交付物：报告列表/生成/打开、下载页（进度/取消/列表）。
- 交付标准：mock 下交互正确；报告顶层打开（不 iframe）；下载无总量时显示已下载量+速率，不伪造百分比。

**T7 集成联调**（coordinator）：合并轨道、embed 联调、解冲突。

**T8 全量审查**（reviewer）：对冻结快照（T7 后 HEAD）审查；审查期间任何人不改被审范围，确需改动则该轮作废重开。

**T9 分级验收**（verifier）：
- L1（授权内）：fixture/模拟验收全部 P0 用户路径。
- L2（需授权）：真实只读 CLI（device-check）+ 一次小型真实模型下载（需网络授权）。
- L3（需单独授权）：真实 run 端到端——按 AGENTS.md 先检查设备、预算与停止条件。

**T10 打包与桌面通知**（implementer-A；T9 通过后）：三平台打包（不签名）、MyGo 通知接入 EventBus。

### 文件所有权

| 路径 | 唯一写入者 |
| --- | --- |
| `main.go`、`internal/**`、`go.mod`、`mygo.config.ts` | implementer-A |
| `web/**`、`package.json`、锁文件 | implementer-B（T0 期间由 A 建骨架，T0 完成交接给 B） |
| `docs/api-contract.md` 与类型镜像 | writer（T1 期间）；冻结后变更须 coordinator 仲裁 + reviewer 重审 |
| 计划文档（本文件） | coordinator |

### 通用边界（写入每个任务包）

- 禁止修改 inferyard 主仓库源码；禁止启动/停止模型服务；不 push、不建 PR，提交停留本地。
- 默认测评范围双 bundle；`--out` 必须新目录；证据目录只读，不复制不覆盖。
- 测试默认 fixture/模拟；真实网络与真实 run 走 T9 分级授权。
- 下载工具链/依赖（2026-10-12 用户补充）：优先镜像站——Go 模块用 `GOPROXY=https://goproxy.cn,direct`，
  npm/bun 用 `https://registry.npmmirror.com`；镜像取不到的用本机代理 `127.0.0.1:7897`。
  均以命令级环境变量或项目级配置使用，不写用户全局配置。
- 报告要求：每个任务回传改动清单、实际运行的检查与退出状态、未验证项。


### 2026-10-13 状态说明

经与用户确认，Studio 作为独立子项目并入本仓库 `studio/` 目录（Go module `inferyard/studio`）；GitHub 独立仓库 `sunny0826/inferyard-studio` 将在验证通过后删除。文档其余内容保留为历史记录。
