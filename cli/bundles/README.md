# Task Bundles

Shipped bundles use `schema_version = 3` and embed human-review records verified by `inferyard`:

| Bundle | Contents |
| --- | --- |
| [zh-core.json](zh-core.json) | 120 Chinese tasks across 6 categories (20 each): instruction following, extraction, grounded Q&A, math/logic, classification, and structured output |
| [zh-svg-pelican.json](zh-svg-pelican.json) | 1 SVG drawing prompt; retains raw output and timing without a quality score |
| [zh-smoke.json](zh-smoke.json) | 40 tasks (instruction following and extraction) for quick verification |
| [zh-context-speed.json](zh-context-speed.json) | 3 synthetic Chinese prompts for 64K / 128K / 256K context speed (`output_target_tokens = 256`, no quality score) |

Revised structured variants ([zh-core-clarified.json](zh-core-clarified.json), [zh-core-fields.json](zh-core-fields.json)) remain optional and do not replace default `zh-core.json`. Editing any case, answer, rule, or shared policy invalidates existing review digests and requires fresh human review.
