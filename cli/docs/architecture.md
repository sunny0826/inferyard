# 当前架构

架构说明按阅读顺序拆开，原章节标题保持不变：

- [证据通路与源码边界](architecture-overview.md)
- [请求、运行与计量](architecture-runtime.md)
- [保存、离线读取与方法边界](architecture-evidence.md)

本文件描述活动源码的职责、执行和证据边界。数据格式见[数据契约](data-contract.md)，用户语法见 [CLI 接口](cli-surface.md)，平台能力与实测范围见 [README](../README.md)。

产品测量本地 AI 在冻结任务上的质量、等待时间和资源需求，使用确定性评分和可核验证据。
不包含自动模型下载、服务编排、模型裁判、社区账号或多机调度。通路见[流程图](show-me-system-flow.html)。

## 一条证据通路

见 [证据通路](architecture-overview.md#一条证据通路)。

## 源码领域包

见 [源码领域包](architecture-overview.md#源码领域包)。

## CLI 与应用边界

见 [CLI 与应用边界](architecture-overview.md#cli-与应用边界)。

## 请求与运行

见 [请求与运行](architecture-runtime.md#请求与运行)。

## 计量与比较

见 [计量与比较](architecture-runtime.md#计量与比较)。

## 保存与离线读取

见 [保存与离线读取](architecture-evidence.md#保存与离线读取)。

## 方法与证据边界

见 [方法与证据边界](architecture-evidence.md#方法与证据边界)。
