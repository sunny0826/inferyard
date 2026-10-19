# Catalogue 与 Schema 生成

本页汇总 catalogue、schema、社区资源和报告模板的生成边界。详细规则按主题分开维护：

- [事实源与生成物](catalogue-generation-sources.md)
- [运行路径](catalogue-generation-paths.md)
- [约束与验证](catalogue-generation-rules.md)

本子系统把可审查的 Markdown 生成源与契约包定义固化为随安装包交付的 JSON 资源（方法/指标 catalogue、JSON Schema、社区资源清单与报告模板），并提供 `--check` 漂移核对。它只负责“生成与一致性”：不解释指标语义与比较资格（见 [评分、比较与报告](scoring-reporting.md)），不执行运行时消费（见 [运行时执行与引擎适配](runtime-execution.md) 与 [CLI 与应用分发](cli-dispatch.md)），不发行分发包（见 [脚本手册](../../cli/scripts/README.md) 的发行物一节）；通用层职责见 [架构](../../cli/docs/architecture.md)。

边界另一侧同样清楚：题包创建脚本（`build_*_bundle.py` 系列）产出待审原题与审核材料，属 [配置、契约与冻结计划](config-plans.md) 范畴；`export_community.py` 只搬运**已审核**题包的原字节，不创建或修改题目内容。运行时对 catalogue 与模板的消费不做网络请求，也不据此声明任何实测资格。

## 职责与事实源

见 [事实源与生成物](catalogue-generation-sources.md#职责与事实源)。

## 运行路径

见 [运行路径](catalogue-generation-paths.md#运行路径)。

## 约束与失败行为

见 [约束与失败行为](catalogue-generation-rules.md#约束与失败行为)。

## 验证

见 [验证](catalogue-generation-rules.md#验证)。
