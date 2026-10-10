# 应用内存诊断契约

`memory-check` 是 macOS 的只读诊断，供 agent 和用户判断是否手动退出应用。
它不进入正式采样、不管理服务、不关闭用户进程，也不提供安全关闭或可释放内存的承诺。
Linux/Windows 当前退出 2（`unsupported_platform`）；后续平台独立实施和验收。

## 命令与保存

```bash
inferyard memory-check
inferyard memory-check --protect-pid 1234 --protect-pid 5678 --out memory-new
inferyard --schema memory_check
```

默认 stdout 为 `CommandResult` JSON，诊断走 stderr。不传 `--out` 不落盘；传入后只向新目录
写 `memory-check.json`，保存内容就是 stdout 的 `details`。已有目录拒绝/退出 4，原件不覆盖。
重复保护 PID 合并，非法或非正整数在采集前拒绝/退出 2；PID 0 作为 macOS 系统进程保留在清单中。
`--protect-pid` 只记录本次观察到的启动身份，不是未来操作令牌，不证明调用前 PID 未被复用。

## 输出与读取支持

新诊断为 `schema_version = 3`、`kind = memory_check.v1`，与 run/sample/device_preflight 独立。
`--schema memory_check` 输出结构 Schema；`contracts.memory_check.read_memory_check` 还核验
重复键、PID/组唯一性、成员关系、原因、保护声明、RSS 归约和覆盖计数。未知 kind/旧版本拒绝，
不迁移或转换历史诊断。`verify` 不接受本诊断为正式 run 或报告证据。

进程新增可选字段 `parent_identity_missing_reason`，新输出始终包含它；旧 v1 文件缺少该字段仍可读取。
父关系缺测为 `parent_identity_unavailable` 或 `parent_identity_changed`，此时父启动身份必须为 null，
自身身份与 RSS 仍有效，原有 `missing_reason` 为 null。自身缺测继续使用原字段并丢弃 RSS。
`coverage.missing` 按自身或父关系缺测的进程数计数，每个进程只计一次；父关系缺测仍退出 3，
但不等于 RSS 缺测，组 RSS 总量只依据成员 RSS 是否完整归约。

快照记录以下字段：

| 字段 | 含义 |
| --- | --- |
| `captured_at` / `collection_ns` | 枚举结束的 UTC 时间 / 含子进程启动与回收的采集纳秒数；非原子快照 |
| `memory_total` / `memory_available` | psutil 整机字节读数、来源、缺测原因；不是模型归因 |
| `processes` | PID、raw kernel 启动身份、父身份、显示名、RSS、缺测、组引用、判断原因 |
| `groups` | 应用显示名、成员/根 PID、归属依据、已知 RSS 小计、可观测总量及判断 |
| `coverage` | 枚举总数、已观察/缺测/未观察数、截断原因及完整性 |
| `budget` | 实际采集 deadline 和进程上限 |
| `protected_requests` | 显式声明 PID、本次启动身份及核验结果 |

内存单位为 bytes，显示为 MiB/GiB 时需显式除以 1024 的相应幂。RSS 包含共享页，应用 RSS
合计不等于退出后释放量。缺测为 null 加原因，合法的观测零值仍为 0。某组成员缺测时
`rss_total_bytes = null`，仅保留明确命名的 `rss_known_bytes` 小计。

## 归组

从进程可执行文件的规范路径识别最外层 `.app`，读取有界 Info.plist 并核验 APPL 类型和
bundle 文件身份；不同安装副本即使同名也分开。同一 bundle 多实例归为整个应用，保留根 PID，
不识别 profile/窗口。内部路径和文件身份仅用于归属，不原样输出；group_id 为内部键的摘要，
不保证跨快照匹配。

helper 有可信嵌套 bundle 时按 bundle 归组；无 bundle 时可依据已核验父链归组。
bundle 与父链冲突时独立列为 unknown；launchd 拉起且两者均无可靠归属的进程也独立展示。
应用组只代表可核验成员，不保证穷尽所有 helper。自身启动身份或所有者在采集中变化时丢弃 RSS
并记录原因，不把两代 PID 合并。父身份不可读、父 PID 或父启动身份变化时，只清空父启动身份，
保留核验过的自身身份和 RSS，不沿该父关系归组或传播保护。受影响的未保护对象为 unknown，
不因此降低无关可读应用的 review 状态。采集不是持续监视，使用旧清单操作前应重新检查。

## 判断与保护

按 `protected > unknown > review` 归约组内状态，原因携带对应 PID。

- `protected`：命中系统路径/PID 0、跨账户、调用者/采集器祖先、已识别 Agent/服务/连接工具，
  或显式保护声明及可核验后代。组内任一成员 protected，整个应用组 protected。
- `unknown`：身份、所有者、应用归属或必要保护检查不足/冲突。未归组解释器和后台服务默认
  unknown。扫描截断、显式保护无法核验，或存在未受保护的本账户/未知账户受限对象时，
  其余未保护对象也不升级到 review。
- `review`：本账户、身份与 GUI 应用归属完整，必要保护检查完成且未命中。仅提示询问用户
  是否仍在使用、有未保存编辑、后台任务或连接；不表示这些情况不存在。

调用祖先仅保护自身，不把所有兄弟进程保护或归组；系统/跨账户保护不会经 launchd 扩散到
所有用户应用。已确认自然退出的对象无需阻断其他应用 review，但仍计入采集缺测/退出 3。

已识别保护线索包括 `.lody` 路径、codex/claude/lody、llama-server/ollama、ssh/sshd/mosh，
以及 Lody、Cursor、VS Code、Terminal/iTerm/Warp、LM Studio、Raycast、Clash Verge、
UURemote/RustDesk/AnyDesk/TeamViewer、Magpie/Kimi/KimiCU/Grok Bot 应用。
名称线索采用保守保护，不能证明所有软件角色；不保证识别任意嵌入式/伪装 Agent 或未识别服务。
调用 agent 应显式保护已知模型/关键服务，未知用途仍交用户确认。规则没有自动关闭动作。

## 有界采集与退出

全机 PID 枚举一次，在自有独立模块子进程读取必要属性；不读取完整命令行、环境变量、
窗口标题或文档内容。权限不足、进程退出、身份变化分别保留原因，不静默当零值。
正常/超时/取消均回收自有采集器，绝不终止被观察的进程。

采集 deadline 固定 **5 秒**、进程上限 **4096**；这不是响应速度承诺。
在本次 macOS 数百进程原生观察的成本之上保留余量，同时限制异常慢查询与异常大进程表。
deadline 含采集子进程启动，结束时可增加最多约 0.5 秒的有界回收等待；输出校验/归组/序列化
在主进程另计。原生观察记录在 backlog，不用预算代替性能证据。
到达上限且仍有 PID 或触及 deadline 时截断/退出 3；没有截断不等于没有权限缺测。

| 退出码 | 含义 |
| --- | --- |
| 0 | 枚举完成、必需读数可读、显式保护已核验；归属 unknown 本身不算采集失败 |
| 2 | 无效参数或不支持平台 |
| 3 | 部分采集：权限/身份/退出缺测、保护未核验、截断或主机读数缺测 |
| 4 | 采集工具失败或输出写入错误 |
| 130 | 用户取消；没有完整快照可供保存 |

用户手动退出应用后重新调用，比较同来源主机可用内存的观测变化；不据此推断因果收益。
准备模型运行仍使用原有实时 preflight，证据结论遵循[血缘规则](../data-contract.md#证据血缘与比较结论)。
