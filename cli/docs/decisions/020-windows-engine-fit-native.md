# ADR 020：Windows 原生 engine-fit 诊断

状态：Accepted

## 决策

Windows engine-fit 使用独立 run.v6，仅接入单 GGUF llama.cpp，接受 plan.v2/v3/v4。
绑定核验同账户 PID、完整创建 FILETIME、文件哈希、argv/cwd、模型与唯一监听，
并拒绝任意大小写的 `LLAMA_ARG_` 环境配置。旧定义不扩大平台枚举。

稳定活进程树记录 CPU 秒与 working-set bytes；有成员缺测时不发布部分和。
温度仅采用明确来源的 NVIDIA GPU 读数，CPU 温度保持缺测。

## 理由与代价

复用 Linux `/proc` 或 Darwin 字段会误读时间与资源；只凭 HTTP 模型名不能证明本机资产和 idle。
新定义增加维护面，但保持旧证据的来源含义。启动文件身份不证明加载字节或 GPU 驻留，边界采样不表示峰值。
核心 KVMem/NInfer run 支持与本诊断分别维护，不将它们伪装为 llama.cpp。

接口见[Windows engine-fit 契约](../contracts/engine-fit-contract.md#windows-原生来源)，原生覆盖见[平台状态](../platforms.md)。
