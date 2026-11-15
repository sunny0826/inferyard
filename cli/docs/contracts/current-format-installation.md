# 安装检查与资格版本

[返回当前格式契约](inferyard-current-format.md)。

当前安装结果为 **installed_safe_checks.v4**，与
[ADR 059](../decisions/059-studio-json-only-report.md) 的 Studio JSON-only 报告同步升版。
v3 及更早结果不被接受为新能力证明，旧候选中的安装 v3 明确失败，双方不默默继承资格。

- 探针用当前 writer 生成 `studio.json` 并用已安装 CLI 的 `--schema studio_report` 输出校验实际字节；
  记录 Schema 的 `$id` 与规范 JSON（排序键、紧凑分隔符、UTF-8）的 SHA-256，不查找 wheel 内静态 Schema 副本。
  仓库导出的 `cli/schemas/studio_report.schema.json` 只服务源码/E2E gate。
- 对报告目录的 `verify` 期望退出 4，且完整 limitations 恰为 `["studio_report_not_verifiable"]`；
  旧 report v1–v8 目录 `verify` 期望退出 2 / unsupported_format，supported 为空。
  不以篡改当前字节后的哈希错误冒充旧版本拒绝。安装/第二环境/回退三个探针都执行新范围。
- 探针输出含 `current_studio_report_kind=studio-report.v1`、
  `unsupported_report_formats_rejected=[1..8]`、wheel 内 `inferyard/` 成员计数与 tool/scorer 摘要；
  逐版失败不能被总 returncode=0 掩盖。
- v4 的 scope 为 `offline_core`、`workspace_init`、`resources_identity`、`studio_json_schema`、
  `current_format_json`、`unsupported_format_rejection`、`uv_uvx_isolation`、`synthetic_upgrade_rollback`。
  测试结果仍是零模型请求，host_lock_tests=not_run。
- `validate_installation` 只接受 v4：要求三次探针 stdout 摘要、`studio_report_schema`（`$id`/argv/实际
  stdout 摘要/规范 JSON 摘要）与检出源码 `export_schema("studio_report")` 一致、`studio_json` 摘要与
  verify 退出记录完整、`wheel_sha256` 绑定已核验 wheel；不能只写新 scope 或只检查探针退出 0。
- run_installed 与 release_common 的 writer/validator、prepare/verify_release_candidate 消费链及夹具同步；
  明确拒绝 v1–v3 结果、旧 scope、缺负例、缺当前成功、来源/字节失配。不把旧结果改个 kind 即视作重验。
- community_distribution.v2 的 build/candidate 外壳和安装结果摘要引用结构不变，故不连带升版。
  至少一个平台重新完成同批字节安装，其余保持 not_verified。

安装验证语义升版不改变产品版本、核心 schema 3 或业务协议编号。
安装安全探针不操作系统真实锁；锁互斥与 dirty 持久化证明由隔离临时根的进程测试提供。
