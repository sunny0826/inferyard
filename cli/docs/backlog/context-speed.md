# 长上下文速度专项测评

[返回 Backlog](../backlog.md)

## 实现与验证范围

- 2026-10-18，指定审查者批准规划快照 `ffe1e49c98cdd77fe126930cac023303cdda9c7ecfdb872996ef36c3d6cd8f98`；用户认可 `fdd192c` 的模拟界面并批准三档题库。题库原字节、分词与批准范围见[人工批准记录](../../bundles/CONTEXT-SPEED-APPROVAL-20261018.md)。正文的「下一轮将将」若修订，需重新分词、更新哈希并复审。
- 软件验证涵盖三档假引擎各五次、离线读取与坏证据拒绝。Studio 的正式编排和模拟分别对照预期，故意改错模拟 Decode 时回归能捕获。接口见[专项 CLI](../cli-reference/context-speed.md)，历史和结果归属见[Studio 参考](../../../docs/subsystems/studio-panel/context-speed.md)。
- macOS 原生模拟窗口 `run-ve1iekny` 通过六项，覆盖结果、逐次样本、历史重开、取消、启动失败、缺测与重读。`run-ue3rwkw2` 验收了六列表头和诊断展开。后续 `run-xqk53r5v` 在自动化初始化超时，未执行用例；该次正式窗口的历史与 64k 结果另行人工核对。

## macOS 64K 真实模型记录

真实环境修复并完成 64K 闭环：新任务重置现在等待 UI 回调确认，避免遗留取消循环影响后续任务；
页面提前拒绝已知仅支持 8K 的模型，并停止在新表单重复显示旧专项错误。
Prism 固定 build 的模板服务表示按 [ADR 053](../decisions/053-prism-template-source-verification.md)
独立核验，保留原模板字节与哈希。延迟回调回归先失败后通过，隔离故意错误被 parity 捕获；
CLI 全量 5610 通过、55 平台跳过，Go 普通及带标签回归、vet、Ruff、Schema/catalogue 检查通过。
`run-jc39mq5k` 原生模拟回归 6 项通过、0 失败、0 跳过。
正式 Studio → CLI → Prism `b10743-adfffbe41` → Qwen3.5 0.8B Q4_0，任务
`24503a67baff4e54ba915fd5cbf6e1b8` 完成五次正式测量，全部有效，每次输出 256 tokens；
`speed-results` 与 `verify` 均通过，六列结果、逐次数据与应用重启后的历史重读已核对；
读取前后 55 个证据文件哈希一致，旧模板拒绝批次的 13 个文件也保持原字节。
Context 为 65536，模板后实际输入为 19233 tokens；中位数 Time 20.520434 s、
Prefill 937.260879 tok/s、Decode 46.383193 tok/s。这证明本配置闭环，不能解释为填满 64K
或其他模型的速度结论，128K/256K 和其他平台未验证。
保留所有旧任务与原始证据；Llama 3.2 1B 的首次尝试被模板检查拒绝，修复后的尝试因
可用内存低于冻结阈值停止一次，未绕过阈值或重试同一批次。换用更低内存模型后新建上述任务。
实测结束已停止 Studio 所有的引擎，日常构建路径已替换为通过原生回归的二进制。

指标审核发现的两项缺陷已修复：正式请求要求唯一 usage 与模板分词、最终 timing 的输入
计数一致；模板核验将原文证明封存在 identity，在线与离线共用纯函数重算，准备长度读取
也采用同一规则。原件与原始 props 保持不变，现有无证明证据只保留精确哈希路径。
缺失/重复/矛盾计数及模板换行、坏证明、外部路径不可用均有回归；失败基线 3 项，
修复后全量 CLI 5630 passed、55 skipped，Ruff/format、Schema/catalogue/resources 检查通过。

新真实批次 `295d63a096cc4db7a674656c55a21ff2` 使用同一 Qwen 模型及固定 Prism build，
64k 五次全部有效。每次模板后输入 19233、输出 256；中位数 Time 16.020276 s、
Prefill 1200.541114 tok/s、Decode 58.338639 tok/s。独立于产品公式的原件复算、离线 verify、
Studio 重启后历史重读均通过，新旧两批各 55 个文件哈希不变；不作跨轮提速比较。
带末尾 LF 的 Llama 原模板另以设备推荐 4k 配置完成 9 次管理接口分词探测，
原件/服务哈希不同但在线及封存后离线核验通过；没有生成请求，不是 64k 测评。
本轮自有服务均已停止。

## 剩余工作

- 通用消费者接入（B2）仍待完成。对 report、compare、export、rescore、repeat-summary、public 和 engine-fit 的相应入口逐项验证专项、混合、派生与坏来源的拒绝路径。覆盖写入前拒绝、普通来源对照及只读 verify/progress/speed-results。例如 [existing_analysis](../../src/inferyard/reporting/export.py) 校验 analysis 与指标引用，[build_studio_report](../../src/inferyard/reporting/studio_report.py) 按 run 归约，两者均无专项消费者的显式拒绝分支。旧 analysis 的来源追溯和读取政策待裁决。专项 resume、单次 rerun 和既有 batch 重用的现行限制见[专项 CLI](../cli-reference/context-speed.md)。
- 128K、256K 及其他平台的真实模型需分别验收。macOS 模拟窗口的 first-responder 顺序警告仍待查。
- 计数和模板证明修复后的独立源码、拒绝路径及注释审核待完成。Lody 会话当时因 workspace cleanup 错误阻断，待恢复或由用户指定替代角色；若发现问题，修复后重验受影响路径。最终实现仍待指定审查者审核。
