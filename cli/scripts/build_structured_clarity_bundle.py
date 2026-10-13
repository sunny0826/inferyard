"""Create a new, unapproved core corpus with explicit JSON root fields.

Only structured prompts change. Existing reviewed corpora are never overwritten.
--check reads generated material and validates an optional, hash-bound human record.
No model service, model request, or approval is created by this script.
"""

import argparse
import copy
import hashlib
import json
from pathlib import Path

from inferyard.analysis.scoring import score_case
from inferyard.config.bundle import content_hash, require_review, validate_bundle
from inferyard.evidence.storage import atomic_bytes, json_bytes, read_json, sha256_file

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "bundles/zh-core.json"
OUTPUT = ROOT / "bundles/zh-core-clarified.json"
SCOPE = ROOT / "bundles/zh-core-clarified.review.json"
REVIEW = ROOT / "bundles/STRUCTURED-CLARITY-REVIEW.md"
INSTRUCTIONS = [
    "顶层字段恰为name和address。name是字符串；address是对象，含city字符串和zip空值。",
    "顶层字段恰为id和status。id是字符串；status是对象，含paid、shipped两个布尔值。",
    "顶层字段仅为items。items是数组，每项是含name字符串、count整数的对象。",
    "顶层字段恰为readings和unit。readings是数值数组；unit是字符串“度”。",
    "顶层字段仅为meeting。meeting是对象，含title字符串和attendees空数组。",
    "顶层字段仅为user。user是对象，含id字符串、active布尔值、points整数。",
    "顶层字段仅为task。task是对象，含name和state字符串；"
    "state使用枚举：待办todo、进行中doing、完成done。",
    "顶层字段仅为matrix。matrix是按行排列的二维整数数组。",
    "顶层字段仅为members。members是数组，每项是含name字符串和captain布尔值的对象。",
    "顶层字段仅为folder。folder是对象，含name字符串和files字符串数组。",
    "顶层字段仅为point。point是对象，含x、y两个数值。",
    "顶层字段仅为stock。stock是对象，其键恰为“笔”和“本”，值为整数。",
    "顶层字段仅为error。error是对象，含code、message两个字符串和retryable布尔值。",
    "顶层字段仅为course。course是对象，含name字符串、teacher空值、room字符串。",
    "顶层字段仅为meta。meta是对象，含tags字符串数组和archived布尔值false。",
    "顶层字段仅为product。product是对象，含id字符串、price数值、quantity整数。",
    "顶层字段仅为route。route是数组，每项是含stop字符串和从1开始的order整数的对象。",
    "顶层字段仅为fields。fields是对象，保留材料中的两个原始字段名。",
    "顶层字段仅为range。range是对象，含min、max数值和includeMin、includeMax布尔值。",
    "顶层字段仅为answers。answers是数组，每项是含question整数和value的对象；未作答用null。",
]
SUFFIX = (
    "最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。\n"
    "只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。"
)


def canonical_hash(value):
    return hashlib.sha256(json_bytes(value)).hexdigest()


def build(source):
    require_review(source)
    bundle = copy.deepcopy(source)
    bundle.pop("review_provenance", None)
    bundle.update(version="2.0.1-draft.1", review_records=[])
    originals = {c["case_id"]: c for c in source["cases"]}
    structured = [c for c in bundle["cases"] if c["category"] == "structured"]
    if len(structured) != 20 or len(bundle["cases"]) != 120:
        raise ValueError("expected_reviewed_120_cases_and_20_structured")
    changes = []
    for number, (case, instruction) in enumerate(zip(structured, INSTRUCTIONS, strict=True), 1):
        if case["case_id"] != f"structured-{number:02}":
            raise ValueError("structured_case_order_changed")
        old = case["prompt"]
        if "输出" not in old:
            raise ValueError("source_prompt_missing_output_instruction")
        facts = old.split("输出", 1)[0]
        case["prompt"] = facts + "输出一个JSON对象，" + instruction + "\n" + SUFFIX
        changes.append(
            {
                "case_id": case["case_id"],
                "before_prompt": old,
                "after_prompt": case["prompt"],
                "case_before_sha256": canonical_hash(originals[case["case_id"]]),
                "case_after_sha256": canonical_hash(case),
                "rules_sha256": canonical_hash(case["rules"]),
                "reference_sha256": canonical_hash(case["reference_answer"]),
            }
        )
    validate_bundle(bundle)
    for case in bundle["cases"]:
        if (
            score_case(case, case["reference_answer"], bundle["answer_policy"])["quality_state"]
            != "pass"
        ):
            raise ValueError("reference_answer_does_not_match_frozen_rules")
    return bundle, changes


def review_text(bundle, scope, source):
    cases = {c["case_id"]: c for c in bundle["cases"]}
    text = [
        "# 嵌套 JSON 措辞修订 · 待人工审核",
        "",
        "只审核下面 20 道题的措辞变化。其余 100 题、全部参考答案、评分规则及答案政策不变。",
        "统一明确最外层为对象，以及必须保留的顶层字段；没有按模型失分挑选修订题。",
        "",
        "候选为 `zh-core-clarified.json`，内容版本 `2.0.1-draft.1`，数据契约仍为 v3。",
        "旧题包、原批准和已有 120 题结果保留；新题包不会自动取得原来的人工批准。",
        "",
        f"本次审核内容 SHA-256：`{content_hash(bundle)}`。",
        f"原题包文件 SHA-256：`{scope['source_file_sha256']}`。",
        f"未修改 100 题内容 SHA-256：`{scope['unchanged_cases_sha256']}`。",
        "",
        "审核要点：新题意是否与参考答案和规则一致；顶层/内部字段是否明确；是否引入新事实。",
        "标准答案仅用于审核，不会作为提示词或 JSON 强制约束发给模型。",
        "",
    ]
    original = {c["case_id"]: c for c in source["cases"]}
    for change in scope["changes"]:
        cid = change["case_id"]
        case = cases[cid]
        text.extend(
            [
                f"<details><summary>{cid} · 顶层字段 "
                + " / ".join(case["rules"]["json_schema"]["required"])
                + "</summary>",
                "",
                "修改前：",
                "",
                "```text",
                original[cid]["prompt"],
                "```",
                "",
                "修改后：",
                "",
                "```text",
                case["prompt"],
                "```",
                "",
                "参考答案（不变）：",
                "",
                "```json",
                case["reference_answer"],
                "```",
                "",
                "评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。",
                "完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。",
                "",
                "</details>",
                "",
            ]
        )
    text.extend(
        [
            "## 审核状态",
            "",
            "状态为待审核。生成器没有创建批准记录，自动参考答案检查不代替人工审核。",
            "审核通过后才记录当前用户的明确答复及上述内容哈希，再创建新的真实运行。",
            "候选不会替换默认题包，旧分数也不会重算成新成绩。",
            "",
        ]
    )
    return "\n".join(text)


def outputs(source_path=SOURCE):
    source = read_json(source_path)
    bundle, changes = build(source)
    unchanged = [c for c in source["cases"] if c["category"] != "structured"]
    scope = {
        "human_review": "pending",
        "source_file_sha256": sha256_file(source_path),
        "source_content_sha256": content_hash(source),
        "candidate_content_sha256": content_hash(bundle),
        "changed_case_count": len(changes),
        "unchanged_case_count": len(unchanged),
        "unchanged_cases_sha256": canonical_hash(unchanged),
        "reference_answers_and_rules": "unchanged_for_all_120_cases",
        "changes": changes,
    }
    return {
        OUTPUT: json_bytes(bundle),
        SCOPE: json_bytes(scope),
        REVIEW: review_text(bundle, scope, source).encode(),
    }


def write_outputs(values, *, check=False, bundle_path=OUTPUT):
    if not check and any(p.exists() for p in values):
        raise ValueError("refusing_to_overwrite_generated_review_material")
    for path, expected in values.items():
        if check:
            actual = path.read_bytes()
            if path == bundle_path:
                saved = read_json(path)
                if saved["review_records"]:
                    require_review(saved)
                saved["review_records"] = []
                actual = json_bytes(saved)
            if actual != expected:
                raise ValueError(f"generated_review_material_differs:{path.name}")
        else:
            atomic_bytes(path, expected)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    values = outputs()
    write_outputs(values, check=args.check)
    print(json.dumps({"checked": args.check, "modified_cases": 20, "model_requests": 0}))


if __name__ == "__main__":
    main()
