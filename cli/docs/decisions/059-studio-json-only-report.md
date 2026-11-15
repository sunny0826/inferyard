# ADR 059：移除产品级离线 HTML 报告，保留原始证据和结构化 JSON 核验

状态：Accepted

日期：2026-11-13

实施：commit 链 `b5c17e4..ee90d09`（分支 `cleanup/remove-offline-report`）；文档同步由后续文档单元完成。

## 背景

InferYard 的普通报告此前同时生成 `index.json`、`report.html` 和 `artifact-manifest.json`，并将两个 run 的展示隐式变成 comparison。Studio 实际只读取部分 JSON 展示字段，却以 HTML 是否存在决定报告能否列出或创建。comparison、engine-fit、engine-comparison 和 extension 也各自携带 HTML、呈现核验或重渲染。

这形成两套状态：原始 run 的证据状态，以及展示文件是否完整、能否重渲染的状态。用户只需要供 Studio 使用的一份最终可读 JSON，并要求移除可核验离线报告的代码、测试和文档。本 ADR 选择退役展示产品，而不是把展示封存改名后继续存在。

## 决策

### 1. 普通报告只有 studio.json

普通报告目录的唯一必需文件为 `studio.json`：

```json
{"schema_version": 3, "kind": "studio-report.v1", "runs": []}
```

`schema_version=3` 保持现有公共 wire envelope；`kind` 是新的报告格式身份。registry key 为 `studio_report`，结构源在 `contracts/schemas_studio_report.py`，导出文件为 `studio_report.schema.json`。不沿用 `report_format_version=8`，不保留 `index.json/report.html/artifact-manifest.json`。

`report --runs ... --out ...` 保留，`--comparison` 及隐式双 run comparison 删除。输出先完成纯投影和 Schema/跨字段校验，再在新目录原子写单文件；不覆盖旧目录或来源证据。

Studio 只读取其消费的 run 身份、回链路径、模型/引擎/机器子集、completion/stop reason、质量/分类/延迟/内存卡片和请求 case/category/state/quality/latency。题面、原始答案、reference answer、reasoning、正文 contents、SVG gallery、资源曲线、证据 URL 和完整运行配置不进入新文件。资源曲线虽被排除，内存卡片需要的原始采样聚合必须保留。

`studio.json` 是可重建的展示投影。`source.path` 只是来源定位，不是内嵌证据或完整性承诺；Studio 不重新评分，也不把成功读取称为 verified。原始 run/batch/plan 和其 manifest 才是证据权威。

### 2. 呈现核验和重渲染退役

删除普通报告 handler、`report-check`、comparison/report 联合核验、`presentation-seal.v1` 的当前产品写入与成功核验、HTML 模板身份、`--rerender` 和 `render_checked`。通用 presentation artifact seal writer 在无当前写入调用后一并删除。

保留私有 `evidence/legacy_presentation_seal.py` 作为拒绝前有限字节检查器：只解析已知 `presentation-seal.v1` 结构，检查固定成员、路径和 SHA-256，并保留版本识别所需的 JSON 字节；不保留 seal writer、renderer、旧评分器或旧格式 verified 成功路径。旧 report/comparison/engine-comparison 若带已知 seal，先通过该检查再按版本 UnsupportedFormat/exit 2；坏 seal、固定成员缺失或成员 hash/path 不符为 exit 4。无 seal 的可识别旧版本也可拒绝，但不声称其内容完整。

`verify --path <studio-report-dir>` 固定返回 `studio_report_not_verifiable`/exit 4，该常量在 `contracts/error_reasons.py` 的安全白名单内。它不写文件，也不将报告状态变成 verified。Python `read_studio_report()` 只执行严格 JSON+Schema；Go 生产 parser 执行防御性 typed read，契约/E2E 测试使用真实导出 Schema，不把二者混称为证据核验。

原始 run/batch/plan、export/rescore/public/overhead/repeat 的核验保持原语义，包括其真实的 `bytes_verified` 等结果。

**原始 manifest 例外：**原始 run 的 writer、reader 和 contract 对 `summary.json/report.html/requests.jsonl` 的 `derived` 分类继续有效。保留该分类不代表继续生产或验证旧报告；它避免历史 run manifest 的字节分类被篡改，维持原始证据核验的一致规则。坏原始文件、坏派生文件和未封存完整前缀仍按既有 evidence/partial 规则处理。

### 3. 其他 JSON 协议按各自语义升版

| 协议 | 新必需布局 / 身份 | 保留的保证 | 旧格式 |
|---|---|---|---|
| comparison | `comparison.json`；`schema_version=3`、`format_version=5` | source ref、来源 hash、performance evidence、顺序及 canonical 语义重算；`verified/source_runs=2` | v1–v4 明确 UnsupportedFormat/exit 2 |
| engine-fit | `engine_fit_manifest.v3`；run 为 plan/run/requests JSON，comparison 为 comparison.json 与两个来源 run 快照 | JSON manifest bytes/hash、来源和语义、partial/exit 3；不保留 HTML | 完整 v2 识别后 UnsupportedFormat；损坏 exit 4 |
| engine-comparison | `engine-comparison.json`；definition 仍为 `engine-comparison.v2`，另有 `schema_version=3/format_version=2` | plan/batch/source 重算、路径恢复、资格和诊断；producer 只含 tool_source_sha256 | 可识别 v1/旧 v2 均 UnsupportedFormat；畸形或损坏 exit 4 |
| extension | run kind `extension_packet.v2`；packet/summary/事件证据/manifest | packet→summary replay、live plan/identity/child evidence 和 containment | 完整 v1 识别后 UnsupportedFormat；损坏 exit 4 |

这些版本不是统一编号：全局 `schema_version`、比较布局版本、engine-comparison 算法 definition 和产物 layout version 是不同概念。除 Studio 新契约外，不为了形式统一把现有本地产物强行加入 registry；其专用结构检查、manifest 或完整重算继续拥有相应保证。

共享的 `comparison_input()`、`verify_metric_references()`、`build_comparison()` 及 `read_verified_comparison()` 的 JSON 用途保留。来源读取移到中立 analysis 模块，export/rescore/public/candidates/engine-comparison 的调用者同轮迁移，不随 HTML 删除评分和来源绑定。

### 4. progress 只表达报告是否存在

`progress_version=2` 的报告状态只有 `missing|present`：缺失时 next_action=report，存在时 next_action=done、next_command=null，且默认范围完成。没有 verify/verified 阶段。旧 progress v1 文档仍明确 unsupported，这与忽略旧报告目录是两件事。

批量发现只把 `studio.json` 当当前候选，不扫描旧 `index.json` 为 legacy 状态；显式打开或 verify 旧目录才 unsupported。只有旧报告时返回 missing 并给出新 out；旧报告与匹配新报告并存时为 present/done；旧报告旁有未完成 run 时保留原 run/resume 优先动作，旧目录不阻断 bind/run。已有但损坏的当前 `studio.json` 显式报错，不得当作 missing 静默跳过。

Studio 删除 ReportVerify、第二次 CLI verify、核验按钮/徽标和 previewVerifyReport；发现、创建和读取均使用 `studio.json`。真实 CLI 和模拟 preview 同轮改变，保留路径包含性、Resolve、symlink 防护、来源 run 检查及其他专用流程。

### 5. 实现身份、依赖与安装资格变化

删除 13 个普通报告模板和 2 个 engine-comparison 模板后，移除 Jinja2/MarkupSafe 的运行依赖和发行元数据，更新 uv.lock。题包人工审核 HTML、模型聊天模板及模型 template_sha256 不属于本决策，继续保留。

`implementation-identity.v1` 的 measurement/scoring/presentation 三角色不变，不拥有报告完整性核验。共享 `schemas_for()` 和语义分派按现行静态导入闭合保守绑定：新 Studio Schema 及其共享 helper 进入 measurement/scoring/presentation 相应清单，修改它们不承诺只改变 presentation。只有先证明未被 measurement/scoring 导入的纯投影文件，才可断言“仅 presentation 改变、execution_matches=true”。Go 源码由 Studio 构建身份负责，不能写入 CLI 包相对清单或声称被 Python 身份覆盖。

三份实现清单随首次新增、删除或 import 迁移在同一实施单元原子更新。presentation 依赖在过渡期同时含 validation 与仍使用的 Jinja 依赖，最后一个 renderer 删除后收窄为 `VALIDATION_DEPENDENCIES`；Python 始终由 `dependency_versions()` 自动加入，不能显式重复。旧 run 身份字节、hash 和资格不回写。

安装检查升级为 `installed_safe_checks.v4`：以 Studio JSON+Schema、当前 JSON 布局和旧格式拒绝为 scope。已安装 CLI 执行 `--schema studio_report`，探针解析其 stdout、校验实际 studio.json，并记录 `$id` 与规范 JSON（排序键、紧凑分隔符、UTF-8）的 SHA-256；该摘要与已核验 `wheel_sha256`、source commit 和实际命令记录绑定。仓库导出的 Schema 仅用于源码/E2E gate，不新增 wheel/sdist 静态 Schema 副本或资源白名单承诺。v3 的 report_verify/current_format_reports 结果不能作为新资格。移除 HTML/rerender 的成本测量不等于证明性能提高，JSON-only 成本需要新的测量记录。

## 破坏性影响

### 旧报告阅读

- 旧 v8 及其他可识别普通报告不支持新的 CLI 核验或 Studio 只读，不自动迁移、转换、补写或删除。
- 对普通报告 verify，UnsupportedFormat 的 supported 集合为空，因为已没有任何可 verify 的普通报告版本。不能用“支持 studio-report.v1”暗示新 JSON 也有 verify。
- 用户可自行打开仍存在的旧 HTML，但 InferYard 不再保证其展示、完整性或重渲染。需要新格式时，从有效原始 run 在新目录重新派生；这不叫旧格式迁移。
- 损坏 JSON、版本类型错误、坏 hash/seal、来源不符仍为证据错误 exit 4，不能伪装成正常旧版本 exit 2。带已知旧封存的输入先经有限字节检查再拒绝；无 seal 的可识别旧版本可 unsupported，但不声称完整。合法未封存前缀仍按各原协议 partial/exit 3。

### 字段语义

- 新 JSON 的 run 数组不 pooled、不隐式比较；nullable 显式存在，缺测不得填 0。
- `quality.complete` 只表示展示质量输入是否完整，不是证据 verified；`source.path` 只是回链。
- count 使用 integer；ms 与由 MiB 换算的 bytes 使用非负 number。请求 latency 取 `request_timing()` 的 L03 value；聚合只采样 completed 请求，excluded 按全部请求计算，p95 少于 20 样本为 null，多来源内存不擅取极值。请求 quality 沿用 score 内的 quality_state，不改取顶层同名字段。
- 正文、原始答案和图表不再由普通报告提供；原始证据中的相应内容不被删除，Studio 也不借旧字段隐式访问它们。

### 测量范围和资格

- 不修改评分、分母、原始执行、服务取消/排空、比较资格或题包审核语义。
- 删除 HTML 渲染、模板 hash、呈现封存和重渲染后，原安装/成本测量范围失效；新 scope 只测实际保留的 JSON 投影、写入和来源/语义行为。
- 源码和依赖身份改变后重新计算身份；不能继承旧安装资格、旧测量资格或声称稳定提速。没有本轮性能测量数据。

### 兼容政策

本决策延续 ADR 038 的“当前格式、原件不覆盖、旧数据不迁移”方向，但进一步取消普通报告的当前核验支持。可识别与损坏的区别必须保留；带已知旧 seal 的输入先通过有限字节检查，无 seal 的输入只作有限版本识别、不声称完整，不为减少代码而把任何无法读取的输入都归为 unsupported。旧 progress v1、旧 comparison v1–v4、旧 engine-fit v2、旧 engine-comparison、旧 extension v1 也不静默降级为新格式。

## 局部取代关系

下列 ADR 保留历史正文，相应条款由本决策局部取代：

- [ADR 011](011-report-dashboard.md) 的自包含离线 HTML 展示及其当前产品承诺由本决策取代；原始回答和评分证据本身不删除。
- [ADR 037](037-offline-evidence-reading.md) 的 report v7/comparison v4/engine-fit manifest v2 的 HTML 封存、默认呈现核验和显式 rerender 由本决策取代；原始产物 verify、partial、坏 seal 不降格、来源映射和携带包边界继续有效。
- [ADR 038](038-inferyard-current-format.md) 中“ADR 011 自包含展示不变”及 [ADR 045](045-report-content-refs.md) 所确立的 report v8 当前格式承诺由本决策取代；当前格式、拒绝迁移和损坏不降格政策继续有效。
- engine-fit/engine-comparison 相关 ADR（[012](012-engine-fit.md)、[016](016-engine-fit-temperature-override.md)、[019](019-engine-fit-memory-override.md)、[056](056-engine-comparison.md)、[057](057-comparison-task.md) 系列）仅取代 HTML 文件集合和呈现核验，不取代已保留的算法、来源、平台限制、显式停止覆盖或测量资格规则。

## 备选方案

1. **只删 HTML，保留 index.json 和旧字段。**改动较小，但保留双 run 隐式比较、正文/图表字段、旧识别和进度耦合；拒绝。
2. **保留 Studio v8 只读。**减少旧工作区断裂，却长期维护两套 Schema 和旧正文边界；拒绝。
3. **为 studio.json 继续 seal/verify。**可以保留字节状态，却直接恢复用户要求移除的 presentation-seal；拒绝。
4. **删除全部 verify 与 JSON 分析。**文件更少，但破坏原始证据、候选筛选、export/rescore/public 和扩展协议；拒绝。
5. **采用本 ADR 的单 JSON 与分协议升版。**迁移面最大，但正确性、消费者需求和长期维护边界最清楚；采纳。

## 迁移与回滚

不迁移历史字节。实现分阶段先新增契约，再切换 CLI/Studio 与各 JSON 协议，最后移除依赖、探针和活动文档。回滚实现只能恢复软件版本，不得把旧身份或资格写回新 run，也不得删除已生成的新目录。
