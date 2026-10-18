# 交付目录组织

[返回 Backlog](../backlog.md)

## 交付目录组织（#28）

2026-10-11 修复 [#28](https://github.com/sunny0826/inferyard/issues/28)：
Windows Bonsai 真机测评的完整工作区含 1,287 个文件，正式产物与初始化检查、
操作员脚本、服务日志、截图和 4 个 Chrome profile 混放，端到端交接只核验了
正式 run 与报告，未核验完整交付目录清单。[使用指南](../usage.md#工作区与交付目录组织)
新增交付组织规则：11/19 为正式输出计数，与工作区总文件数分开表述，不宣称
"每次测评只新增 N 个文件"；交付目录只复制封存 run 与共享报告并保持相对布局，
交接清点完整交付目录；浏览器 profile 放交付树外的专用临时目录，检查后只回收
任务自建临时数据；准备与操作员证据留在原工作区分开存放。
README、README.zh-CN、CHANGELOG 同步计数限定。纯文档改动，本地链接/锚点检查通过。

2026-10-11 二次修订（用户决策：整体移除浏览器 profile 环节）：核实评分链路后
确认 benchmark 执行与评分从不使用浏览器——`zh-svg-pelican` 题 `rules` 为空、
不做质量评分，CLI 仅做 SVG XML 语法与大小检查，动画效果无自动判定；上轮的
"profile 放交付树外临时目录"规则仍默认要创建隔离 profile，为操作员脚本留下
口子。[使用指南](../usage.md#工作区与交付目录组织)改为：人工检查用日常浏览器直接打开
report.html，不创建隔离 profile、不编写浏览器检查脚本，任何情况下（含
`--user-data-dir` 指向工作区内部目录的浏览器调用）都不在 benchmark 工作区内
创建或存放浏览器用户数据；确需隔离环境时用户数据放系统临时目录。工作区
预期文件量回到正式输出 19 + 准备/操作员辅助产物量级，不再出现千级 profile
文件。候选增强（未实施）：报告内对 SVG 输出做动画元素统计展示，进一步减少
打开浏览器的需求。

独立挑刺审查（挑刺者会话，只读）确认四项核心事实主张全部成立（评分链路无
浏览器调用、SVG 免质量评分、仅 XML 语法与大小检查、无动画自动判定），并
指出一处阻断残留与三处收紧：usage.md 运行与报告一节残留的"浏览器验证文件"
合法辅助产物表述已删除；交付树内同样禁止浏览器用户数据且不属于可登记的
可选新增；"确需隔离"出口限定为访客/隐身窗口或一次性系统临时目录
`--user-data-dir`，同样不编写自定义检查脚本；CHANGELOG 英文措辞同步消除
"isolation profiles 一律禁止"的歧义。

2026-10-11 三次修订（用户决策：准备与操作员辅助产物移出 benchmark 工作区）：
核实准备链路后确认候选/绑定等准备目录可整体外置——全部准备命令 `--out`
接受任意新目录，`config create --results` 不允许位于候选目录内
（community_candidates.py:97），run 目录自含冻结输入，verify/report 不依赖
工作区外的准备目录，`report --out` 自动创建父目录
（report_common.py:81）。[使用指南](../usage.md#工作区与交付目录组织)定义双目录
布局：操作目录（如 `bench-ops/`）承载 init 导出、模型、preflight、候选、绑定、
操作脚本、日志、截图、导出与传输准备；benchmark 工作区（如 `bench-work/`）
只存放 run/batch 与报告，由 `run` 和 `report` 自动创建。操作目录中的证据与
日志原样保留，不为凑数删除（issue #28 验收要求）。README、README.zh-CN、
installation、cli-surface 的示例路径同步改为 `bench-ops/` 准备 + `bench-work/`
正式产物；节名由"交付目录组织"改为"工作区与交付目录组织"并同步全部锚点
引用。此布局为文档约定，CLI 不强制；历史工作区（含 Windows 1,287 文件原工作
区）不迁移不改写。约 131 个非 profile 辅助文件的逐文件归属清单因 San3an-win
掉线暂未枚举，类别层面结论不依赖该清单，待机器恢复后可另行核对。

真机重组由 San3an-win 独立会话（Codex auto-review）完成：原工作区
`D:\bench-work\bonsai-597367d-after-release-20261011-112436` 未改动，重组前后
递归 1,287 文件、全部文件大小与 SHA-256 一致；两个封存 run 与共享报告以
`shutil.copy2` 复制到 `D:\inferyard\deliveries\bonsai-597367d\`，恰好 19 文件
（2×8 证据 + 3 报告）且与原件逐字节一致。报告的 `../work/results/...` 相对
来源引用保持有效，未使用 `--source-root`；入口为源码 venv 的 inferyard 0.0.1
（Python 3.14.7）。报告、core run、SVG run 三次 `verify` 均退出 0
（bytes/sources/semantic verified，两 run sealed，120/120 与 1/1）。
本次未重渲染 HTML（`render_checked=false`）。首次 SVG 核验因 Windows GBK
控制台无法输出自行车 emoji 退出 1，设置 `PYTHONIOENCODING=utf-8` 与
`PYTHONUTF8=1` 后三次全部通过；该控制台编码问题与证据无关，另行开 issue 跟踪。
未启动模型服务、发送模型请求或联网下载。

2026-10-11 候选增强实施（信息性动画元素统计）：report.html 渲染期对 SVG 题的模型
输出（报告内唯一一份 `contents` 文本）现算静态动画元素统计——SMIL 动画元素
（animate / animateTransform / animateMotion / set，含 SVG namespace 形式）计数与
`<style>` 文本中 `@keyframes` 规则、`animation` 属性声明的静态文本匹配计数（不做完整
CSS 解析）——在 SVG 画廊与逐题详情各显示一行，措辞明确标注信息性、不构成动画判定、
质量评分或资格结论。统计仅在渲染时计算：不写入 index.json、不进入封存证据、不改变
report_format_version=8 与 schema_version=3，同输入结果确定；SVG not_found / too_large /
malformed 时计数为 null 并附原因（svg_not_extracted / svg_too_large / svg_malformed），
不以 0 冒充。实现位于 reporting 层（`svg_render.animation_stats` + 渲染环境 Jinja 过滤器
+ 模板宏），无 CLI、参数、退出码或契约字段变化。usage.md"动画效果没有自动判定"结论
不变；模板字节变化由 `template_sha256` 随生成自然反映，旧报告不受影响。单测覆盖
SMIL（含 namespace/裸名）、@keyframes、animation 简写与长名排除、无动画真实 0、三类
缺测 null+原因；集成测试断言统计不进 index.json、HTML 展示正确且 `--rerender` 通过。

2026-10-11 四次修订（用户决策：操作目录全清理 + 双目录同步补全）：独立挑刺审查
b0f8b1b 后修复两处阻断——MACOS.md、WINDOWS.md（issue #28 的平台指南）、
configs/README.md 补同步双目录布局；usage.md 浏览器禁令收回为"benchmark 工作区或
操作目录"（原"任何目录树"单方面扩大了 3df4ac6 定案范围且与系统临时目录例外
自相矛盾）。init 导出的入门说明（data/community/README.md）改称操作目录，
`resources.json` 经 `scripts/export_community.py`（`generated(None)`）再生且
`--check` 通过。按用户决策新增清理规则：本轮全部 run 封存、汇总报告 verify 退出 0
后，操作目录本轮内容可整体清理——封存证据自含冻结输入，verify/report 此后不读
bench-ops；清理为操作员显式手动操作，CLI 不自动删除，补跑重走准备流程（PID 变化
后本须重新绑定）。审查建议同步落实：安装指南说明骨架空 `results/` 不使用、
`--results` 指向工作区；README 与安装指南的自动创建措辞补 `batch`；文档地图不再
称安装指南含"工作区"；CHANGELOG"任意 --out 路径"修正为"父目录须已存在的新目录"。
San3an-win 已恢复在线，131 文件逐文件清单核对仍未执行，与 PR 无关可后续进行。


