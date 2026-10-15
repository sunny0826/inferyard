# ADR 052：退役独立 Observer 项目

状态：Accepted。取代 [ADR 009](009-native-observer.md) 与
[ADR 010](010-observer-benchmark-adaptation.md)。

## 决策

移除独立 `observer/` Go module、`inferyard-observer` 命令的源码和构建器，
以及 CLI 工程目录中的独立流解析、摘要和测评关联工具与专属测试。
当前仓库只维护 Python CLI 和原生 Studio 两个产品。

独立监测流和摘要不再由当前版本读写；既有原件保持原字节，历史实现由 Git 追溯，
不增加转换器、兼容 stub 或新的 CLI 子命令。
支持边界的事实源为[格式支持矩阵](../contracts/inferyard-current-format.md#格式支持矩阵)。

## 理由与边界

项目已决定停止维护独立监测器。它的仓库内消费者仅有工程脚本中的构建器和离线桥；
CLI 包、Studio 与发行工作流不调用独立二进制。保留桥接脚本会留下无法维护的另一套数据接口。

CLI 内部资源采样、环境与安全观察、LM Studio 原生观察接口、四臂总开销实验继续使用
各自现行协议。它们不依赖独立项目，不能因名称包含 observer 一并删除。
核心 schema 3、运行 layout1/layout2 的既有读取范围、评分与报告字段保持不变；
本次不产生新的原生采集来源，也不授予新的设备或性能资格。

已安装的二进制、本地运行和历史证据不随源码退役删除，不改线上配置或服务。

## 代价

当前源码不再提供独立 snapshot/watch、监测流摘要或 `--bench-run` 关联。
原有监测数据不会自动转成 CLI 运行证据。保留的 CLI 和 Studio 功能通过现有回归分别验证。
