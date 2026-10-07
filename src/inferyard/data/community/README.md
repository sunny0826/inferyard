# 离线测评工作区

此目录是准备材料，不是已验证配置或测评结果。题包原字节及内嵌人工审核信息保留。
`preparation.json` 的 bundle_path 指向本次选择的题包。模板中的 REPLACE 项都需要填写；
PID/start_ticks 的 1 是未绑定哨兵，不是真实进程身份。不要直接 run 示例。

1. `inferyard device-check --model MODEL --out NEW_PREFLIGHT --json`。
2. Windows 先从固定归档 `runtime prepare`，再 `config create`；macOS 直接 `config create`。
   Linux 用 `config assets` 导出模板和身份，再手工填写 configs/linux.example.toml 的副本。
3. 操作者外部启动模型服务，用 `config bind --candidate FILE --pid PID --endpoint URL --out NEW`。
4. `run --config NEW/config.toml` 自带当次普通/流式探测；独立 probe 仅作可选排障。
5. `report --runs RUN --out NEW_REPORT`，再 `verify --path NEW_REPORT`；HTML 可离线打开。

所有新准备入口的 --out 都是不存在的新目录；父目录须存在，失败后换新目录重试。
不下载模型、不隐式启动服务。模板和哈希不替代资产、身份、预算与停止条件核验。
Windows 候选引用 runtime prepare 目录的资产及清单。不要移动/删除它；移动/删除后须重新准备
候选并绑定，CLI 不自动复制资产改变路径语义。完整命令见项目 docs/installation.md 的随发行指南。
