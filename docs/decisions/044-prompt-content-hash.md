# ADR 044：请求事件不再内嵌第二份 prompt 全文

状态：Accepted

## 背景

每个请求写两份 prompt 全文。请求快照保存实际发出的 body。
`request_started` 事件又内嵌同一份 `messages`。
离线核验用事件里的全文和快照、计划互相核对。

回答的逐 chunk 与终态全文是另一份重复。那份不在本决定里。

## 决策

请求快照继续保存实际发出的 body，包括 `messages` 全文。
新的 `request_started` 不再内嵌 `messages` 全文，改为保存这份 `messages` 的 `messages_sha256`。
事件里的 `model`、`stream`、`generation`、`case_id` 和其他计时字段保持不变。

离线核验要求新事件的 `messages_sha256` 与对应快照里的 `messages` 一致。
快照缺失、哈希对不上，仍然失败，不用计划或题包里的 prompt 填上。
warmup 和 probe 没有 case prompt，也以各自的快照为准。

旧事件里已有 `messages`、没有 `messages_sha256` 的，按原字节读取，不改写。
不要求旧事件补哈希，也不要求新事件补全文。
核心 `schema_version` 仍为 3。

不改回答 chunk 与终态全文的双份核对。那要另立存储决定。

## 后果

只读 `events.jsonl` 看不到 prompt 全文，必须打开对应请求快照。
快照和事件哈希不一致时，核验失败。
旧运行保持原样。新源码不继承旧事件体积下的测量资格。
