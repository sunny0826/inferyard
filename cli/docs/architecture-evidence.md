# 架构证据边界

本文属于 [当前架构](architecture.md)。

## 保存与离线读取

新 run 使用 `evidence_layout_version=2`：Journal 在创建六份日志前原子发布 `inputs.json`，
低频观测追加到 `observations.jsonl`，完整脱敏请求追加到 `request-bodies.jsonl`。
请求原件同步 flush/fsync 后才允许 dirty、started 和发送；native clean receipt 同步耐久后才清 dirty。
各领域通过 `TrialEvidence` 查询已核验成员及真实来源引用。合法 schema-3 快照布局只读，
通用 EvidenceStore、独立 baseline 与 extension 保持各自协议，详见[容器契约](contracts/run-evidence-layout.md)。

请求身份先落盘，再发送请求；终态与评分分别保存。日志按顺序追加，必要证据写入失败即停发并保留 dirty，不继续产生无证据结果。封存 manifest 记录身份、来源和文件哈希，不哈希自身。

离线读取不发模型请求，不根据证据中的 URL 获取内容。携带包只读取包内允许的相对路径，拒绝路径穿越和越界符号链接。普通报告的外部来源可用相对路径及显式源根映射定位，定位不代替 manifest 身份；见[离线读取契约](contracts/offline-reading-contract.md)。未封存日志可读取完整前缀并报告末尾截断；中间损坏、重复记录或哈希不符不能跳过后发布完整成绩。派生物重建到新目录，原件不覆盖。

保存的 completed 请求缺评分时标 unscorable，不自动套用新评分器。显式重评分产生独立分析身份。
InferYard 按 [ADR 038](decisions/038-inferyard-current-format.md) 的[支持矩阵](contracts/inferyard-current-format.md#格式支持矩阵)核验当前格式，拒绝不支持的旧格式及 `origin=migrated` 运行，不提供历史运行迁移。
旧数据保留原件并交原项目处理；当前派生产物保留原测量源码、评分身份及来源，遵循[证据血缘规则](data-contract.md#证据血缘与比较结论)，不把重建结果当作新测量。

凭据通过环境变量引用，不进入配置快照、日志和 Git；已知凭据在落盘前脱敏。脱敏后无法复核的评分输入不得给出可复现成绩。HTML 自包含并转义题目、答案、路径和异常信息；v3 的 SVG 提取结果局部原始渲染，脚本限制由 CSP 保证，详见[报告契约](contracts/report-contract.md#v3-svg-展示与-csp)。

## 方法与证据边界

[分析项表](reference/methods-matrix.md)是带适用前提的方法目录，不是所有设备都要跑完的套餐。工具已实现、模拟已验证、真机已通过分别登记。人工题包审核绑定题目、答案和规则哈希；自动测试不能代替人工审核。

重复同题只说明固定题集上的波动；安全停止保留原因与计数，不把未测档位写成模型不支持。受限实测留下接手步骤和验收条件，软件缺口不能归因于硬件。人工题包审核仍有效，独立操作者签收不是验收门槛。
