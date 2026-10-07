# ADR 015：LM Studio 本机 CLI 身份

状态：Accepted

## 决策

LM Studio 观察器固定连接 `127.0.0.1` 与合法显式端口。
`lms ps --json --port PORT` 和 `lms link status --json --port PORT` 不传 `--host`，
使用本机 CLI 身份，同时保留同账户、唯一监听、模型文件和 CLI 哈希核验。

## 理由与代价

指定 `--host` 会切换为远程 CLI 身份，可能缺少 Link 查询权限；省略端口又可能触发发现或启动 llmster。
显式端口且不传 host 可观察已有本机服务，不授予远程权限、不读取或持久化 CLI 凭据。
依据为[固定构建的 createClient 源码](https://github.com/lmstudio-ai/lms/blob/69d945a/src/createClient.ts#L95-L142)。

字段定义保持，非法地址或端口在 CLI 调用前拒绝。接口见[常用引擎契约](../contracts/engine-fit-contract.md#常用引擎扩展)。
