# ADR 046：运行身份只保留 implementation_identity 这一条核对

状态：Accepted

## 背景

新运行同时写下 `tool_source_sha256` 和 `implementation_identity`。
有 `implementation_identity` 时，执行核对只比较其中 measurement 与 scoring 两个角色。
没有它时，退回比较整包 `tool_source_sha256`。
两个字段都在时，现行代码已经只采用 `implementation_identity`，另一个字段不构成第二道拒绝。

`implementation_identity` 覆盖已审核的角色文件清单和依赖版本。
`tool_source_sha256` 覆盖整包源码。角色清单之外的文件变化，只会被后一个字段看见。

## 决策

新运行应写入 `implementation_identity`，不再把 `tool_source_sha256` 当作第二道身份门。
新 RUN 记录不再要求 `tool_source_sha256`。
执行核对仍是 `execution_matches`：measurement 与 scoring 都应存在且相等。presentation 不参与这道核对。
角色身份不符时，仍在今天会拒绝的入口拒绝。重跑入口是否还拒绝，由 [ADR 047](047-rerun-recorded-comparability.md) 单独决定；本决定不放宽它。

旧运行没有 `implementation_identity` 时，继续用它保存的 `tool_source_sha256` 核对，不补造当前摘要。
一份记录里两个字段都在时，只采用 `implementation_identity`。
核心 `schema_version` 仍为 3。[ADR 038](038-inferyard-current-format.md) 的格式矩阵改为：新运行的身份是 implementation-identity.v1；`tool_source_sha256` 只用于没有前者的旧运行。

## 后果

角色清单之外的源码变化，不再使新运行的身份核对失败。
measurement 或 scoring 不一致，仍然失败。
旧运行的整包哈希继续有效，不改写。
