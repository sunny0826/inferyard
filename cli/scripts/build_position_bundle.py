"""Deterministic shared-text position corpus; character sizes are not token counts."""

import argparse
import hashlib
from pathlib import Path

from inferyard.analysis.position import register_position_families
from inferyard.config.bundle import content_hash
from inferyard.contracts.validation import validate_document
from inferyard.evidence.storage import atomic_bytes, json_bytes

FAMILIES = (
    {
        "family_id": "archive-retrieval",
        "task_kind": "retrieval",
        "evidence": ["档案记录：青禾站的校验口令是松雨。"],
        "question": "青禾站的校验口令是什么？只输出口令，不加标点。",
        "answer": "松雨",
    },
    {
        "family_id": "archive-link",
        "task_kind": "dual_evidence",
        "evidence": ["档案甲：编号澄风对应柜位戊。", "档案乙：柜位戊的口令为竹星。"],
        "question": "编号澄风所对应柜位的口令是什么？只输出口令，不加标点。",
        "answer": "竹星",
    },
)
POSITIONS = {"front": (0.10, 0.20), "middle": (0.45, 0.55), "back": (0.80, 0.90)}


def digest(text):
    return hashlib.sha256(text.encode()).hexdigest()


def background(length):
    """Fill an exact character gap with whole neutral lines, never partial sentences."""
    line = "背景记录：石桥旁有树。\n"
    count, remainder = divmod(length, len(line))
    return line * count + ("　" * (remainder - 1) + "\n" if remainder else "")


def build(sizes=(256, 1024, 3072)):
    if (
        len(set(sizes)) != len(sizes)
        or not sizes
        or any(type(n) is not int or not 256 <= n <= 16384 for n in sizes)
    ):
        raise ValueError("body character sizes must be distinct integers in 256..16384")
    cases, records, cohorts = [], [], []
    prefix = "请仅根据以下材料回答末尾问题。结合与问题相关的档案记录作答。\n材料开始：\n"
    for size in sizes:
        for position, fractions in POSITIONS.items():
            for family in FAMILIES:
                cid = f"{family['family_id']}-{size}-{position}"
                # Fixed background and fixed total character length within each size.
                body = ""
                spans = []
                for text, fraction in zip(family["evidence"], fractions, strict=False):
                    start = int(size * fraction) - len(text) // 2
                    end = start + len(text)
                    body += background(start - len(body)) + text + "\n"
                    spans.append(
                        {
                            "start": len(prefix) + start,
                            "end": len(prefix) + end,
                            "sha256": digest(text),
                        }
                    )
                body += background(size - len(body))
                suffix = "\n材料结束。\n问题：" + family["question"]
                prompt = prefix + body + suffix
                cases.append(
                    {
                        "case_id": cid,
                        "category": "qa",
                        "prompt": prompt,
                        "reference_answer": family["answer"],
                        "rules": {"answers": [family["answer"]], "normalization": "strip"},
                    }
                )
                records.append(
                    {
                        "case_id": cid,
                        "family_id": family["family_id"],
                        "task_kind": family["task_kind"],
                        "prompt_sha256": digest(prompt),
                        "body_start": len(prefix),
                        "body_end": len(prefix) + size,
                        "evidence_spans": spans,
                    }
                )
                cohorts.append({"case_id": cid, "body_characters": size, "position": position})
    bundle = {
        "schema_version": 3,
        "bundle_id": "zh-position-v2",
        "version": "2.0.0-draft.2",
        "language": "zh-CN",
        "license_note": "Project-authored synthetic archive tasks",
        "review_records": [],
        "task_protocol": "quality",
        "answer_policy": {
            "instruction_newlines": "crlf_to_lf",
            "strip_line_edges": True,
            "ignore_empty_lines": False,
            "unicode_normalization": "none",
            "reasoning": "separate_channel_excluded_content_unchanged",
        },
        "cases": cases,
    }
    validate_document("bundle", bundle)
    families = {}
    for cohort, record in zip(cohorts, records, strict=True):
        register_position_families(
            {
                "purpose": "position",
                "protocol": {"case_ids": [cohort["case_id"]]},
                "position_cases": [record],
            },
            bundle,
            families,
        )
    protocol = {
        "position_cases": records,
        "cohorts": cohorts,
        "body_length_unit": "unicode_characters_not_tokens",
        "independent_families": len(families),
        "variant_count": len(cases),
        "cross_model_policy": "identical_text_measure_template_tokens_per_model",
        "limitations": [
            "synthetic_retrieval_not_general_long_context_comprehension",
            "runtime_context_admission_required",
            "human_review_pending",
        ],
    }
    return bundle, protocol


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    bundle, protocol = build()
    args.out.mkdir(parents=True, exist_ok=False)
    for name, value in (("bundle.json", bundle), ("position-protocol.json", protocol)):
        atomic_bytes(args.out / name, json_bytes(value))
    atomic_bytes(
        args.out / "review.json",
        json_bytes(
            {
                "status": "pending_human_corpus_review",
                "content_sha256": content_hash(bundle),
                "variant_count": 18,
                "independent_families": 2,
            }
        ),
    )


if __name__ == "__main__":
    main()
