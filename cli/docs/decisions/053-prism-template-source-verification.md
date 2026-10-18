# ADR 053：分别核验模板原件与 Prism 服务表示

状态：Accepted

## 背景

Prism `b10743-adfffbe41` 的 `/props.chat_template` 来自 Jinja lexer 的 source，
不总是 GGUF 内模板原字节。该版本先归一化 CRLF/CR，再根据原文是否以 LF 结尾去掉一个 LF。
真实 Llama 3.2 1B 模板末尾带 LF，原件哈希与服务表示不同，旧比较错误拒绝了它。
依据为该版本的 [lexer](https://github.com/PrismML-Eng/llama.cpp/blob/adfffbe41/common/jinja/lexer.cpp)
及 [模板构造器](https://github.com/PrismML-Eng/llama.cpp/blob/adfffbe41/common/chat.h)。

## 决策

保留原模板文件与 `template_sha256` 的原字节语义。服务核验先重新检查原件哈希，
再从这个已核验原件推导固定 build 的服务表示并精确比较。不能对两边通用 trim，
不能把从服务取得的模板写成新的信任来源，也不能略过原件变化。
此规则只属于上述 Prism build，其他适配器不继承。
具体变换与拒绝规则见 [Prism 模板身份契约](../contracts/prism-template-contract.md)。

这是身份核验实现修复，按身份检查变更记录 ADR、契约及实施计划；不作为可选开关。
没有字段、枚举或单位变更，schema 仍为 3。旧原件、失败结果与哈希不改写；
新源码不继承旧资格，新的实测写新目录。读取和比较遵循[血缘规则](../data-contract.md#证据血缘与比较结论)。

## 验证边界

软件回归覆盖 LF、CRLF、CR、双 LF、其他空白、模板内容及原件替换、未知 build；
macOS 正式服务验证单独进行，不能外推 Windows 或其他引擎版本。
