# Prism 模板身份核验

适用于 `prism_llama_server_v1` 的已知 build `b10743-adfffbe41`。
设计理由及固定版本源码见 [ADR 053](../decisions/053-prism-template-source-verification.md)。

`model.template_sha256` 始终是 `template_path` 原文件字节的 SHA-256；候选生成保留 GGUF
模板原字节，不能归一化或改写已有配置、模型、模板与运行证据。

`verify_properties` 先核对 `/props.build_info`、槽数、context 和模型路径，然后：

1. 读取原模板字节，精确核对冻结的 `template_sha256`，严格解码 UTF-8。
2. 从原件推导该 build 的 lexer source：CRLF 改 LF，剩余 CR 改 LF；仅当**原文**以 LF
   结尾时，从归一化结果末尾去掉**一个** LF。空格、tab、其他换行和正文不变。
3. 将服务返回的 `chat_template` 原样编码为 UTF-8，与推导表示进行精确 SHA-256 比较。

模板非字符串、原件不可读/无效 UTF-8、原件哈希不符或服务表示不符均拒绝，原因
`service_template_mismatch`。不接受原件或服务两侧通用 `strip`/`rstrip`，也不接受服务
模板回填原件、任意引擎模板替换或其他 build 的推定兼容。未知 build 仍先报
`unsupported_engine_build`。其他适配器保持原有规则。

没有新增字段或开关，schema 仍为 3。模型、引擎、运行库、模板文件与进程身份的既有
复验继续执行。原始 `/props` 仍按原观察流程封存，不改成归一化后伪造的服务响应。
旧运行只读，不将旧失败改为成功；新实现须新建测量，资格依照
[血缘规则](../data-contract.md#证据血缘与比较结论)。
