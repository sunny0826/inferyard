"""Prepare two field-meaning clarifications; no approval or model requests."""

import argparse
import copy
import json
from pathlib import Path

from inferyard.analysis.scoring import score_case
from inferyard.config.bundle import content_hash, require_review, validate_bundle
from inferyard.evidence.storage import json_bytes, read_json, sha256_file

if __package__:
    from .build_structured_clarity_bundle import canonical_hash, write_outputs
else:
    from build_structured_clarity_bundle import canonical_hash, write_outputs

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "bundles/zh-core-clarified.json"
OUTPUT = ROOT / "bundles/zh-core-fields.json"
SCOPE = ROOT / "bundles/zh-core-fields.review.json"
REVIEW = ROOT / "bundles/STRUCTURED-FIELDS-REVIEW.md"
FACTS = {
    "structured-13": "错误记录：代码为字符串E2；错误信息为字符串“超时”；该错误可以重试。",
    "structured-18": "原始字段名a/b的值是字符串“启用”；原始字段名x~y的值是布尔值false。",
}


def build(source):
    require_review(source)
    if source["version"] != "2.0.1-draft.1" or len(source["cases"]) != 120:
        raise ValueError("expected_reviewed_clarified_core_120")
    bundle = copy.deepcopy(source)
    bundle.pop("review_provenance", None)
    bundle.update(version="2.0.2-draft.1", review_records=[])
    changes = []
    for case in bundle["cases"]:
        if case["case_id"] not in FACTS:
            continue
        old = copy.deepcopy(case)
        before, separator, instruction = old["prompt"].partition("输出")
        if not separator or not before:
            raise ValueError("missing_frozen_output_instruction")
        case["prompt"] = FACTS[case["case_id"]] + separator + instruction
        changes.append(
            {
                "case_id": case["case_id"],
                "before_prompt": old["prompt"],
                "after_prompt": case["prompt"],
                "case_before_sha256": canonical_hash(old),
                "case_after_sha256": canonical_hash(case),
                "rules_sha256": canonical_hash(case["rules"]),
                "reference_sha256": canonical_hash(case["reference_answer"]),
            }
        )
    if {c["case_id"] for c in changes} != set(FACTS):
        raise ValueError("missing_expected_field_cases")
    validate_bundle(bundle)
    for case in bundle["cases"]:
        if (
            score_case(case, case["reference_answer"], bundle["answer_policy"])["quality_state"]
            != "pass"
        ):
            raise ValueError("reference_does_not_match_unchanged_rules")
    return bundle, changes


def outputs():
    source = read_json(SOURCE)
    bundle, changes = build(source)
    scope = {
        "human_review": "pending",
        "source_file_sha256": sha256_file(SOURCE),
        "source_content_sha256": content_hash(source),
        "candidate_content_sha256": content_hash(bundle),
        "changed_case_count": 2,
        "unchanged_case_count": 118,
        "unchanged_cases_sha256": canonical_hash(
            [case for case in source["cases"] if case["case_id"] not in FACTS]
        ),
        "reference_answers_and_rules": "unchanged_for_all_120_cases",
        "changes": changes,
    }
    text = [
        "# 两道 JSON 题字段含义修订 · 待人工审核",
        "",
        "只审核两处：错误信息仅为“超时”，重试许可另映射为 retryable；",
        "`x~y` 的 false 明确为布尔值，`a/b` 的“启用”明确为字符串。",
        "全部答案、评分规则、外层字段要求及其余 118 题不变。",
        "当前 18/20 结果保留；新候选不替换默认题包、不重评分、不自动取得批准。",
        "",
        f"审核内容 SHA-256：`{content_hash(bundle)}`。",
        "新候选：`zh-core-fields.json`，内容版本 `2.0.2-draft.1`，契约 v3。",
        "本次修订基于已看到的两处失分，因此后续结果仅是新协议的单次观察。",
        "",
    ]
    cases = {case["case_id"]: case for case in bundle["cases"]}
    for change in changes:
        case = cases[change["case_id"]]
        text.extend(
            [
                f"## {case['case_id']}",
                "",
                "修改前：",
                "",
                "```text",
                change["before_prompt"],
                "```",
                "",
                "修改后：",
                "",
                "```text",
                change["after_prompt"],
                "```",
                "",
                "参考答案和评分规则不变：",
                "",
                "```json",
                case["reference_answer"],
                "```",
                "",
            ]
        )
    return {OUTPUT: json_bytes(bundle), SCOPE: json_bytes(scope), REVIEW: "\n".join(text).encode()}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    write_outputs(outputs(), check=args.check, bundle_path=OUTPUT)
    print(json.dumps({"checked": args.check, "modified_cases": 2, "model_requests": 0}))


if __name__ == "__main__":
    main()
