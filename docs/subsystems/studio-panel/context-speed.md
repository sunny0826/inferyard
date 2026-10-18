# Studio 长上下文速度

本参考记录 Studio 正式接线，不授予题库人工批准、原生窗口或真实模型资格。
结果协议见[共享契约](../../../cli/docs/contracts/context-speed-contract.md)，
证据解释见[血缘规则](../../../cli/docs/data-contract.md#证据血缘与比较结论)。

## 调用与归属

同一 Dashboard 按钮经 `startSpeedFlow` 调用 `Service.StartContextSpeed`。
先查询 `--schema context_speed_result` 的版本、两种分支、必需字段及五项样本结构；
不缓存能力，CLI 替换后重新查询。旧 CLI 的专项返回升级提示，普通功能继续使用原接口。

专项在工作区 `context-speed-operations/TASK` 新建独立材料，调用 init、device-check、
带 profile/context 的 config create，复用 StartEngine 的完整 argv、进程所有权与健康等待，
然后由 CLI bind 核验 PID/start ticks，再调用 plan preset。
当前正式 init 拒绝为 `human_corpus_review_required` / 4；页面显示固定原因，
不以 fixture 或模拟模式绕过。预算读取冻结计划的 `max_wall_seconds`。

准备任务和冻结计划任务都归原 `s.runs`；计划就绪后由原 `runQueue` 单 worker 执行。
`SpeedPlanRef`、`SpeedBatchRef` 与子 `SpeedRunRef` 不占用普通 `RunPath`。
命令固定为 `run --plan PLAN --output-root NEW_BATCH`；执行前复核路径包含及新目录，
只扫描本 batch 的 plan/trial/run。专项活动期间拒绝新普通任务，反向也拒绝。
CLI 保留 host lock、dirty recovery 和全部安全门。

## 取消与读取

等待项取消不启动 CLI。活动取消保留 cancelling，CLI 退出前不发布终态。
Unix 专项使用十秒有界 SIGINT 宽限，覆盖当前五秒 idle wait 和封存余量；普通调用仍两秒。
Windows 客户端仍是终止行为，不能声明已有 Unix 排空证据。
强杀/异常保留失败与真实退出码，不能当作干净取消；不自动重启服务或补跑 trial。

存在 batch 时，结束后用新的可取消、五秒有界 context 调用 `speed-results`；
不复用已取消执行 context，也不持有全局 mutex 执行 CLI IO。
Shutdown 等准备与队列清理完成后停止受管引擎。读取失败可以人工重读，不发送模型请求。

`ParseContextSpeed` 校验外壳、退出码、必需字段、重复键、五位置、计数与身份、
nullable 原因和统计一致性；展示采用 CLI 数值，不用 Go 计算值替换结果。
0/3 为可读完整/部分，2/4 为错误，命令取消130独立处理；错误无旧 rows/summary。

## 历史与展示

`workspace/logs/context-speed-tasks/TASK.json` 通过临时文件、fsync、rename 原子替换，
仅记录任务身份、生命周期和引用；与原始 batch 隔离，无六列缓存。
准备失败、排队取消及 batch 尚未创建也可恢复。中途退出留下的非终态记录显示 interrupted。

历史另扫描冻结 profile batch；计划不可读或越界的未知 batch 保留失败条目供 CLI 重读，
不凭目录名认定专项成功。冻结 config 仅供索引身份提示，成绩始终由 CLI 认证。
重测恢复原组合和 context；本地资源已不可映射时明确提示，不使用当前选择代替。

结果首列模型/引擎来自返回的 metadata；Context、Prompt length、Time、Prefill、Decode
保留单位与逐列缺测原因。五次有效时显示 CLI 中位数、范围及逐次表；不显示 GPU/显存。
默认窗口宽度完整展示六列表头；窄窗口或文字放大时保留横向滚动。
取消和未形成汇总的结果先显示中文说明；原始汇总原因码、逐次原因与逐列缺测详情
保留在默认收起的「查看诊断详情」中，展开不改变结果或重新执行测量。
读取旧任务不受当前模型选择影响；重新测评新建完整计划和 batch，不 resume 或覆盖旧证据。
专项不调用 report/export/HTML，普通任务仍保留自身报告范围。

## 检查边界

`internal/inferyard` 对照原28项黄金样例并测试能力/非法JSON；core 使用受控 CLI 响应
和 localhost 假引擎覆盖五次、零次、取消、Shutdown、路径与历史；页面测试和专项 parity
使用实际页面投影及 Service 边界的受控响应。模拟 fixture 仅在 `ui_debug && !release`。

`tests/e2e/context_speed_bridge.py` 复用 CLI 的 localhost Prism fixture，经公开命令五次运行，
保留原件前后 hash、请求数和无HTML证据；Go e2e 消费真实结果。
其中 host/review 是显式测试注入，不代表正式 init 已通过，也不代表64K性能测量。
原生 macOS、Windows/Linux窗口、真实模型与最终独立审核由后续验收承担。
