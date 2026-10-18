# 应用内存诊断

[返回 Backlog](../backlog.md)

## 新功能：应用内存诊断

2026-10-11，issue #25 Windows 能力基于 `597367d` 实施。原基线 Windows 的退出 2 是
预期平台限制。新增独立 `memory_check.windows.v1`，保留 macOS v1 字段与读取支持。
Windows 清单分开报告 working set/private commit、TokenUser 账户关系/session 缺测；
EXE 文件/账户/session 分组仅作观察，protected/unknown，不给出终止建议。
首次原生公开 CLI 枚举 246 PID；最终源码再次枚举 250 PID，无截断，186 项必需检查缺测
（150 项所有者/session 不可得，148 项 working set/private commit 缺测，38 项父关系缺测，
这些计数有重叠），退出 3；69 protected、181 unknown、0 review，采集约 0.518 秒。
显式保护已核验，保存读回相等，重复目录退出 4 且原件哈希不变，无效 PID 退出 2。
相关软件回归 356 passed / 3 skipped；跳过两项 POSIX 管道测试及 macOS 原生诊断。
超时测试复现 Windows venv 启动器 PID 与实际解释器 PID 不一致，改为直接持有基础解释器
进程句柄并继承模块路径；实际 worker PID/自有进程退出断言通过，不再只回收启动器。
macOS 归组/旧格式读回/拒绝路径模拟通过；Windows TokenUser/session/FILETIME 本机读取通过。
Ruff check/format、Schema/catalogue 一致性通过。中文 Windows 测试使用 `PYTHONUTF8=1`，
避免旧共享夹具默认 GBK 解码；不改其业务源码。
这不是历史 fallback 的复用，也不构成 benchmark/安装包或 macOS 原生资格。
原始 JSON、退出码/耗时、版本和软件回归日志保存在本机独立临时目录，交付报告提供定位。
多账户、实际 PID 复用、原生交互取消与源码外安装等未验证项见[剩余计划](../plans/memory-check.md)。

2026-10-11，PR #19 Bugbot 的父身份查询失败问题已复现并修复：父关系缺测或变化仅清空父边，
保留核验过的自身身份/RSS；受影响对象 unknown，不降低无关可读应用的 review。
新增可选 `parent_identity_missing_reason`，旧 v1 文件仍可读取；缺测计数包含父关系失败，仍退出 3。
注入父查询失败（前/后）与父代际变化的 3 项回归先失败、修后通过；相关软件回归 269 项通过，
覆盖保存读回/退出 3、坏父边及缺测计数拒绝、旧样例读取；Ruff、Schema/catalogue 一致性通过。
真实 macOS CLI 枚举 645 PID，无截断，207 项缺测，退出 3，保存读回一致；
该次未出现父关系缺测，竞态修复由上述注入回归验证，未声称真机复现了竞态。
原始失败/通过日志在 `/tmp/memory-parent-before.log` / `/tmp/memory-parent-final-tests.log`；
原生回执位于仓库外 `/var/folders/7r/gjhs3r216gvgl1yq_pr2nvxh0000gn/T/inferyard-parent-review-xe2pilyx/`。

2026-10-10，MC-1a 已在 `feat/memory-check-macos` 实施，基线 `515c599`。
`inferyard memory-check [--protect-pid PID] [--out NEW_DIR]` 默认输出只读 JSON，
按可信 bundle/稳定父身份聚合应用，输出 protected/unknown/review 及原因；
不自动退出应用、不读取完整命令行或环境、不提供 safe_to_kill 或释放量保证。
规则见[诊断契约](../contracts/memory-check-contract.md)；Linux MC-1b 与 Windows 剩余原生验证
继续见[后续计划](../plans/memory-check.md)，Linux 仍返回不支持平台/退出 2。

采集冻结为 5 秒 deadline、4096 PID 上限，在独立模块子进程读取，超时/取消只回收自有采集器。
最终 macOS 五次扫描均观察全部 624–627 PID，其中 223–226 项缺测，退出 3；无截断，
不将权限缺测计为完整成功。采集耗时中位数 165.20 ms（153.40–170.07），
启动至退出 wall 中位数 232.62 ms（215.51–248.14），`time -l` 进程峰值 RSS
中位数 34.20 MiB（34.09–34.34）；该指标不是父子同时驻留总内存，不宣称提速或全机上界。
本机观察支持预算选择，不代表其他机器或未来平台成本。

原生自有应用场景验证了多实例/helper（4 个成员）、同名副本（另 2 个成员）、显式保护、
保存读回、重复目录拒绝及自然退出后刷新。进程上限截断原生退出 3；
超时/进程组 Ctrl+C 为注入场景，取消 130 且自有采集器已回收。
最新版 wheel 在源码外固定依赖环境通过帮助、Schema 和真实清单/保存检查。
基线/候选 device-check 均退出 0、kind 保持 device_preflight.v1；帮助各 5 次交替观察，
不据此宣称性能不退化。合法样例包含三态，非法样例经过离线读取边界拒绝。

本轮全量软件回归 **4796 passed / 53 skipped**；53 项为平台或显式启用限制，未计为通过。
旧 CLI 定向回归 240 项、最后离线拒绝路径修复后新诊断定向回归 60 项通过；Ruff check/format、Schema/catalogue
一致性及本地链接/锚点检查通过。Lody“挑刺者”源码审查 approve；按其建议收窄系统路径，修复 /usr/local 自装工具误保护，
先失败复现、修后通过。helper 显示名建议未复现（始终取已分配 bundle 名）；进程组取消已有
仓库外真实注入回执，尚未固化为信号回归。后续完整回执确认另一项为理论窗口：
SIGINT 落在 Popen 返回与进入 try 之间时，自有采集器可能短暂成为孤儿；
该创建窗口未专项验证，不将其写成通过。所有本机原件位于仓库外
`/tmp/inferyard-memory-mc1a-kg_o9862/`（`accepted/` 最终原生场景与最新 wheel/安装，
`final/` 回归与取消/期限边界）；临时目录不是发行或长期证据仓库。
没有启动模型服务、发送模型请求、关闭用户应用或改写旧 run。
2026-10-11 用户授权提交 PR：同步 main 后保留双方 CLI/请求类型与计划索引，
相关内存/CLI/模型获取回归 362 项通过，Ruff、格式及 Schema/catalogue 一致性通过。

