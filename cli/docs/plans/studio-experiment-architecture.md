# Studio 实验模型与设计层重构

CLI 已有[比较计划与派生接口](../contracts/engine-comparison.md)，Studio 的默认方案 A（薄边紧凑）见[设计系统](../../../studio/docs/design-system.md)。[ADR 057a](../decisions/057-comparison-task.md) 决定首轮只接入专用比较任务，长上下文专项沿用现有流程。后续派发仍按已确认的任务、角色、写入范围和验收方式进行；范围或角色变化需重新确认。以下为剩余工作，实测记录见[跨引擎比较 backlog](../backlog/engine-comparison.md)。

1. 确定比较判定、限制与阻断原因的中文词表归属（D3）。Studio 保留未知原因的原码。比较呈现使用当前 JSON 接口。
2. 在 Studio core 实现比较专用任务（T6）。按冻结槽位显式换服务、重新绑定并执行；覆盖取消清理、替换与绑定失败、重启后的任务归属。CLI 决定能否继续及最终比较结论。保留实现快照审查和独立复核。
3. 接入比较设置、运行、结果及历史入口（T7）。支持按题包查看判定、展示指标和限制。正式与模拟共用页面，分别对照相同的独立预期；验证故意引入的分歧确实能被捕获。原生验收由独立会话执行，范围见[原生 UI 测试](../../../studio/tests/ui/README.md)。
4. 同步接口、页面和子系统参考，并在包含文档的最终提交上完成整分支审查（T8）。
5. 在授权设备上核对预算和停止条件，由 Studio 执行单份默认双题包计划的 A/B/A/B/A/B 六块、12 槽（T9）。保存逐槽位绑定证据、`compare-engines --plan PLAN --batch BATCH --out NEW` 的输出和 `verify --path NEW` 的结果。模拟窗口、CLI 合成链及历史多计划比较分别记录。
