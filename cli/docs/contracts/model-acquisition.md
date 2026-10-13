# 模型获取契约

`model acquire` 是显式准备命令，接收一个固定 revision 的单 GGUF 链接，
像 `curl -O` 一样流式写入一个新目录，并记录实测字节数与 SHA256。
参数、URL 规则和退出码见 [CLI 参考](../cli-surface.md#各动作的冻结行为)，
简化决定的理由见 [ADR 051](../decisions/051-simplified-model-acquisition.md)。

## 超时与失败

默认 `--timeout-seconds 1800`，作用于各网络操作（连接、读取、写入）；
必须为正有限数，布尔值、零、负数、NaN 和 Infinity 拒绝。
没有总期限、读取监督或收尾期限；失败后不自动重试或续传，失败目录保留且不能复用。
取消保留退出 130。

失败原因固定为 `invalid_model_source`、`model_acquire_incomplete`、
`output_exists`（退出 2）、`io_error`（退出 4）、`cancelled`（退出 130）。
stderr 只输出固定原因，不输出 URL、请求头、响应正文或凭据。

## 下载与发布

获取客户端使用 `trust_env=False`，忽略 `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY`
等环境代理，也不读取系统代理。跟随重定向；`--token-env` 的值只放在初始请求的
`Authorization` 头中，跨 origin 重定向时由 httpx 自动剥离，不转发给 CDN。
token、请求头和响应正文不输出或持久化。

请求声明 `Accept-Encoding: identity`；响应状态非 200 或内容编码非 identity 均拒绝。
正文流式写入 `文件名.part`，flush/fsync 后普通 rename 发布；
`source.json` 以同样方式先写 `.part` 再发布。失败清理 `.part`，保留目录。

## 来源记录

新 `source.json` 使用 `definition="remote_model_source.v3"`：
`parser`、`platform`、`repository`、`revision`、`path`、`bytes`、`sha256`、
`retrieved_at`。parser 保持 `remote-model-source-parser.v1`。
`bytes` 与 `sha256` 从实际下载的字节流计算，不再请求远程元数据清单，
也不再保存 `metadata_sha256` 与 `acquisition_budget`。

旧 v1/v2 记录严格只读：v1 保留 `metadata_sha256`，v2 另含 `acquisition_budget`
（恰含 `total_seconds`、`read_seconds`、`cleanup_seconds`、`max_bytes`）；
缺键、多键、非法值均拒绝，不补写、不转换。未知定义拒绝。
候选生成只读取显式 `--model-source` 指定的记录，核验当次模型字节数和 SHA256，
并拒绝与显式 repo/revision 的冲突。来源记录错误保持
`model_source_metadata_mismatch` / 2。支持集合见[格式矩阵](inferyard-current-format.md#格式支持矩阵)。
模型获取成功不证明可加载、服务就绪或完整量化血缘；解释遵循[血缘规则](../data-contract.md#证据血缘与比较结论)。
