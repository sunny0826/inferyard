# ADR 051：模型获取简化为 curl 级下载

状态：Accepted。日期：2026-10-12。取代 [ADR 049](049-model-acquisition-budgets.md)。

## 问题与决定

`model acquire` 此前为一次下载维护了直接子进程、IPC 进度协议、父进程监督、
远程元数据预核验、手工重定向白名单、三段预算和排他原子改名，约 700 行。
操作者实际需要的能力与 `curl -O` 相同：把一个固定 revision 的 GGUF 存进新目录。

按操作者决定，下载简化为进程内流式 GET：

- httpx `follow_redirects=True` 取代手工重定向策略与签名 query 白名单；
  `Authorization` 只放在初始请求上，httpx 在跨 origin 重定向时自动剥离。
- 不再请求远程元数据清单；字节数和 SHA256 从实际下载的字节流计算。
- 三个超时参数收敛为单个 `--timeout-seconds`（默认 1800，正有限数），
  作用于各网络操作；不再有总期限、读取监督、收尾期限和失败诊断协议。
- 取消 20 GiB 上限、磁盘余量预检和 ctypes 排他改名；发布为 `.part` + 普通 rename。
- 保留输入契约：HF/ModelScope 固定 revision 的 `.gguf` blob 链接白名单、
  `--token-env` 只接受环境变量名、输出必须是不存在的新目录。

## 来源记录与兼容

新生产记录使用 `remote_model_source.v3`，字段为 `parser`、`platform`、`repository`、
`revision`、`path`、`bytes`、`sha256`、`retrieved_at`；不再含 `metadata_sha256` 和
`acquisition_budget`，parser 保持 `remote-model-source-parser.v1`。
v1/v2 记录严格只读，不补写、不转换；v2 预算键值仍按原规则校验。
核心 `schema_version=3` 不变。字段及拒绝路径见[模型获取契约](../contracts/model-acquisition.md)。

## 取舍

- 远程预核验能在下载前发现大小/哈希不符，但要求维护两个平台元数据 API 的
  解析与固件；下载后实测哈希已满足候选绑定的血缘需要，故移除。
- 子进程隔离能强制回收卡住的传输，但进度协议本身是主要的读者负担；
  进程内流式读取配合 httpx 超时覆盖同一失败面。
- 失败诊断协议（固定阶段/异常分类/进度）随之删除；失败原因与退出码不变。

## 验证边界

软件验证覆盖 URL 拒绝、token 边界、回环 HTTP 成功/重定向/404/已存在目录、
v1/v2/v3 记录读取和 CLI 全链路 e2e。回环覆盖不授予真实 CDN 性能资格；
`config create --model-source` 的血缘绑定语义不变。
