# 长上下文速度题库草案 · pending

人工审核未完成。用户认可的是 UI 原型，不是本题库；两类审核记录均为空。
这是已做参考正文离线分词的固定草案，不能称为可正式测评的已审题库。

## 全文与身份

三篇全文保存在 [zh-context-speed.json](zh-context-speed.json) 的各 `cases[i].prompt`。
字符数、UTF-8 字节数、全文与逐题 hash 见 [zh-context-speed.review.json](zh-context-speed.review.json)。
版本 `2026.10.1`；bundle content SHA256：`a92e8b9f9e108071d979efc598c3ed55a047220a893b9a8c85525357e6d31407`。

| case | 全文 JSON Pointer | 字符数 | UTF-8 字节数 |
| --- | --- | ---: | ---: |
| context-speed-64k | `/cases/0/prompt` | 30999 | 86611 |
| context-speed-128k | `/cases/1/prompt` | 62793 | 175443 |
| context-speed-256k | `/cases/2/prompt` | 126402 | 353030 |

## 生成与许可

本项目原创合成材料，采用 [MIT 许可](../LICENSE)，无外部摘录或下载。
生成器版本 `context-speed-corpus.v2`，固定种子 `inferyard-2026-10-18-context-speed`。
按固定档案数71/144/290生成；12主题轮换，每12条换一种结构，共6结构；SHA256(seed|index|field)前8字节的大端整数取模选数据；LF换行、UTF-8、无时间戳、无运行模型输入。
主题：河道观测、种子保存、古籍修复、公交调度、陶瓷烧制、山地步道、社区借阅、食品晾晒、海岸照明、戏剧排练、雨水回用、手工织造。
结构：过程叙述、批次表、访谈摘记、JSON摘录、工作步骤、方案对照。
生成器源文件摘要在绑定 JSON 中；按固定顺序生成，不能按运行模型调整文本。

模板与边界段落重复，非真实人类语料，不代表所有长上下文任务。
三档共享前缀档案，不能用它们证明跨档独立性或缓存性能。
档案关联仅为检索线索；合成数值不构成实测或事实。
参考计数只覆盖正文、无chat模板和BOS；8K参考模型不证明专项档位能力或实际预算。

## 规则与五次运行配方

三个 case 均为 performance，参考答案为空，唯一规则为 output_target_tokens=256。
不设质量分、正确率或参考答案评判。请人工检查三篇全文、规则、许可和合成局限。
五次运行配方以[专项规划](../docs/plans/context-speed.md#专项题库与运行协议)为来源：
一个选中 case，5 个完整 trial；并发/槽位 1，每 trial 一次短文本预热；
strict_fixed_length=256、空 stop、关闭 prompt cache 与投机解码；
temperature=0、top_p=1、top_k=1、min_p=0、presence_penalty=0、
repeat_penalty=1、seed=0、reasoning_mode=off。seed_support 必须实证核验。
该配方由后续 profile/plan 实现并冻结；本题包不执行或保证五次运行。

## 待核验与准入

参考负载目标 52,625 / 106,294 / 213,612 token；正文实测 52,469 / 106,313 / 213,940。
参考为 Prism b10743-adfffbe llama-tokenize + SmolLM2-135M-Instruct-Q4_K_M.gguf，
工具与模型 SHA256、计数选项均在绑定 JSON 中；--offline，逐个进程最多60秒。
只对完整 prompt 字符串计数，不套 chat 模板、不加 BOS、不解析特殊标记、不处理转义。
参考模型原生 context 仅8192；分词成功不证明支持64K/128K/256K或能进行推理。
actual_input_tokens 仍为 null，实际模板后计数和预算须由被测服务逐请求核验。
生成器只复制有正文 hash 绑定的冻结观测，不调用分词工具；正文改变会拒绝旧计数。
如需调整，生成新版本和新审核材料，保留当前原件；运行时只核验预算，禁止截断或换题。
实际输入 + 256 必须不超过对应 context（65,536 / 131,072 / 262,144）。
人工批准必须绑定当前内容 hash；自动校验与 Agent 自审不能生成批准。
require_review 应拒绝当前草案；不注册社区资源，不绕过正式准入。
