# lab_observation 协议夹具

本目录是 [T0-A](../../../docs/contracts/windows-ninfer-adaptation-t0.md) 冻结的 `lab_observation.v1`
项目接口的**小型合成协议 mock**，全部内容手工编写，不来自任何真实引擎或发布二进制。
`/lab/v1/*` 路由尚无获证实的引擎实现；这些夹具只用于解析与拒绝路径的单元测试，
不构成生产 idle、Windows 或真机通过证据。

ID 约定：实例 ID 为 `11…11`（32 个小写十六进制），请求 ID 为 `22…22`，
SHA256 字段为 `aa…aa` / `bb…bb`（64 个小写十六进制）。
