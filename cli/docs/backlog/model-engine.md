# 模型与引擎接入

[返回 Backlog](../backlog.md)

## 模型获取简化

`model acquire` 已实现固定 Hugging Face/ModelScope 单 GGUF 获取、来源核验和候选绑定。
2026-10-12 按操作者决定简化为 curl 级进程内下载：移除子进程 worker、IPC 进度协议、
远程元数据预核验、手工重定向白名单、三段预算与排他原子改名；新记录为
`remote_model_source.v3`，v1/v2 严格只读。依据 [ADR 051](../decisions/051-simplified-model-acquisition.md)。
现行规则见 [CLI 参考](../cli-surface.md#各动作的冻结行为)和[模型获取契约](../contracts/model-acquisition.md)。
正式 run 仍只使用本地已绑定服务。真实 CDN 性能仍未验证。

## 引擎获取与闭环下载 e2e

2026-10-12 新增 `engine acquire`（用户授权的 curl 级命令）：单 HTTPS 链接流式
下载引擎归档/二进制到新目录，实测 bytes/SHA256 写 `remote_engine_source.v1`
记录；不限 host 与扩展名、无 token、不解压不装配不启动，Windows 归档仍经
`runtime prepare --archive` 固定身份核验，引擎身份由 `config create` 层冻结。
规则见[引擎获取契约](../contracts/engine-acquisition.md)，取舍对齐
[ADR 051](../decisions/051-simplified-model-acquisition.md)。

同日新增 opt-in 闭环 e2e（`tests/integration/test_closed_loop_download_e2e.py`，
env `LAB_E2E_CLOSED_LOOP=1` + 两个来源 URL）：`model acquire` + `engine acquire`
真实下载后驱动 `ci_model_e2e.py` 全链（装配、`config create --model-source`
血缘绑定、双题包运行、report、verify）。macOS arm64 真机全绿（约 9 分钟），
回执归档本地 `validation/closed-loop-e2e-20261012/`；zh-smoke 40/40 complete、
pass 1（135M 模型低分非 CLI 失败）。硬件安全停止保留为 incomplete 证据不重试。
未验证范围：Windows/Linux 闭环、其他来源、真实 CDN 性能与正式双题包资格。
`ci_model_e2e.py` 增加可选 `--model-source` 透传候选血缘（默认行为不变）。

## 模型 e2e 三平台接入

2026-10-11 新增 `tests/packaging/ci_model_e2e.py` 与 `model-e2e` workflow（分支
ci/model-e2e，基于 #31 分支）：以 SmolLM2-135M-Instruct Q4_K_M（105MB）+ Prism
b10743 归档驱动真实模型全链路——init（zh-svg-pelican 单题包，用户显式固定）→
device-check → 平台准备（macOS 自动候选 / Windows runtime prepare 按预检推荐
CPU 或 CUDA / Linux config assets + 手工模板填实测环境值）→ 外部启动 llama-server
→ bind → run（真实推理）→ report → verify --rerender → 断言工作区 11 文件
（run 8 + report 3）、SVG 统计行、删除整个 bench-ops 后 verify 仍退出 0。

三平台本机实测全绿：macOS arm64（Metal）、Windows x64（CUDA 12.4，San3an-win）、
Linux x86_64（CPU，omarchy 物理机）。Linux 经六轮迭代修复三处脚本结构性问题
（TOML 引号丢失、模板 environment "unknown" 透传而 Linux 准入无豁免、
seed_support 未声明 "supported"），另规避一个引擎侧问题：Prism chat handler 的
peg-native 解析对 seed=42 在 Linux CPU 量化路径生成的无效 UTF-8 字节返回
HTTP 500（/completion 端点同字节 200；seed 1/7/123/2024 均 200；macOS/Windows
同 seed 正常），e2e 改用 seed 1，引擎容错待上游 PrismML-Eng 处理。macOS 首次
执行签名的引擎二进制链需约 13 秒（超出 CLI 10 秒版本查询预算），脚本在解压后
做一次 180 秒预算的 --version 预热。已知前提：云 Linux runner 无 cpufreq sysfs
时（governor 为缺测）严格环境准入会以明确错误阻断 run；经 continue-on-error、
探测加条件跳过两轮迭代后，按用户最终决策 Linux leg 从 CI 矩阵移除，Linux 以
本机物理机（omarchy 全链全绿）验证为准，同一脚本可在任何满足前提的机器上运行；
按用户决定，资产不做字节校验（Windows 引擎归档的固定
SHA 仍由 runtime prepare 内置校验）。Windows/omarchy 会话各轮现场全部保留。

2026-10-11 首次真实 CI 运行（PR #32）另暴露并修复一个 macOS 候选生成缺陷：
无电源 profile 的机器（macos-15 runner）把 None 写入 conditions.profile 导致
契约拒绝、无法生成候选——生成器改为实测 None 时保留既有 "unknown" 兜底
（macOS 准入本就豁免实测 None 的 unknown 字段，严格性不变），带单测回归
（b69f487）。

2026-10-11 e2e 补齐真实评分链路（分支 ci/e2e-zh-smoke）：原 e2e 仅跑
zh-svg-pelican（SVG 类不计质量分），真实模型输出的评分从未被执行。改造后
同一外部 llama-server 依次跑 zh-smoke（40 道 instruction/extraction 真实
评分题）与 zh-svg-pelican，`report --runs SMOKE SVG` 生成双 run 合并报告
（side-by-side comparison 仅 bundle 相关字段 different，80 项 same），并新增
内容级断言：40 题 quality_state 全为 pass/fail、dashboard 分母 40 且
numerator 与 pass 数一致、SVG 题 not_applicable、报告含「答案通过数」与
「动画元素统计」、工作区 19 文件（8+8+3，即正式输出计数）、删除两个
bench-ops 目录后 verify 仍退出 0。操作目录改为 bench-ops-smoke /
bench-ops-svg，Linux 候选的 bundle 版本改为从题包文件读取。macOS arm64
（Metal）本机实跑全绿：SmolLM2-135M 得 1/40（小模型预期低分，验证的是
评分链路而非模型质量），总耗时约 3 分钟。Windows/Linux 两条腿尚未以新
流程实跑（CI 将覆盖 Windows；Linux 仍需本机 omarchy 复跑确认）。

2026-10-13 首次上手引导落地（分支 feat/first-run-onboarding，commit 1ea41cd）：
新增只读 `progress --workspace`（progress.v1，退出码仅 0/2）、`config create
--workspace` 推导模式（显式参数优先，旧入口不变）、init 工作区新增
preflight/candidates/bound/reports 目录与中文路线图 README（`{{WORKSPACE}}`
导出替换）；Studio 新增工作区设置（默认 `~/.inferyard`）、readiness/provision
软门槛、progress 适配层与首页向导。真实真机验收通过：SmolLM2-135M（e2e 同款）
经 Studio 向导接口完整走 acquire → device-check → 双题包候选/绑定/运行 →
report+verify，`default_scope_complete: true`，两 run 封存、报告 verified
（zh-core 120 题约 3 分钟）。遗留：macOS 上引擎 `--version` 查询偶发 12 秒
签名校验卡顿，超过固定 10 秒超时导致 `config create` 暂态失败（重试即过）；
向导或文档宜提示预热/重试，是否放宽超时另行决策。

2026-10-13 版本查询超时问题已处理：两次主动复现（服务空闲 + 生成负载下各
8-12 次 --version）均未复现 12 秒卡顿；结合 e2e 既有记载（新解压 ad-hoc
签名二进制执行期校验可超 10 秒，CI 在超时预算外预热），定性为 macOS 执行期
签名/扫描延迟的偶发表现。修复：`config create` 的版本查询默认超时 10→30 秒
（runtime_verification.query_version），Windows `runtime prepare` 维持冻结
profile 的 10 秒不动（改 profile 需固定归档重生成资源）；契约文档同步，新增
回归断言预算 ≥30 秒且超时仍映射 engine_version_query_failed。

2026-10-13 向导改为 MyGo 原生 UI 窗口（阶段二，分支 feat/first-run-onboarding）：
用户手动验收结论 web 向导交互差（裸命令 textarea、阶段枚举直出、无整体进度
感），方向确认为原生 UI。新增 `studio/internal/wizard`（Manager + viewState
直调 core，六站步骤轨：准备/设备检查/候选/启动与绑定/运行/报告核验；命令仅
在服务启动步出现且只读可复制；阶段全部人话；失败给重试不自动重试；token 只
接受环境变量名）；main.go 在 readiness 未完成或 CLI 未安装时自动开向导窗口，
`mygo.Bind(WizardBridge)` + `mygo generate` 产出 TS client（bindings 路径
web/src/mygo.ts，新增 mygo-runtime 依赖）；web 向导页降级为入口卡（进度人话
摘要 + 打开向导按钮，mock 模式不打包 mygo 代码），移除命令/进程/endpoint 表
单。协调者修正：Go 侧不再改写 progress next_command 的 --out（CLI new_output
已保证新目录，改写破坏 stem-N 产物命名），Bind 移至 App.Run 前顶层（generate
模式不执行 WhenReady）。验证：go vet/test（含 ui.NewTester 五状态 view 测试）、
build:web、verify.mjs（mock 与 wire 全过）、mock 产物无 mygo 代码、首跑/配置后
两态桌面冒烟进程稳定。未验证（留给用户手动验收）：原生窗口视觉呈现、入口卡
点击聚焦向导、完整真机向导流程重跑。
