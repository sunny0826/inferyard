# 历史操作者策略样本

`safety.json` 提取自基线 `c0fd763` 的
`validation/phase2/P2-13/local-reproduction-08/input/experiment.json` 中 `safety` 对象，
字段和值保持不变，重新序列化为小型 JSON；它不是封存证据。
用于保留原操作者回归：温度只观察、环境检查和外部 CPU 上限仍保留。
不读取旧实验目录，不代表当前默认安全策略，也不授权真实模型运行。
