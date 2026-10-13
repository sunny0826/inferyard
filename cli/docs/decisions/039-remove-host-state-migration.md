# ADR 039：移除主机状态迁移与旧入口退休机制

状态：Accepted。日期：2026-10-09。
局部取代 [ADR 038](038-inferyard-current-format.md) 的锁迁移方案；ADR 038 的格式矩阵、
拒绝语义与安装资格版本条款继续有效。软件实现与平台验证范围见 [backlog](../backlog.md)。

## 决策

删除 `inferyard host-state migrate` 命令、`runtime/host_migration.py`、
`runtime/host_receipt.py` 及全部退休标记、维护凭据与迁移事务语义。具体行为：

- **自动初始化**：实时入口首次获得 `HostLock` 时，若新 state 不存在则直接创建
  `{"schema_version": 1, "dirty": false}` 的 clean 状态。新机器不再需要任何显式初始化步骤。
- **v0.0.1 遗留状态**：接受带 `migration` envelope 的 schema 1 状态，`dirty` 语义照常执行；
  envelope 不再核验，在下一次状态写入时自然消失。遗留的 `inferyard-host-migration.json`
  凭据文件视为无关文件，不读取、不删除、不校验。
- **旧工具文件完全忽略**：`local-ai-benchmark-host.lock/.state.json`（含 Windows D 根旧位置）
  不读取、不加锁、不退休。InferYard 不再承担对固定旧工具的退休证明或跨工具互斥；
  主机互斥只覆盖 InferYard 自身进程。
- **不变项**：InferYard 自身主机锁互斥、崩溃 dirty 持久化与人工恢复确认、
  锁/根身份核验、安全文件检查（权限、nofollow、链接数、reparse）全部保留。

安装 CI 的初始化阶段改为依赖自动初始化：显式 `--initialize` 步骤改为首次 `HostLock`
进入即建立 clean，`host-initialization.json` 记录自动初始化语义；一次性 runner 环境守卫不变。

## 局部取代范围

| 原决策或规则 | 被取代的承诺 | 继续有效 |
| --- | --- | --- |
| [ADR 038](038-inferyard-current-format.md) 锁迁移方案 | 显式迁移事务、退休标记、维护凭据、旧锁退休证明、`host-state migrate` 命令 | 格式支持矩阵、拒绝语义、安装结果 v3、主机互斥与 dirty 恢复本身 |
| [当前格式契约](../contracts/inferyard-current-format.md) 锁迁移章节 | 首次部署应显式迁移、旧位置退休事务与中断幂等规则 | 新锁固定路径、state schema 1、dirty 恢复规则 |

## 替代方案与风险

| 方案 | 取舍 |
| --- | --- |
| 保留机制、仅从 README 移除表述 | 文档与实际行为不一致，用户仍被强制执行初始化；不采用 |
| 保留命令但仅做 fresh 初始化 | 保留无意义维护入口和退休语义，清理不彻底；不采用 |
| 完全移除（采用） | 实现最简、新机器零摩擦；代价见下 |
| 顺带更换锁路径 | 无必要，路径已与旧工具分离 |

采用方案的风险与接受理由：

- **旧工具可并发运行**：两工具锁路径不同，移除退休后旧固定基线可再次实时执行。
  这是有意的范围收缩：InferYard 是独立产品，不再为旧工具的进程纪律负责；
  需要旧工具隔离时由操作者自行管理。
- **v0.0.1 遗留 dirty 不降级**：接受遗留状态时 dirty 字段照常阻断并走人工恢复，
  安全属性不因 envelope 忽略而放宽。
- **凭据残留**：已迁移机器上的 `inferyard-host-migration.json` 与退休标记成为无主文件，
  保持原样不动，不删除。
- **中断状态接受面扩大**：v0.0.1 的 pending 状态（从未 ready）在新版中被视为 clean。
  该状态在 v0.0.1 下本来就无法运行，不构成安全回退。
- **外部删除 state 不再阻断**：state 文件被外部删除后，下一次实时入口会自动重建 clean。
  v0.0.1 下「删 state 留凭据」会阻断；旧契约本就把管理员删除状态列为保证范围外，
  新版显式接受这一边界，dirty 服务仍由恢复流程约束。

## 拒绝与验收

- CLI 不再有 `host-state` 命令；传参按参数错误退出 2，帮助不再提及历史迁移。
- 无任何状态时，首次实时入口自动建立 clean 状态并正常执行，不发送额外模型请求。
- v0.0.1 envelope 状态：`dirty=false` 直接可运行；`dirty=true` 阻断并走原恢复流程。
- 旧工具文件存在时不影响新工具读取、加锁与运行。
- 新锁互斥、dirty 持久化、崩溃后恢复确认的原有回归全部保留并通过。
- 删除迁移事务与退休准入测试，新增自动初始化与遗留状态接受回归。

实现的适用范围与平台未验项见 [backlog](../backlog.md)。
