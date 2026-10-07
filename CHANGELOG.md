# 变更记录

## 0.0.1 — 本地候选，未发布

- 以 InferYard 名称开源，Python 包和 CLI 统一为 `inferyard`；保留既有证据协议、主机锁及历史模板兼容。

首个面向公开分发整理的版本。版本号不改变活动数据契约 `schema_version = 3`。

- Python CLI 执行冻结题包、保存逐题答案、确定性评分、客户端计时和平台资源证据。
- 单次与批量计划、取消与恢复、显式重跑；模型服务由操作者外部启动。
- 自包含离线 HTML 报告、比较、重评分、导出、公开包及原始证据核验。
- 随包提供题包、平台模板和 Windows 固定 runtime profile；支持 `init`、`config assets/create/bind` 与 `runtime prepare`。
- 支持从本地 wheel 使用 `uv tool` / `uvx`；清理过期文档、设备配置和历史工具，`validation/` 脱离 Git 工作树。
- 发行准备采用 `community_distribution.v2`，绑定构建清单与实际源码外安装结果 `installed_safe_checks.v2`；至少验证一个平台，其余标为 `not_verified`，核验后暂存同一份字节。
- 发布 workflow 仅手动触发，默认 `verify-only`，准备同候选字节的 GitHub Release/PyPI 路径；尚未执行上传。

项目采用 [MIT 许可证](LICENSE)。[平台状态](docs/platforms.md)列出实现与验证边界，候选发行物及安装检查由[发布待办](docs/backlog.md)跟踪；本条不表示已上传 PyPI 或完成公开发行。
