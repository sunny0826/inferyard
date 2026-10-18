# 专项可选启动参数

本页补充[长上下文速度共享契约](context-speed-contract.md)的封闭启动参数规则。

两个可选带值选项支持分离与等号写法：`--device CUDA0` / `--device=CUDA0`，
`--log-verbosity 4` / `--log-verbosity=4`。`--device` 仅在 `engine.backend=cuda` 时允许，
值须整串匹配 `CUDA(0|[1-9][0-9]*)`（ASCII 数字，支持 CUDA2、CUDA10 及更大索引）；
拒绝前导零、负数、尾部字符/空白、Unicode 数字和多设备。`--log-verbosity` 只接受字面值 `4`。
两者均不新增别名、默认值或环境兜底；未出现时保持原行为。仅校验原始 argv，不修改参数；
候选设备选择及日志配方保留，实际绑定/预检仍核验设备与完整 startup_args。
这两个选项的缺值、空值或 `--` 长选项占值报 `startup_value_missing`，
重复（含分离/等号混写）报 `startup_option_ambiguous`，非法值或 backend 不符报
`startup_recipe_mismatch`；词表外选项仍报 `startup_option_unsupported`。
多错误并存遵循原有先失败顺序：追加 `--device -c 65536` 造成 context 重复时先报
`startup_recipe_mismatch`；若 `--device` 占用唯一 `-c` 的位置则报 `startup_option_ambiguous`。
这些可选参数不改变 schema_version=3、profile/result 结构、计时或证据支持集。
