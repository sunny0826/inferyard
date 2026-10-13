# ADR 005：macOS 原生执行

状态：Accepted

## 决策

macOS 单次与批量执行共用核心请求生命周期，通过平台工厂选择原生进程身份与 `macos-resource.v1`。
不把 Linux `/proc`、jiffy、CPUFreq 或 socket inode 语义套到 Darwin。活动数据格式保持 v3，来源独立记录。
Metal 需原生硬件能力、正数 offload、绑定动态库及实际加载核验；模型文件绑定与 GPU 驻留分别表述。

## 理由与代价

只放宽平台允许列表会把身份与采样单位混用。独立后端增加平台维护面，但可以保留共享运行、取消、排空和封存逻辑。
启动身份依赖锁定 psutil 的原生私有接口，失效时拒绝而不猜测；依赖替换须覆盖原生身份回归。
安装资源从包内读取，历史证据不作为安装依赖。

温度和电源来源由 [ADR 006](006-macos-performance-prerequisites.md)补充，诊断准入由
[ADR 035](035-purpose-specific-admission.md)部分替代。现行字段见[macOS 契约](../contracts/macos-contract.md)，覆盖范围见[平台状态](../platforms.md)。
