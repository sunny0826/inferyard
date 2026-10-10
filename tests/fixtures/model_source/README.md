# 固定远程清单固件

以下公开响应于 2026-10-10 录制，测试只读取本地 JSON；不下载模型。
原字节保留用于 `metadata_sha256` 检查，不包含请求头或凭据。

- `huggingface.json`：`GET https://huggingface.co/api/models/ggml-org/models-moved/revision/499bc8821c6b12b4e53c5bffcb21ec206f212d81?blobs=true`。
  SHA256：`6c318e8c0e51d0d253881905b2638eb5ab6a8432a40ef1fb51f23b25aaccf31c`。
- `modelscope.json`：`GET https://modelscope.cn/api/v1/models/ggml-org/models/repo/files?Revision=f68dcd8c87746ccbf4f1f02703b45e393f921ce4&Recursive=False&Root=tinyllamas`。
  SHA256：`151a24bd14a2aadea60f1e61a7cfd42ef67763d2004d58ba01f824d7d446bce4`。

ModelScope 所选 `tinyllamas/stories260K.gguf` 的 `Revision` 与固定请求相同，
并声明 1185376 字节及 `270cba1bd5109f42d03350f60406024560464db173c0e387d91f0426d3bd256d`。
发现阶段的可变 master 响应未用作固件或端到端证据。此固件只证明解析规则；
ModelScope 文件传输没有真机端到端覆盖。
故障注入测试派生的大小、哈希、字段或状态变更均为模拟数据。
