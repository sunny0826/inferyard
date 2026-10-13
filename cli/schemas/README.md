# 单一数据契约

当前 wire revision 为 `schema_version = 3`。全部 Schema 位于本目录；没有版本选择参数或活动旧版注册表。`Document.parse(kind, data)` 和 `validate_document(kind, data)` 拒绝旧 1/2 文档，历史原件由显式迁移工具读取并写入新目录。

权威接口说明见 [统一数据契约](../docs/data-contract.md)。定义入口为 [contracts/schemas.py](../src/inferyard/contracts/schemas.py)，按配置、题包/评分、事件/采样、实验、封存/汇总拆分；JSON Schema 仅约束结构，跨字段语义由 `contracts/validation.py`、`contracts/contracts_experiment.py` 与 `contracts/contracts_storage.py` 验证。扩展协议和模型来源/转换记录的纯结构定义分别位于 `contracts/schemas_extensions.py`、`contracts/schemas_lineage.py`。Schema 查询只依赖契约包与根版本常量，不加载配置业务或实时执行器。

| Schema                                                     | 用途                                          |
| ---------------------------------------------------------- | --------------------------------------------- |
| config_input / config                                      | 用户输入、默认值展开后的冻结配置              |
| bundle / score                                             | 统一题型、原始审核证明与客观评分              |
| experiment / plan / run / selection                        | 冻结预算、执行身份、重复和选题                |
| event / sample                                             | 实验、轮次、运行关联的事件及采集记录          |
| manifest / summary                                         | 文件哈希封存、固定题序或持续窗口汇总          |
| metric_definition / metric_observation / analysis          | 指标来源、缺测、派生分析                      |
| closed_concurrency / native_tools / total_observer_control | 专项协议；definition 版本独立于 wire revision |

生成与检查：

```bash
mise exec -- uv run --frozen python scripts/export_schemas.py
mise exec -- uv run --frozen python scripts/export_schemas.py --check
```

不要手工修改生成的 JSON。导出会移除目录内不属于当前注册表的旧 `*.schema.json`；原始 `validation/` 证据不受影响。

配置、题包及所有固定记录拒绝未知字段；布尔值不能冒充整数，拒绝 NaN/Infinity 与重复 JSON 键。缺测使用 null 并保留原因。固定计划五终态之和等于 planned；持续负载 planned 为 null，保留请求上限、实际发送与排空窗口。评分错误不能移除分母；不完整证据不得获得比较资格。

`upgrade_legacy_bundle(source)` 仅用于显式题包迁移。保留原审核记录和原 JSON 文本，逐项核验原 hash、原批准、题目、答案、规则、政策与元数据等价性；改题或移除原记录会使旧证明失效。未审核题包可以用于明确标识的诊断，不能因格式迁移获得人工批准。新内容仍需人工审核。

测量、引擎、采集器及评分算法的 definition 标识不随 wire revision 自动变化。模型/模板/引擎身份、端点本机限制、凭据引用与平台能力继续由预检和运行模块核验，结构合法不等于真实测量通过。
