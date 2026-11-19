# 交付目录组织

[返回 Backlog](../backlog.md)

## 交付目录组织（#28）

当前交付步骤、相对布局及清理前提见[使用指南](../guides/onboarding-and-scope.md#工作区与交付目录组织)；报告字段与读取边界见[报告契约](../contracts/report-contract.md)。以下是 [#28](https://github.com/sunny0826/inferyard/issues/28) 在 2026-10-11 的 Windows 历史回执，使用当时的三文件 HTML 报告。

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

待核对原工作区约 131 份非浏览器辅助文件的逐文件归属；GBK 控制台输出失败另行跟踪。该历史清单不作为当前 `studio.json` 交付的文件数要求。
