# 规范语法与兼容入口

本页是[CLI 接口](../cli-surface.md) 的子文档，收录「规范语法与兼容入口」的正文；内容不变，入口保留原节标题与锚点。

| 规范语法                                              | 已删除的兼容语法                                 | 语义                               |
| ----------------------------------------------------- | ------------------------------------------------ | ---------------------------------- |
| `verify --path DIR`                                   | 八个 `*-check --run DIR`                         | 自动识别产物，调用专用验证器       |
| `verify --path PUBLIC --config CFG`                   | `public-config-check --run PUBLIC --config CFG`  | 核验公开包与声明配置               |
| `verify --path PUBLIC --source-run RUN`               | `public-check --run PUBLIC --from-run RUN`       | 核验公开源投影                     |
| `verify --path OVERHEAD --target-run RUN`             | `overhead-check --run OVERHEAD --target-run RUN` | 核验开销目标绑定与阈值             |
| `probe --config CFG`                                  | `check --config CFG`                             | 发送普通/流式探测，保存诊断证据    |
| `plan --experiment FILE`                              | `plan --config FILE`                             | 输入为实验定义                     |
| `report --runs RUN [RUN ...]`                         | `report --run RUN`                               | 一至多个运行目录                   |
| `run --rerun-from RUN`                                | `run --from-run RUN`                             | 整体新建一次重跑，要求真实服务绑定 |
| `public package --run RUN --out DIR`                  | `public-package` 的相同参数                      | 生成脱敏公开包                     |
| `public plan --run PUBLIC --config CFG --out DIR`     | `public-plan` 的相同参数                         | 冻结复现计划                       |
| `extension freeze --spec FILE --config CFG --out DIR` | `extension-freeze`                               | 输入原始扩展规范                   |
| `extension run --plan FILE --config CFG --out DIR`    | `extension-run --spec FILE`                      | 输入冻结扩展计划                   |
| `extension replay --packet FILE --out DIR`            | `extension-replay --spec FILE`                   | 离线导入合成 packet                |

表中的旧命令名和参数别名已删除，调用返回输入阻断；只接受规范语法。扩展 run/replay 分别使用 `--plan` / `--packet`；freeze 继续使用 `--spec`。`resume --from-run` 只续测未执行部分，与整体重跑不同。

报告、评分、导出、公开包及冻结计划的 `--out` 要求新产物目录；新批量运行使用不存在或为空的 `--output-root`，续测绑定同一批次根目录。扩展 run/replay 的 `--out` 是输出根目录，其下创建新的封存子目录，stdout 的 `evidence_dir` 才是可核验的具体目录。

已删除的命令也不再提供帮助。八个 `*-check` 为 `overhead-check`、`repeat-check`、`rescore-check`、`export-check`、`public-check`、`report-check`、`compare-check`、`extension-check`；`public-config-check` 同样由表中的 `verify` 语法取代。
