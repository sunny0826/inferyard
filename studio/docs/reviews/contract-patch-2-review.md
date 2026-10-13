# 冻结契约补丁 3eb5a0b 复核报告

- 复核对象：`3eb5a0b`（docs(contract): optional GPU index, §4.6 rationale fix, §4.7 .part lifecycle），基线 `aa17ed9`
- 复核角色：reviewer（冻结治理流程：coordinator 仲裁 + reviewer 重审），独立复核事实与措辞
- 复核日期：2026-10-13
- 事实来源：主仓库 `/Users/guoxudong/codes/local-ai/inferyard` 只读核对；L2 证据 `/tmp/studio-t9/evidence/`

## 一、逐项结论

### P1 `DeviceGpuDevice.index` → 可选（docs/api-types.ts:95）— 落实

diff 为单行修改 `index: number` → `index?: number`，无其他字段变动。

事实核对（主仓库 `src/inferyard/platforms/device_preflight.py`）：

- :72 `"index": index,` 确在 NVIDIA/nvidia-smi 解析分支的 `devices.append(...)` 内（CUDA 选卡语义），行号精确命中。
- :116 `"gpu": env.get("apple_gpu")` 确为 Darwin 分支取值，行号精确命中。
- `hardware_snapshot` 函数体 :92-147：Darwin 下 `gpu` 字段直接取 `apple_gpu` 环境快照，不经过 NVIDIA 分支，快照形状无 `index`。
- `recommend()` :167 过滤条件含 `and "index" in g`，消费方确按条件存在处理，行号精确命中。

证据核对：

- `/tmp/studio-t9/evidence/device-get.json`：`body.snapshot.hardware.gpu.devices[0]` 为 Apple M4（`memory_kind: shared_host`），无 `index` 键——实测确认。
- `/tmp/studio-t9/evidence/zod-device.json`：记录前端 zod 对 `device-get.json`/`device-get-2.json` 校验失败于 `devices.0.index`（expected number, received undefined），证明放宽的必要性真实存在。
- 服务端透传路径：`internal/core/service.go:267` 快照以 `json.RawMessage` 持有，Go 侧无 struct 重序列化，不会注入 `"index":0`，与 L2 实测响应一致。

放宽性质判断：成立。`index: number` → `index?: number` 是类型超集化，NVIDIA 平台的既有响应（带 index）仍合法；契约注释本已声明「CLI 新增字段两端必须容忍」。前端消费侧修复由先行提交 `aa17ed9` 完成（`web/src/api/types.ts:95` 已为 `index?: number`），本补丁使权威镜像与之一致。不破坏既有客户端与数据。

### P2 §4.6 理由句勘误（docs/api-contract.md:297-299）— 落实

事实核对：主仓库 `src/inferyard/reporting/report_common.py:83` 确为
`out.parent.mkdir(parents=True, exist_ok=True)`（`_new_output` 内），行号精确命中。即 CLI
实际会自动创建父目录，旧理由「CLI `--out` 要求父目录已存在」不成立，勘误正确。

行为不变性判断：成立。正文指令「服务端先创建 `reports_root` 父目录」原样保留，仅括号内理由
更正为「服务端主动保证目录存在，不依赖 CLI 该行为，S10 挂账」。全文检索无旧理由残留
（`要求父目录` 已无匹配）。纯措辞勘误，无行为变更。

### P3 §4.7 `.part` 生命周期澄清（docs/api-contract.md:334-336）— 落实

事实核对（主仓库 `src/inferyard/platforms/model_source_download.py`）：

- :59 `temporary = name + ".part"`，`temporary` 即 `.part` 文件。
- :78-83 精确覆盖 `except BaseException:` 处理块：遍历 `(temporary, "source.json.part")`
  并 `unlink()`（`OSError` 容忍），随后 `raise`。行号区间精确命中。
- `BaseException` 涵盖 `KeyboardInterrupt`，与契约上文「SIGINT 子进程（退出 130 →
  `cancelled`）」衔接，「中断（含取消）时主动清理」的表述成立。

证据核对：`/tmp/studio-t9/evidence/cancel-files.json` 显示取消后 `out_dir`
（`/private/tmp/studio-t9/models/qwen25-q2k`）保留且 `total: 0`、无文件——目录保留占名、
无 `.part` 留存，与澄清表述一致（T9-L2 实测确认属实）。

行为不变性判断：成立。既有承诺「失败/取消目录保留，不自动删除」原样保留并澄清为「目录保留
占名（同名 409 `output_exists`）」；`output_exists` 语义在 §4.7（:324）与错误表（:67）中已有
依据，新增句子不含新承诺或新义务。纯措辞澄清。

## 二、意外改动扫描

`git show 3eb5a0b` 仅触及 2 个文件、4 个 hunk（8 insertions, 3 deletions）：

1. `docs/api-contract.md` 版本行注记 +1 行（补丁 2026-10-13 摘要，内容与三处修改一一对应）；
2. `docs/api-contract.md` §4.6（-1/+3，理由句勘误）；
3. `docs/api-contract.md` §4.7（-1/+3，.part 澄清）；
4. `docs/api-types.ts`（-1/+1，index 可选）。

与提交信息声明的三处补丁 + 版本注记完全一致，**无意外改动**；无代码、测试、配置或其他文档
被触及。工作区 `git status` 除本报告目录（`docs/reviews/`，未跟踪）外干净。

## 三、镜像一致性验证

- 工作区：`diff web/src/api/types.ts docs/api-types.ts` → 为空。
- 提交内：`git show 3eb5a0b:web/src/api/types.ts` 与 `git show 3eb5a0b:docs/api-types.ts`
  对比 → 为空。
- `web/src/api/types.ts:95` 确认为 `index?: number;`，与权威镜像逐字节一致。

前端修复（`aa17ed9`）先行、本补丁恢复镜像字节一致，验证通过。

## 四、§9 Q1 留档无需同步的判断 — 复核成立

Q1 留档实际位于 `docs/api-contract.md:418-419`（仲裁简报所称 413-414 略有偏差，不影响结论）：

> 接受派生（`dirname(results_root)/reports`），不设第四项设置 → §4.2；`reports_root` 父目录由
> 服务端创建 → §4.6。

该条目只陈述两项行为事实（派生规则、父目录由服务端创建），并未复述被勘误的理由
（「CLI 要求父目录存在」）。补丁后 §4.6 仍保留「服务端先创建 `reports_root` 父目录」指令，
Q1 陈述的事实继续成立。writer「无需同步」的判断正确。

## 五、附注（不构成修订要求）

- 提交信息引用证据文件为 `zod-device.json`，正文注记表述为「L2 真实证据」；`zod-device.json`
  与 `device-get.json` 均存在且内容互相印证（前者是对后者的 zod 校验记录），引用均有效。

## 总体结论

**通过。** 三处补丁全部落实，行号与措辞同主仓库事实逐项吻合；P1 确为放宽、P2/P3 确为纯措辞
澄清，无破坏性变更；diff 无意外改动；前后端镜像恢复字节一致；§9 Q1 留档无需同步的判断成立。
