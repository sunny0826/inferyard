# ADR 002：领域包与薄 CLI

状态：Accepted。日期：2026-10-01。

## 决策

在[单一契约](001-unified-contract.md)基础上，按 cli、application、contracts、config、runtime、evidence、analysis、reporting、platforms、extensions 和 adapters 划分职责，保留 `inferyard` 与 `python -m inferyard` 两种入口。

CLI 只处理参数、请求和呈现；应用类型唯一放在 `application/types.py`，业务模块不依赖 CLI。实际分派延迟加载后端，帮助、版本和 Schema 查询不加载实时执行模块。未知请求明确拒绝。

安装资源按包根读取，源码身份覆盖完整包。脚本保留既有物理路径，历史 `validation/` 快照、内部路径和封存字节不改写。

原先保持全部命令和参数名称的限制由 [003](003-cli-surface.md) 部分修订；领域、类型、延迟加载、安装资源和证据边界继续有效。

## 理由与代价

仅拆 CLI 文件不能表达业务和资源边界；维护每个旧内部模块的包装会留下两套入口。采用领域包，保留既有公共入口，内部导入指向实际模块，不引入额外框架或运行依赖。

包移动带来循环导入、资源打包和测试注入路径风险，需要验证冷启动、分派、安装后入口与离线流程。模块或目录数据变化形成新源码身份，不能继承旧实测资格；源码重构不扩大平台范围。

目录和接口约束见[架构](../architecture.md)。
