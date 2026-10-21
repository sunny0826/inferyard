# 安装检查与资格版本

[返回当前格式契约](inferyard-current-format.md)。

清理前的 [installed_probe.py](../../tests/packaging/installed_probe.py) 在三次安装探针中生成 report v1–v6，
并计算 v1–v7 模板摘要；[release_common.py](../../scripts/release_common.py) 的旧 SCOPE 包含
`synthetic_historical_reports`，旧 `validate_installation` 要求 installed_safe_checks.v2。
这不是可原样保留的当前安装资格。

- 探针改用当前 writer 生成 report7、verify 和显式 rerender；只核验当前模板摘要。
  对 v1–v6 构造最小旧格式负例，通过 CLI 确认退出 2 / unsupported_format；不打包旧 renderer。
  不以篡改当前 seal 后得到的哈希错误冒充旧版本拒绝。安装/第二环境/回退三个探针都执行新范围。
- 探针输出改为 `current_report_formats_verified=[7]`、
  `unsupported_report_formats_rejected=[1,2,3,4,5,6]`、`template_hashes={"7": ...}`；
  删除 synthetic_historical_reports_verified，逐版失败不能被总 returncode=0 掩盖。
- 安装结果 **应升级为 installed_safe_checks.v3**。scope 保留原六项，将
  synthetic_historical_reports 替换为 `current_format_reports` 和 `unsupported_format_rejection`。
  既有 probe 命令名可保留，执行语义由结果 v3/scope 绑定；测试结果仍是零模型请求，host_lock_tests=not_run。
- v3 新增必需 `report_format_checks` 对象，恰含 probe-install-a、probe-install-b、probe-rollback 三键；
  每项保存对应探针实得的 current_report_formats_verified、unsupported_report_formats_rejected、template_hashes。
  run_installed 先校验三次 stdout 再汇总；validate_installation 要求三项都完整，模板摘要与已核验 wheel 的当前模板一致，
  不能仅写新 scope 或只检查探针退出 0。
- run_installed 与 release_common 的 writer/validator、prepare/verify_release_candidate 消费链及夹具同步；
  明确拒绝 v1/v2 结果、旧 scope、缺负例、缺当前成功、来源/字节失配。不把旧结果改个 kind 即视作重验。
- community_distribution.v2 的 build/candidate 外壳和安装结果摘要引用结构不变，故不连带升版；
  新候选仅接受安装 v3，旧候选中的安装 v2 明确失败。旧核验器也会因不认识安装 v3 而失败，
  双方不默默继承资格。至少一个平台重新完成同批字节安装，其余保持 not_verified。

这是安装验证语义改变所需的单独升版，不改变产品版本、核心 schema 3 或业务协议编号。
安装安全探针不操作系统真实锁；锁互斥与 dirty 持久化证明由隔离临时根的进程测试提供。
