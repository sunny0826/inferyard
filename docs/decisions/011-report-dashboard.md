# ADR 011：自包含离线测评报告

状态：Accepted

## 决策

报告从保存证据生成模型、机器、参数、质量、资源曲线与逐题详情；HTML 自包含，关闭服务后仍可阅读。
原始输出、评分与来源分别展示，筛选不改变分母。报告写新目录，不覆盖历史 HTML。

## 理由与代价

只展示总分无法解释错误与条件差异；依赖在线仪表板会降低证据可携带性。
内联资源增加 HTML 大小，但免去服务与 CDN；模型文本须转义。
SVG 展示由 [ADR 033](033-svg-generative-category.md)补充，描述性比较由
[ADR 035](035-purpose-specific-admission.md)补充，保存字节与当前渲染核验分离见
[ADR 037](037-offline-evidence-reading.md)。字段与版本见[报告契约](../contracts/report-contract.md)。
