# 离线读取与定位契约

依据 [ADR037](../decisions/037-offline-evidence-reading.md) 与
[证据血缘总规则](../data-contract.md#证据血缘与比较结论)。

## 原始产物与退出码

verify --path 支持运行目录、batch 目录及冻结 plan 文件/目录，复用已有严格读取器。
已封存且来源/语义核验通过的 run 返回 0/verified，包括原执行取消或 partial。
details.sealed、verified、integrity 表示证据状态；execution_completeness、stop_reason
及结果顶层 completeness 保留原执行状态，不以 code 0 宣称执行完整。
未封存取证返回 3/partial、sealed=false、verified=false，仍返回已有答案和原执行状态。
batch 使用 history 的已存身份内部一致性路径，不调用续跑 open_batch 的当前实现准入。
继续核验 plan/run 来源、父链/事件摘要、冻结输入与服务绑定；源码更新不视为旧证据损坏。
batch 无整体 seal（sealed=false），逐 run 列封存与执行状态；已有运行全部封存且核验
通过则 code 0，即使计划尚有未运行 trial。空 batch 或含未封存运行返回 3/partial。
计划未完成与证据损坏分别表达，坏来源/哈希/链始终为 4。
冻结 plan 返回 validated/frozen_document（0），不表示已执行。坏输入、坏 seal、来源失配
仍为 4；调用参数非法为 2，取消仍 130。不会写回被验证的目录。

engine-fit 最小可读取证文件为 plan.json、checkpoint.json、requests.json。
checkpoint 定义 engine_fit_checkpoint.v1，保存请求前 run 身份、初始实际资源边界及计划
绑定；请求数组沿用原原子检查点。读者核对初始身份和当前冻结请求顺序/终态/答案结构，
从当前数组计算计数，并明确 interrupted_checkpoint，不推定服务 clean、结束或资源峰值。
原 run v1–v6 结构不因该检查点改变，只有新封存 manifest 升级。存在 manifest 时必须
完整核验，缺文件或摘要不符不得使用 checkpoint 降格读取。
只有 manifest 目录项真正缺失才允许 partial 分派；悬空符号链接、非普通文件或不可读
状态是证据错误。专用 verify 与直接 read_partial 共享这一 nofollow 判定。
checkpoint.json 是在途辅助文件，最终封存后固定保留但忽略，不加入 manifest 的原四文件。
此时最终 run.json 是身份与资源权威；checkpoint 缺失或损坏不参与 sealed 读取，也不能
掩盖 manifest 所列文件损坏。核验不删除或改写 checkpoint，不自动恢复执行。

## 新呈现封存与定位

report v7 的 artifact-manifest.json 封存 index.json/report.html（比较时另含 comparison.json）。
comparison v4 保留 phase2.v3 计算语义，仅改变来源定位结构；旧 v1–v3 不改义。
engine-fit manifest v2 继续封存原四文件和携带来源；旧 v1 仍校验当前渲染重建。
新验证分别报告 bytes_verified、sources_verified、semantic_verified、render_checked。
新格式默认不调用 renderer，--rerender 显式请求当前 renderer 一致性；渲染变化不能
自动改写原报告。摘要一致不独自代替来源和语义核验。
未重渲染时 render_checked=false，不返回 html_matches_index=true；未封存取证也不
因传入 --rerender 而声明已渲染核验。旧八类产物的退出语义保持。

普通报告 source.path 为相对报告目录的外部定位，可含 ..；manifest 摘要和 run_id 仍是
身份。--source-root OLD=NEW 可重复，以明确前缀映射定位，拒绝重复/歧义根，不搜索磁盘。
原始数据/HTML 字节不改。携带包 sources/N 始终限于包内且拒绝 symlink/逃逸，不应用
外部根映射。旧格式保持旧绝对定位规则，不追改旧字节。

未封存的原始 run 可生成描述报告：新 source 另存 unsealed_content_sha256，绑定本次
严格读取所得数据投影；manifest_sha256 保持 null，不把投影摘要称为原始封存。
显示链接按保存的来源定位确定性重建；显式映射仅改变验证读取的位置，不修改原链接。
旧报告不支持源根映射，须保留其原定位；新比较中已绑定的旧总开销源绝对引用可按显式
根映射定位，原绑定文件字节及 manifest 摘要仍核验。

## 共享边界

离线读取共享有来源的能力证据、checkpoint 计数与严格 JSON 小函数；不统一业务
runner/HTML，不新增资源日志或请求双写。公共 CLI 转换保留，在应用边界构造明确离线
验证选项。纯冻结/离线函数移出 runtime 时同步职责清单，源码身份按实际变化。
能力推断只消费已读且经过来源校验的 props/manifest，保留 lab/native 未知降级及旧账本结果。

Prism 的历史能力定义仍绑定已知 build 内容与来源，集中在 EngineCapabilities 解释，
不是删掉已知定义边界；未知 build 或单独自报 true 不授予 timings/预算/协议资格。
llama.cpp 与明确 Python module/受控 MLX script 入口不以 executable basename 授权；
实际二进制摘要、argv、模型路径、监听归属、运行中变化及服务协议检查保留。
MLX script 内容 hash 必须匹配随包入口。已支持纯日志参数继续允许，未知加载参数不扩白。
LM Studio app/helper 发现路径尚无等价识别证据，保留原限定，未加通用 allow 选项。
