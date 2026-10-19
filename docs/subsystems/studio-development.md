# Studio 源码开发环境

## 职责与边界

根级 [开发工具](../../tools/README.md) 负责从当前 checkout 准备配套 CLI/Studio、选择开发
profile、管理受管实例和原子切换。Studio 仍使用真实 `core.Service`、页面与 CLI 子进程；
`--debug-ui` 的隔离模拟路径保持独立。CLI 的 schema、证据、身份核验与宿主互斥契约不变。

本环境使用 mise 固定工具链，uv 读取现有锁文件；不修改全局工具安装、shell 配置或正式
Studio 设置。源码开发命令的操作步骤只维护在[开发工具说明](../../tools/README.md)。

## 事实源

| 部分 | 归属 |
| --- | --- |
| 命令入口、profile 选择和切换 | [main.py](../../tools/inferyard_dev/main.py) |
| 工具链核对、依赖同步、本地 wheel 与 Go 构建 | [build.py](../../tools/inferyard_dev/build.py) |
| 来源摘要、目录边界、启动器互斥与原子 JSON | [state.py](../../tools/inferyard_dev/state.py) |
| 实例记录、就绪确认与正常关停 | [lifecycle.py](../../tools/inferyard_dev/lifecycle.py) |
| 开发参数和真实服务装配 | [development.go](../../studio/development.go) |
| manifest 解析与产物核验 | [development_manifest.go](../../studio/development_manifest.go) |
| 进程身份、关停请求和会话回执 | [development_session.go](../../studio/development_session.go) |

## 运行路径与状态

1. 启动器持有本 checkout 的持久 `launcher.lock`，取得分支、HEAD 和相关源码、资源、锁文件
   的内容摘要。未提交、未跟踪的有效源码也参与摘要；用户计划与生成目录不参与。
2. 在全新的 build 目录同步锁定依赖、构建本地 wheel、安装 CLI、构建 `studio_dev` 原生
   程序。CLI 非 editable，已运行的两端不会随源码编辑而分离。构建前后输入变化使本次失败，
   不能将这个检查解释为原子源码快照。
3. 普通启动复用当前分支 profile；rebuild 分配新 profile。初始化使用尚不存在的 workspace
   路径。旧配置、预检、候选、绑定与历史证据不复制到新工作区。
4. 启动器写入独立 manifest，Studio 校验后再控制或启动实例。新产物核验成功才请求旧
   开发实例正常退出；身份或关停失败即停止切换。
5. 新窗口就绪回执必须匹配本次 session、实际子进程 PID 和固定 CLI 路径。就绪后才更新
   profile 的 current 指针和 checkout 的 active 指针。pending 指针保留尚未干净关闭的启动。

每个 build 保存原生产物、专属 CLI 环境、本地 wheel 与构建摘要。每个分支 profile 代保存
settings、window-state 和 workspace；结果与报告派生自该 workspace。所有分支共享本 checkout
内的 assets/models 与 assets/engines。引擎下载和扫描使用同一资产根，不隐式扫描正式用户
引擎目录。工作区、结果、模型与 CLI 路径在开发窗口内固定，下载目录名仍可保存。

## 约束与失败行为

- manifest 绑定 checkout、构建根、profile 根、资产根、源码摘要、程序摘要及已安装 CLI
  包摘要。版本号和 `source=checkout` 不承担 checkout 身份证明。
- 开发构建校验自己的可执行文件、CLI 入口和实际 Python import 位置；包摘要覆盖源码与
  资源，忽略 Python 字节码缓存。受管目录不能借符号链接写到 checkout 的开发根之外。
- 普通构建和带 `release` 的构建不接受开发参数，不能凭参数进入真实开发模式。窗口布局
  store 在第一个窗口创建前设置，正式 settings 与布局保持原归属。
  开发构建缺少启动 manifest 时拒绝进入普通用户环境；显式 UI 预览仍走模拟入口。
- `--dev-stop` 只操作 manifest 对应且 PID/启动标识吻合的进程，通过 session 专属停止请求
  进入既有取消、draining 和自有引擎关停路径。超时、身份不明、遗留所有权记录或关停失败
  明确报错，不强杀、不清锁、不删除原证据。
  停止流程不执行 CLI，只核验 Studio 实例与所有权，因此 CLI 环境损坏也能正常关停。
  启动和 `--dev-check` 仍完整核验 CLI 入口、包摘要与实际导入位置。
- CLI 宿主锁和 dirty 标记继续使用真实宿主范围；开发目录隔离不创建第二套互斥状态。
  证据解释沿用[血缘规则](../../cli/docs/data-contract.md#证据血缘与比较结论)。
- 构建失败时旧实例尚未被关闭，指针不切换。新窗口启动失败时保留该次 manifest、日志和
  工作区；无法确认关停的实例保留 pending 所有权，后续命令先处理它。
  子进程已退出也必须经过同一停止控制器核验；关停失败、遗留引擎所有权或会话无法核验时，
  仍保留 pending，不把进程退出当作清理成功。只有核验成功或子进程从未创建才释放该记录。
- settings、布局和指针是各自唯一拥有者的可变状态；manifest、build 记录和原始证据只写
  新路径。旧代不自动清理。

## 验证边界

[启动器行为回归](../../tools/tests/test_dev_lifecycle.py) 检查失败不切换、历史与资产保留、
实例归属和 pending 恢复；[状态回归](../../tools/tests/test_dev_state.py) 检查源码变化、
分支隔离、重复执行、符号链接与 JSON 拒绝路径。它们使用受控进程边界，不证明原生窗口。

Go 开发测试覆盖协议、固定绑定和关停路径；普通、`studio_dev`、`ui_debug`、
`studio_dev,release` 与 `ui_debug,release` 分别验证入口边界。真实开发窗口需要在已登录桌面
启动并检查来源和 CLI 调用；预览原生 E2E 使用模拟数据，其通过不等于开发链路或实模验收。
平台与实际执行结果按本轮验证证据报告，不从交叉编译推导原生平台资格。
