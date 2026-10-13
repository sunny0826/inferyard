# ADR 009：独立单文件系统与进程监测器

状态：Accepted

## 决策

`inferyard-observer` 使用 Go 标准库，每个 OS/架构交付单一可执行文件，提供 CLI、JSON/JSONL。
原生读取系统与指定进程 CPU/内存，不加载模型；可选 loopback 健康查询不发送推理请求。

## 理由与代价

独立二进制避免为了观察资源安装 Python 或推理依赖；原生来源避免周期启动系统命令。
代价是分平台维护进程时间、内存与退出语义。CPU 按单核百分比，首样本缺基线为 null，
进程存活、端点可达、模型加载和生成分别解释。

六目标可交叉编译，原生运行覆盖需分别记录。接口见[observer 契约](../contracts/observer-contract.md)，
使用与覆盖见[observer 说明](../../observer/README.md)，与 benchmark 的关联由 [ADR 010](010-observer-benchmark-adaptation.md)定义。
