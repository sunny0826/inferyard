# Task Bundles

Shipped benchmark bundles live in the root [benchmarks/](../../benchmarks/README.md) directory (`benchmarks/catalog.json` and `benchmarks/bundles/*.json`) as the single source of truth:

| Bundle | Contents |
| --- | --- |
| [zh-core.json](../../benchmarks/bundles/zh-core.json) | 120 Chinese tasks across 6 categories (20 each): instruction following, extraction, grounded Q&A, math/logic, classification, and structured output |
| [zh-svg-pelican.json](../../benchmarks/bundles/zh-svg-pelican.json) | 1 SVG drawing prompt; retains raw output and timing without a quality score |
| [zh-smoke.json](../../benchmarks/bundles/zh-smoke.json) | 40 tasks (instruction following and extraction) for quick verification |
| [zh-context-speed.json](../../benchmarks/bundles/zh-context-speed.json) | 3 synthetic Chinese prompts for 64K / 128K / 256K context speed (`output_target_tokens = 256`, no quality score) |

This directory retains optional revised structured variants ([zh-core-clarified.json](zh-core-clarified.json), [zh-core-fields.json](zh-core-fields.json)) and historical human-review artifacts. Editing any case, answer, rule, or shared policy invalidates existing review digests and requires fresh human review.
