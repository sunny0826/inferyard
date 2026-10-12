# 引擎获取契约

`engine acquire` 是显式准备命令，接收一个 HTTPS 链接，像 `curl -O` 一样把单个
引擎归档或二进制流式写入一个新目录，并记录实测字节数与 SHA256。参数、URL
规则和退出码见 [CLI 参考](../cli-surface.md#各动作的冻结行为)；设计取舍与
[ADR 051](../decisions/051-simplified-model-acquisition.md) 的模型获取简化一致。

## 与既有准备入口的边界

`engine acquire` 只下载：不解压、不装配、不启动服务、不核验 profile，也不读写
主机锁或 dirty 状态。macOS 候选仍要求 `config create --engine` 收到引擎二进制
及同目录动态库并执行一次有界 `--version`；Windows 归档下载后仍走
`runtime prepare --archive` 的固定身份核验，两个入口的职责不变。引擎身份由
`config create` 层的 `binary_sha256` 与 `runtime_library_manifest` 冻结，
本命令的记录只是获取回执，不参与血缘绑定。

## URL 规则与失败

`--source` 只接受 `https://HOST/PATH` 形式：ASCII、无控制字符、无 query、
fragment、userinfo 或端口，路径分段无穿越与非法字符，基名即输出文件名。
不限 host 与扩展名——引擎分发源由操作者选定并自行确认可信。无 token 选项；
公开下载不引入凭据面。

默认 `--timeout-seconds 1800`，作用于各网络操作（连接、读取、写入）；
必须为正有限数，布尔值、零、负数、NaN 和 Infinity 拒绝。没有自动重试或
续传；失败后目录保留且不能复用。取消保留退出 130。

失败原因固定为 `invalid_engine_source`、`engine_acquire_incomplete`、
`output_exists`（退出 2）、`io_error`（退出 4）、`cancelled`（退出 130）。
stderr 只输出固定原因，不输出 URL、请求头或响应正文。

## 下载与发布

获取客户端使用 `trust_env=False`，忽略环境与系统代理；依赖本机代理的网络
需要 TUN/全局路由接管直连。跟随重定向。请求声明 `Accept-Encoding: identity`；
响应状态非 200 或内容编码非 identity 均拒绝。正文流式写入 `文件名.part`，
flush/fsync 后普通 rename 发布；`engine-source.json` 以同样方式先写 `.part`
再发布。失败清理 `.part`，保留目录。

## 来源记录

新记录使用 `definition="remote_engine_source.v1"`：`parser`
（`remote-engine-source-parser.v1`）、`source_url`（规范化 HTTPS 链接）、
`bytes`、`sha256`、`retrieved_at`；`bytes` 与 `sha256` 从实际下载的字节流
计算。记录无独立 JSON Schema，字段集由解析器冻结。引擎下载完成不证明
可加载、服务就绪或候选生成成功。

## 验证边界

软件验证覆盖 URL 拒绝、回环 HTTP 成功/重定向/压缩拒绝/404/已存在目录、
超时参数和 CLI 懒性回归（对称模型获取的回环套件）。2026-10-12 已有一次
macOS arm64 真机闭环回执：GitHub release 真实下载经装配后驱动完整
benchmark 链（见本地 `validation/` 目录，不外推其他平台或 CDN 性能）；
回环测试保留软件回归定位。Windows `runtime prepare` 的真实归档核验与
源码外安装验证不在本次回执范围内。
