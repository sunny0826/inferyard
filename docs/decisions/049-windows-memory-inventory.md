# ADR 049：Windows 内存诊断使用独立 kind

状态：Accepted。

## 背景

issue #25 为 macOS-only memory-check 增加 Windows 能力。Windows 的 working set、
private committed bytes、TokenUser SID/session 与 macOS bundle/uid 不是相同观测。
直接放宽旧平台/分组枚举或把 private bytes 写入 RSS 会混淆旧证据语义。

## 决策

保留 macOS `memory_check.v1`，增加 `memory_check.windows.v1`；公开 Schema 是两个结构的
联合，核心 schema_version 仍为 3。Windows 新增 private_commit 与 ownership，独立核验
缺测和汇总。现有 macOS 文件无需新字段，仍按原件读取；平台/kind 混用拒绝。

独立 Windows worker 负责有界只读采集，复用已固定 psutil 和原生 FILETIME 查询。
与把 macOS selector/bundle 代码扩成通用后端相比，独立模块减少平台耦合。
按 EXE 文件、所有者、session 的分组仅用于查看，不推断 GUI 用途；输出 protected/unknown，
不赋予 review。所有者未知保留内存证据但不建立 EXE 组。只终止自有采集 worker。

## 验证与后果

先采集/数据形状，再保护/CLI，再软件拒绝路径及 Windows 原生只读清单验证。
预算、PID/父身份复用与取消使用可控注入，不操作用户进程；macOS 真机须另行验证。
原生不同权限、跨账户、安装包及未完成场景留在[后续计划](../plans/memory-check.md)。
当前结果见 [backlog](../backlog.md#新功能应用内存诊断)，完整语义见
[诊断契约](../contracts/memory-check-contract.md#windows独立来源与保守分组)。
