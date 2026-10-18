"""Create a fixed, unapproved context-speed corpus and human-review materials."""

import argparse
import hashlib
import json
from pathlib import Path

from inferyard.config.bundle import content_hash, validate_bundle
from inferyard.config.bundle_review import case_content_hash

if __package__:
    from .context_speed_corpus import GENERATOR_VERSION, SEED, SPECS, TOPICS, prompt
else:
    from context_speed_corpus import GENERATOR_VERSION, SEED, SPECS, TOPICS, prompt

ROOT = Path(__file__).resolve().parents[1]
BUNDLE_NAME = "zh-context-speed.json"
REVIEW_NAME = "zh-context-speed.review.json"
MARKDOWN_NAME = "CONTEXT-SPEED-REVIEW.md"

# Frozen offline observations, bound to exact prompt bytes; generation does not tokenize.
REFERENCE_BINARY_SHA256 = "8d4023c3812a6106c564b18b2ebe6a13bfe0f1240b2b65e1b2f4f2582a4388f6"
REFERENCE_MODEL_SHA256 = "ed5fa30c487b282ec156c29062f1222e5c20875a944ac98289dbd242e947f747"
REFERENCE_COUNTS = {
    "context-speed-64k": {
        "prompt_sha256": "297550143742adface68e344360d84376d1cdafd06fdd0c7cd2a31882d3c81be",
        "reference_prompt_tokens": 52469,
    },
    "context-speed-128k": {
        "prompt_sha256": "ba7ec8230224e7eb1613cad8bee874d147fabb9036048e920e59101775f00bef",
        "reference_prompt_tokens": 106313,
    },
    "context-speed-256k": {
        "prompt_sha256": "5942d49cf60f708c19872006db56f8953ba97aef8491746f411e176e4532c7a2",
        "reference_prompt_tokens": 213940,
    },
}


def encode(value):
    return (json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False) + "\n").encode()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def build():
    bundle = {
        "schema_version": 3,
        "bundle_id": "zh-context-speed",
        "version": "2026.10.1",
        "language": "zh-CN",
        "license_note": "本项目原创确定性合成中文材料，按项目 MIT 许可发布；"
        "无外部语料、非真实人类语料；参考正文已离线分词，人工审核待完成。",
        "task_protocol": "performance",
        "review_records": [],
        "case_review_records": [],
        "answer_policy": {
            "instruction_newlines": "crlf_to_lf",
            "strip_line_edges": True,
            "ignore_empty_lines": True,
            "unicode_normalization": "none",
            "reasoning": "separate_channel_excluded_content_unchanged",
        },
        "cases": [
            {
                "case_id": f"context-speed-{label}",
                "category": "performance",
                "prompt": prompt(label, count),
                "reference_answer": "",
                "rules": {"output_target_tokens": 256},
            }
            for label, _, _, count in SPECS
        ],
    }
    validate_bundle(bundle)
    return bundle


def outputs():
    bundle = build()
    raw = encode(bundle)
    cases = []
    for index, (case, spec) in enumerate(zip(bundle["cases"], SPECS, strict=True)):
        label, context, target, count = spec
        measurement = REFERENCE_COUNTS[case["case_id"]]
        if sha(case["prompt"].encode()) != measurement["prompt_sha256"]:
            raise ValueError("reference_token_count_prompt_hash_mismatch")
        cases.append(
            {
                "case_id": case["case_id"],
                "context_tokens": context,
                "reference_workload_target_tokens": target,
                "record_count": count,
                "full_text_location": f"{BUNDLE_NAME}#/cases/{index}/prompt",
                "prompt_characters": len(case["prompt"]),
                "prompt_utf8_bytes": len(case["prompt"].encode()),
                "prompt_sha256": sha(case["prompt"].encode()),
                "case_content_sha256": case_content_hash(bundle, case),
                "reference_tokenizer": "Prism llama-tokenize",
                "reference_model": "SmolLM2-135M-Instruct-Q4_K_M.gguf",
                "reference_tokenizer_revision": "prism-b10743-adfffbe",
                "reference_tokenizer_sha256": REFERENCE_BINARY_SHA256,
                "reference_model_sha256": REFERENCE_MODEL_SHA256,
                "reference_prompt_tokens": measurement["reference_prompt_tokens"],
                "reference_count_scope": "raw_prompt_no_chat_template_no_bos",
                "reference_count_flags": [
                    "--offline",
                    "--ids",
                    "--stdin",
                    "--no-bos",
                    "--no-parse-special",
                    "--no-escape",
                ],
                "reference_model_native_context_tokens": 8192,
                "reference_count_status": "frozen_offline_observation_not_recounted_by_generator",
                "actual_input_tokens": None,
                "token_count_missing_reason": "actual_input_tokens 尚缺；"
                "须由被测模型应用模板后逐请求计数。",
                "human_review": "pending",
            }
        )
    review = {
        "definition": "context-speed-corpus-review.v1",
        "human_review": "pending",
        "bundle_id": bundle["bundle_id"],
        "version": bundle["version"],
        "bundle_file_sha256": sha(raw),
        "bundle_content_sha256": content_hash(bundle),
        "generator_version": GENERATOR_VERSION,
        "generation_seed": SEED,
        "generator_sources_sha256": {
            path.name: sha(path.read_bytes())
            for path in (Path(__file__), Path(__file__).with_name("context_speed_corpus.py"))
        },
        "license": "MIT",
        "license_source": "../LICENSE",
        "material_origin": "本项目原创确定性合成；无下载、无外部语料；地点与事件均虚构。",
        "generation_rule": "按固定档案数71/144/290生成；12主题轮换，每12条换一种结构，"
        "共6结构；SHA256(seed|index|field)前8字节的大端整数取模选数据；"
        "LF换行、UTF-8、无时间戳、无运行模型输入。",
        "topics": [topic[0] for topic in TOPICS],
        "structures": ["过程叙述", "批次表", "访谈摘记", "JSON摘录", "工作步骤", "方案对照"],
        "limitations": [
            "模板与边界段落重复，非真实人类语料，不代表所有长上下文任务。",
            "三档共享前缀档案，不能用它们证明跨档独立性或缓存性能。",
            "档案关联仅为检索线索；合成数值不构成实测或事实。",
            "参考计数只覆盖正文、无chat模板和BOS；8K参考模型不证明专项档位能力或实际预算。",
        ],
        "execution_recipe_source": "../docs/plans/context-speed.md#专项题库与运行协议",
        "execution_recipe_status": "planning_reference_only_not_executed_or_enforced_by_bundle",
        "cases": cases,
    }
    lines = [
        "# 长上下文速度题库草案 · pending",
        "",
        "人工审核未完成。用户认可的是 UI 原型，不是本题库；两类审核记录均为空。",
        "这是已做参考正文离线分词的固定草案，不能称为可正式测评的已审题库。",
        "",
        "## 全文与身份",
        "",
        f"三篇全文保存在 [{BUNDLE_NAME}]({BUNDLE_NAME}) 的各 `cases[i].prompt`。",
        f"字符数、UTF-8 字节数、全文与逐题 hash 见 [{REVIEW_NAME}]({REVIEW_NAME})。",
        f"版本 `2026.10.1`；bundle content SHA256：`{content_hash(bundle)}`。",
        "",
        "| case | 全文 JSON Pointer | 字符数 | UTF-8 字节数 |",
        "| --- | --- | ---: | ---: |",
    ]
    for index, case in enumerate(cases):
        lines.append(
            f"| {case['case_id']} | `/cases/{index}/prompt` | "
            f"{case['prompt_characters']} | {case['prompt_utf8_bytes']} |"
        )
    lines += [
        "",
        "## 生成与许可",
        "",
        "本项目原创合成材料，采用 [MIT 许可](../LICENSE)，无外部摘录或下载。",
        f"生成器版本 `{GENERATOR_VERSION}`，固定种子 `{SEED}`。",
        review["generation_rule"],
        "主题：" + "、".join(review["topics"]) + "。",
        "结构：" + "、".join(review["structures"]) + "。",
        "生成器源文件摘要在绑定 JSON 中；按固定顺序生成，不能按运行模型调整文本。",
        "",
        *review["limitations"],
        "",
        "## 规则与五次运行配方",
        "",
        "三个 case 均为 performance，参考答案为空，唯一规则为 output_target_tokens=256。",
        "不设质量分、正确率或参考答案评判。请人工检查三篇全文、规则、许可和合成局限。",
        "五次运行配方以[专项规划](../docs/plans/context-speed.md#专项题库与运行协议)为来源：",
        "一个选中 case，5 个完整 trial；并发/槽位 1，每 trial 一次短文本预热；",
        "strict_fixed_length=256、空 stop、关闭 prompt cache 与投机解码；",
        "temperature=0、top_p=1、top_k=1、min_p=0、presence_penalty=0、",
        "repeat_penalty=1、seed=0、reasoning_mode=off。seed_support 必须实证核验。",
        "该配方由后续 profile/plan 实现并冻结；本题包不执行或保证五次运行。",
        "",
        "## 待核验与准入",
        "",
        "参考负载目标 52,625 / 106,294 / 213,612 token；正文实测 52,469 / 106,313 / 213,940。",
        "参考为 Prism b10743-adfffbe llama-tokenize + SmolLM2-135M-Instruct-Q4_K_M.gguf，",
        "工具与模型 SHA256、计数选项均在绑定 JSON 中；--offline，逐个进程最多60秒。",
        "只对完整 prompt 字符串计数，不套 chat 模板、不加 BOS、不解析特殊标记、不处理转义。",
        "参考模型原生 context 仅8192；分词成功不证明支持64K/128K/256K或能进行推理。",
        "actual_input_tokens 仍为 null，实际模板后计数和预算须由被测服务逐请求核验。",
        "生成器只复制有正文 hash 绑定的冻结观测，不调用分词工具；正文改变会拒绝旧计数。",
        "如需调整，生成新版本和新审核材料，保留当前原件；运行时只核验预算，禁止截断或换题。",
        "实际输入 + 256 必须不超过对应 context（65,536 / 131,072 / 262,144）。",
        "人工批准必须绑定当前内容 hash；自动校验与 Agent 自审不能生成批准。",
        "require_review 应拒绝当前草案；不注册社区资源，不绕过正式准入。",
        "",
    ]
    return {BUNDLE_NAME: raw, REVIEW_NAME: encode(review), MARKDOWN_NAME: "\n".join(lines).encode()}


def write_outputs(out_dir):
    # Reserve every name before writing; pre-existing files/symlinks are never replaced.
    data = outputs()
    out_dir.mkdir(parents=True, exist_ok=True)
    if any((out_dir / name).exists() or (out_dir / name).is_symlink() for name in data):
        raise FileExistsError("output_exists: use a new output directory; originals are preserved")
    handles = []
    try:
        for name in data:
            handles.append(((out_dir / name).open("xb"), data[name]))
        for handle, raw in handles:
            handle.write(raw)
    finally:
        for handle, _ in handles:
            handle.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--out-dir",
        type=Path,
        required=True,
        help="output directory; refuses if any of the three outputs already exists",
    )
    args = parser.parse_args()
    try:
        write_outputs(args.out_dir)
    except FileExistsError as exc:
        parser.exit(2, f"{exc}\n")
    print(json.dumps({"human_review": "pending", "cases": 3, "model_requests": 0}))


if __name__ == "__main__":
    main()
