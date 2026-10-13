# 持续负载发送准入契约

依据 [ADR 018](../decisions/018-duration-send-admission.md)。

## 接口和权威

Prism 的内部 `generate(body, timeout, emit, *, before_send=None)` 允许可选同步回调。
客户端请求起点由 adapter 的新鲜 monotonic_ns 采样定义；回调接受该值，返回 False
表示拒绝、True 表示准入。准入后的 ResponseState.t_send_ns 与该采样相同。
该接口不改变网络/API 请求、wire 字段、核心 schema 或现有指标方法。

TrialRequests 只对带 admission_deadline_ns 的持续请求提供回调；时间严格小于
截止才登记 request_started 和推进 next_formal。普通请求的调用与默认 adapter 行为保持兼容。
拒绝应返回 None、无 HTTP POST、无正式尝试/终态；快照和 dirty 准备可保留，清理仍需 idle。
未准入即取消时没有正式终态；已准入取消保留既有 cancelled/排空语义。
没有正式尝试的清理 idle 观察应使用 null request_id，不能引用不存在的开始记录。

## 责任和拒绝

adapter 负责采样、回调和生成；runtime 负责截止、持久化、身份、锁、计数和清理。
计数仍按真实客户端 t_send 的半开窗口归组；read_trial 的守恒拒绝不改动。
工具/写盘失败保留 dirty，即使外层同时收到取消也不能吞掉该异常或清理未知状态。
不能用返回 None 掩盖已准入尝试；没有准入回调的调用不能
以 None 伪造成功。旧数据读取和失败原件不改写。

## 风险与验证

回调写盘开销进入新持续请求耗时，不能套用旧采集资格；资源与传输仍为原定义。
确定性时钟与 MockTransport 覆盖截止前后、任务排队和取消；代表性执行保存正式计数、
发送 cohort、终态和封存哈希，不以单项偶然通过替代竞态回归。
