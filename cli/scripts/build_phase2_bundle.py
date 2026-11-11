"""Build the original synthetic corpus and its unapproved review material."""

import argparse
import copy
import hashlib
import html
import json
from collections import Counter
from pathlib import Path

from inferyard.analysis.scoring import score_case
from inferyard.contracts.bundle import content_hash
from inferyard.contracts.validation import validate_document

ROOT = Path(__file__).resolve().parents[1]

if __package__:
    from .phase2_corpus import CLASSIFICATION, LABELS, MATH, QA, STRUCTURED
else:
    from phase2_corpus import CLASSIFICATION, LABELS, MATH, QA, STRUCTURED


def dumps(value):
    return json.dumps(value, ensure_ascii=False, indent=2) + "\n"


def shape(value):
    """Schema checks shape and types; scored fields check exact semantics separately."""
    if isinstance(value, dict):
        return dict(
            type="object",
            properties={k: shape(v) for k, v in value.items()},
            required=list(value),
            additionalProperties=False,
        )
    if isinstance(value, list):
        if not value:
            return dict(type="array", items=False, maxItems=0)
        return dict(
            type="array",
            prefixItems=[shape(v) for v in value],
            items=False,
            minItems=len(value),
            maxItems=len(value),
        )
    return {
        "type": {
            str: "string",
            int: "integer",
            float: "number",
            bool: "boolean",
            type(None): "null",
        }[type(value)]
    }


def fields(value, path=""):
    if isinstance(value, dict) and value:
        return [
            p
            for k, v in value.items()
            for p in fields(v, path + "/" + k.replace("~", "~0").replace("/", "~1"))
        ]
    if isinstance(value, list) and value:
        return [p for i, v in enumerate(value) for p in fields(v, path + "/" + str(i))]
    return [path]


def new_cases():
    result = []

    def add(category, prompt, answer, rules):
        index = sum(c["category"] == category for c in result) + 1
        result.append(
            dict(
                case_id=f"{category}-{index:02}",
                category=category,
                prompt=prompt,
                reference_answer=answer,
                rules=rules,
            )
        )

    for prompt, answer in QA:
        add(
            "qa",
            "仅根据材料回答，只输出答案，不加解释。\n" + prompt,
            answer,
            dict(answers=[answer], normalization="strip"),
        )
    for prompt, expected, unit, tolerance in MATH:
        instruction = (
            f"只输出数值及单位“{unit}”，不加解释。" if unit else "只输出数值，不加单位和解释。"
        )
        add(
            "math",
            prompt + "\n" + instruction,
            str(expected) + unit,
            dict(expected=expected, unit=unit, absolute_tolerance=tolerance, relative_tolerance=0),
        )
    # Interleave labels to avoid a contiguous category-answer pattern.
    for index in range(5):
        for label in LABELS:
            add(
                "classification",
                "将以下客服消息归类，只输出一个标签。\n"
                "物流：运输、派送、收货问题；账务：扣款、退款、发票问题；"
                "技术：软件故障；其他：不属于前三类的消息。\n消息：" + CLASSIFICATION[label][index],
                label,
                dict(labels=LABELS, expected=label),
            )
    for prompt, expected in STRUCTURED:
        schema = shape(expected)
        if "task" in expected:
            schema["properties"]["task"]["properties"]["state"]["enum"] = ["todo", "doing", "done"]
        add(
            "structured",
            prompt + "\n只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。",
            json.dumps(expected, ensure_ascii=False),
            dict(
                json_schema=schema,
                expected=expected,
                fields=fields(expected),
                array_matching="ordered",
            ),
        )
    return result


def boundary_examples(task):
    answer, category = task["reference_answer"], task["category"]
    accepted = " \n" + answer + "\n "
    if category == "qa":
        rejected, reason = answer + "。", "仅去首尾空白；添加标点不视为精确匹配"
    elif category == "math":
        r = task["rules"]
        rejected = str(r["expected"] + max(1, r["absolute_tolerance"] * 10)) + r["unit"]
        reason = "数值超出冻结容差；单位必须完整匹配"
    elif category == "classification":
        rejected, reason = "类别：" + answer, "只接受冻结标签；额外前缀属于无效标签"
    else:
        rejected, reason = (
            "```json\n" + answer + "\n```",
            "Markdown围栏不属于JSON；解析失败时所有字段保留分母",
        )
    return [
        dict(answer=accepted, expected_state="pass", reason="首尾空白允许"),
        dict(answer=rejected, expected_state="fail", reason=reason),
    ]


def build():
    original = json.loads((ROOT / "bundles/zh-smoke.json").read_text())
    bundle = copy.deepcopy(original)
    bundle.pop("review_provenance", None)
    bundle.update(
        schema_version=3,
        bundle_id="zh-core-v2",
        version="2.0.0-draft.1",
        review_records=[],
        task_protocol="quality",
    )
    bundle["cases"].extend(new_cases())
    validate_document("bundle", bundle)
    boundaries = {c["case_id"]: boundary_examples(c) for c in bundle["cases"][40:]}
    for task in bundle["cases"]:
        assert (
            score_case(task, task["reference_answer"], bundle["answer_policy"])["quality_state"]
            == "pass"
        )
        for example in boundaries.get(task["case_id"], []):
            assert (
                score_case(task, example["answer"], bundle["answer_policy"])["quality_state"]
                == example["expected_state"]
            )
    return bundle, boundaries


def render(bundle, boundaries):
    escape = html.escape
    digest = content_hash(bundle)
    sections = []
    names = dict(qa="材料问答", math="数学与逻辑", classification="文本分类", structured="嵌套JSON")
    for task in bundle["cases"][40:]:
        cid = task["case_id"]
        sections.append(
            f'<details data-category="{task["category"]}">'
            f"<summary>{cid} · {names[task['category']]}</summary>"
            f"<h3>题目</h3><pre>{escape(task['prompt'])}</pre>"
            f"<h3>标准答案</h3><pre>{escape(task['reference_answer'])}</pre>"
            f"<h3>边界样例</h3><pre>{escape(dumps(boundaries[cid]))}</pre>"
            f"<details><summary>完整评分规则</summary><pre>{escape(dumps(task['rules']))}</pre></details></details>"
        )
    template = (ROOT / "scripts/phase2_review.html").read_text()
    values = {
        "DIGEST": digest,
        "BOUNDARIES": hashlib.sha256(dumps(boundaries).encode()).hexdigest(),
        "OPTIONS": "".join(f'<option value="{k}">{v} · 20题</option>' for k, v in names.items()),
        "SECTIONS": "".join(sections),
    }
    for key, value in values.items():
        template = template.replace("@@" + key + "@@", value)
    return template


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    bundle, boundaries = build()
    outputs = {
        "bundles/zh-core.json": dumps(bundle),
        "bundles/zh-core-v2.boundaries.json": dumps(boundaries),
        "bundles/zh-core-review.html": render(bundle, boundaries),
    }
    for name, text in outputs.items():
        path = ROOT / name
        if args.check:
            # Review records are human-owned; regeneration cannot remove or invent them.
            if name.endswith("zh-core.json"):
                existing = json.loads(path.read_text())
                existing["review_records"] = []
                assert existing == bundle, name
            else:
                assert path.read_text() == text, name
        else:
            if name.endswith("zh-core.json") and path.exists():
                existing = json.loads(path.read_text())
                if existing.get("review_records"):
                    raise SystemExit(
                        "Refusing to overwrite a reviewed bundle; create a new version."
                    )
            path.write_text(text)
    print(
        dumps(
            dict(
                content_sha256=content_hash(bundle),
                categories=Counter(c["category"] for c in bundle["cases"]),
                new_questions=80,
                human_review="pending",
            )
        )
    )


if __name__ == "__main__":
    main()
