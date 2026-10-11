# 模型获取契约

`model acquire` 是显式准备命令，接收一个固定 revision 的单 GGUF 链接，
核对远程清单、字节数与 SHA256，写入一个新目录。
参数、URL/重定向规则和退出码见 [CLI 参考](../cli-surface.md#各动作的冻结行为)，
预算与诊断的理由见 [ADR 049](../decisions/049-model-acquisition-budgets.md)。

## 预算与进度

默认总期限 1800 秒、网络读取期限 180 秒、失败收尾期限 60 秒。
操作者可用 `--timeout-seconds`、`--read-timeout-seconds`、`--cleanup-timeout-seconds`
分别修改；必须为正有限数，不以默认值封顶。布尔值、零、负数、NaN 和 Infinity 拒绝。
文件大小上限仍为 20 GiB，声明大小加临时开销必须满足所在卷可用空间。

总期限不因新数据续期。读取期限基于实际网络进展，不基于 `.part` 可见大小或空闲心跳；
持续收到小块数据不等于读取停滞。父进程可独立停止卡住的子进程，并在收尾期限内回收。
本地写入与发布受总期限约束。取消保留退出 130，回收失败为工具错误。
父进程信任随包 worker 报告的网络/本地阶段切换；它不是任意第三方 worker 的监管协议。
随包 worker 不发送空闲心跳，重复进度通知不能延长读取期限。

失败进度区分已写入模型字节、经过秒数和平均写入速度，速度单位为 bytes/s。
写入计数不代表已经 fsync、完整发布或可用于测评；失败目录不能复用。
父进程强制停止时使用最后收到的写入计数，不能保证包含尚未报告的写入。
失败 `details.diagnostics` 与 stderr 提供 `stage`、`exception_class`、`bytes_written`、
`elapsed_seconds`、`average_bytes_per_second` 和 `progress_reason`。
未收到子进程进度时，字节数和速度为 `null`，原因为 `worker_progress_unavailable`；
已知进度的 `progress_reason` 为 `null`。阶段仅为 `preflight`、`metadata`、`redirect`、
`download-open`、`download-body`、`publish`；异常分类来自固定允许列表。
公开诊断只含固定阶段、分类和有界数值，
不包含 URL、原始异常文本、请求头、响应正文或凭据。

## 来源记录

新 `source.json` 使用 `definition="remote_model_source.v2"`，保留 v1 的全部来源字段：
`parser`、`platform`、`repository`、`revision`、`path`、`bytes`、`sha256`、
`metadata_sha256`、`retrieved_at`。parser 保持 `remote-model-source-parser.v1`。
新增必需对象 `acquisition_budget`，恰含 `total_seconds`、`read_seconds`、
`cleanup_seconds`、`max_bytes`，值为本次实际使用的预算。

例如一次显式增加总时限的预算为：

```json
{
  "total_seconds": 14400,
  "read_seconds": 180,
  "cleanup_seconds": 60,
  "max_bytes": 21474836480
}
```

这是预算对象示例，不是独立完整来源记录。
缺任一预算键、多余键、非正或非有限秒数、布尔数值、非整数或超上限 `max_bytes`
均拒绝；v2 缺预算不能退回 v1。完整合法/非法记录由来源读取回归覆盖。

旧 v1 仅接受原字段集合，预算未知，不补写、不转换；未知定义和额外字段拒绝。
候选生成只读取显式 `--model-source` 指定的记录，核验当次模型字节数和 SHA256，
并拒绝与显式 repo/revision 的冲突。来源记录错误保持
`model_source_metadata_mismatch` / 2。支持集合见[格式矩阵](inferyard-current-format.md#格式支持矩阵)。
模型获取成功不证明可加载、服务就绪或完整量化血缘；解释遵循[血缘规则](../data-contract.md#证据血缘与比较结论)。

## 网络路由

获取客户端使用 `trust_env=False`，忽略 `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY`
等环境代理，也不读取 Windows 系统代理。仅打开系统代理不足以改变这条连接。
若网络必须经过本机代理，需由操作者预先配置能接管直接连接的 TUN/全局路由，
并验证仓库及 CDN 都可达；InferYard 不配置网络，也不能保证具体 TUN/Fake-IP 实现的行为。

当前不提供显式代理参数，原因见 ADR 049。`Authorization` 只发送给声明的仓库主机，
不转发给 CDN；任何路由方式都不能放宽现有重定向与哈希检查。
