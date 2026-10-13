"""Deterministic, script-free, offline HTML for diagnostic engine observations."""

import json
from html import escape

from inferyard.reporting.engine_fit_metrics import observations

LIMITATIONS = [
    "diagnostic_only_no_performance_qualification",
    "client_timing_not_engine_compute_time",
    "endpoint_reported_tokens_not_independently_measured",
    "process_tree_rss_may_double_count_shared_pages_not_gpu_memory",
    "model_binding_does_not_prove_gpu_weight_residency",
    "effective_template_parameters_and_dependencies_not_fully_verified",
]

_CSS = """
:root{color-scheme:light;--ink:#152a3b;--muted:#536576;--line:#d9e2ea;--paper:#fff}
*{box-sizing:border-box}body{margin:0;background:#f0f4f7;color:var(--ink);
font:16px/1.65 system-ui,-apple-system,'Segoe UI',sans-serif}
main{max-width:1080px;margin:auto;padding:32px 24px 64px}header{margin-bottom:24px}
h1{font-size:clamp(1.7rem,4vw,2.4rem);line-height:1.25}h2{font-size:1.4rem}h3{font-size:1.1rem}
p{margin:.6em 0}section,.notice{background:var(--paper);border:1px solid var(--line);
border-radius:12px;padding:20px;margin:18px 0}.notice{border-left:5px solid #b66a00}
.badge{display:inline-block;padding:3px 10px;border-radius:20px;background:#fff0cc}
.muted{color:var(--muted)}.table-wrap{overflow-x:auto}table{width:100%;border-collapse:collapse}
th,td{text-align:left;padding:10px;vertical-align:top;border-bottom:1px solid var(--line)}
th{background:#edf3f8;font-weight:600}code,pre{font-family:ui-monospace,monospace}
pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#f5f7f9;padding:14px;border-radius:8px}
details{border-top:1px solid var(--line);padding:12px 0}summary{cursor:pointer;font-weight:600}
.facts{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:12px}
.fact{padding:12px;background:#f5f7f9;border-radius:8px}.fact strong{display:block;font-size:1.2rem}
li,td,p,code,summary{overflow-wrap:anywhere}.missing{color:#825400}
@media(max-width:600px){main{padding:18px 12px 32px}section,.notice{padding:14px}
th,td{padding:8px;font-size:.9rem}.facts{grid-template-columns:1fr 1fr}}
@media print{body{background:white}main{max-width:none;padding:0}section{break-inside:avoid}
details{break-inside:avoid}.table-wrap{overflow:visible}}
"""


def _e(value):
    return escape(str(value), quote=True)


def _json(value):
    return _e(json.dumps(value, ensure_ascii=False, allow_nan=False, sort_keys=True, indent=2))


def _value(value, reason="not_observed", suffix=""):
    if value is None:
        return '<span class="missing">缺测 · ' + _e(reason) + "</span>"
    return _e(value) + _e(suffix)


def _version(run):
    service = run["service"]
    if (
        run["definition"]
        in ("engine_fit_run.v3", "engine_fit_run.v4", "engine_fit_run.v5", "engine_fit_run.v6")
        and service["version"] is None
    ):
        return (
            '<span style="overflow-wrap:anywhere">'
            + _value(None, service["version_missing_reason"])
            + "</span>"
        )
    return _e(service["version"])


def _request_rows(plan, rows):
    prompts = {case["id"]: case["prompt"] for case in plan["cases"]}
    parts = []
    for row in rows:
        response = row["response"]
        description = " · ".join((row["request_id"], row["case_id"], row["status"]))
        part = "<details><summary>" + _e(description) + "</summary>"
        part += "<h3>请求原文</h3><pre>" + _e(prompts[row["case_id"]]) + "</pre>"
        if response is None:
            part += "<p>响应：" + _value(None, row["reason"]) + "</p>"
        else:
            part += "<h3>模型原始文本</h3><pre>" + _e(response["text"]) + "</pre>"
            part += '<div class="table-wrap"><table><thead><tr>'
            headers = ("客户端总耗时", "客户端首内容耗时", "端点输入 token", "端点输出 token")
            part += "".join("<th>" + label + "</th>" for label in headers)
            part += "</tr></thead><tbody><tr>"
            values = [
                _value(response["elapsed_ms"], suffix=" ms"),
                _value(response["first_content_ms"], "no_content", " ms"),
                _value(response["prompt_tokens"], response["usage_missing_reason"]),
                _value(response["completion_tokens"], response["usage_missing_reason"]),
            ]
            part += "".join("<td>" + value + "</td>" for value in values)
            part += "</tr></tbody></table></div><p>终止原因："
            part += _e(response["finish_reason"]) + "</p>"
        parts.append(part + "</details>")
    return "".join(parts)


def _run(data):
    plan, run, rows = data["plan"], data["run"], data["requests"]
    html = "<section><h2>" + _e(run["engine"]) + " · " + _version(run) + "</h2>"
    html += "<p>运行 <code>" + _e(run["run_id"]) + "</code> · " + _e(run["completeness"]) + "</p>"
    if run["stop_reason"] is not None:
        html += "<p>停止原因：" + _e(run["stop_reason"]) + "</p>"
    html += '<div class="facts">'
    for key, label in (
        ("planned", "计划请求"),
        ("completed", "完成"),
        ("failed", "失败"),
        ("cancelled", "取消"),
        ("invalid", "工具错误"),
        ("not_executed", "未执行"),
    ):
        html += (
            '<div class="fact">' + label + "<strong>" + _e(run["counts"][key]) + "</strong></div>"
        )
    html += "</div><h3>资源观察</h3>"
    html += "<p>原生进程树在请求边界采样。RSS 相加可能重复计算共享页；"
    html += "它不等于物理内存或显存，不证明 GPU 权重驻留，也不测量整机能耗。</p>"
    if run["resources"]:
        html += "<details><summary>逐次资源快照 · bytes / CPU seconds</summary><pre>"
        html += _json(run["resources"]) + "</pre></details>"
    else:
        html += "<p>" + _value(None, "no_resource_snapshots") + "</p>"
    html += "<details><summary>服务绑定与观察范围</summary><pre>"
    html += _json({"binding": run["binding"], "service": run["service"]}) + "</pre></details>"
    html += "<details><summary>本次运行限制</summary><pre>" + _json(run["limitations"])
    html += "</pre></details><h3>逐题请求与回答</h3>"
    return html + _request_rows(plan, rows) + "</section>"


def render(runs, *, comparison=False):
    """Render only escaped text; no JavaScript, remote fonts, images or stylesheets."""
    title = "同机同模型引擎适配对照" if comparison else "引擎适配诊断"
    plan = runs[0]["plan"]
    html = '<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">'
    html += '<meta name="viewport" content="width=device-width,initial-scale=1">'
    html += "<title>" + title + "</title><style>" + _CSS + "</style></head><body><main>"
    html += '<header><span class="badge">诊断观察 · 性能比较资格：false</span><h1>'
    html += title + "</h1><p>同一冻结计划、模型内容与机器身份下的执行记录。</p></header>"
    if plan["definition"] == "engine_fit_plan.v3":
        html += '<aside class="notice"><strong>本次已显式跳过温度停止</strong><p>冻结原因：'
        html += _e(plan["parameters"]["temperature_stop_override_reason"])
        html += "</p><p>温度仍采集；内存、磁盘、身份、请求预算和排空限制继续适用。</p></aside>"
    elif plan["definition"] == "engine_fit_plan.v4":
        html += '<aside class="notice"><strong>本次已显式跳过内存停止</strong><p>冻结原因：'
        html += _e(plan["parameters"]["memory_stop_override_reason"])
        html += "</p><p>内存仍采集，缺测保留原因；磁盘、身份、请求预算和排空限制继续适用。</p>"
        reason = plan["parameters"]["temperature_stop_override_reason"]
        if reason is not None:
            html += "<strong>本次已显式跳过温度停止</strong><p>冻结原因："
            html += _e(reason) + "</p><p>温度仍采集。</p>"
        else:
            html += "<p>温度停止上限仍为 85°C。</p>"
        html += "</aside>"
    html += (
        '<aside class="notice"><strong>适用范围</strong><p>耗时是客户端观察；token 用量为端点自报。'
    )
    html += "实际模板、有效参数及完整依赖未全面独立核验。本报告不发布赢家、加速比或适配总分。"
    html += "completed 表示请求协议完成，不代表答案正确或模型质量通过。</p></aside>"
    html += "<section><h2>冻结输入</h2><p>计划 <code>" + _e(plan["plan_id"]) + "</code></p>"
    model_label = "模型文件" if plan["model"].get("kind") == "gguf" else "模型目录"
    html += "<p>" + model_label + "：<code>" + _e(plan["model"]["path"]) + "</code></p>"
    html += "<p>模型 SHA256：<code>" + _e(plan["model"]["sha256"]) + "</code></p>"
    html += "<p>主机 SHA256：<code>" + _e(plan["host"]["sha256"]) + "</code></p>"
    html += "<details><summary>主机描述与冻结参数</summary><pre>"
    html += (
        _json({"host": plan["host"], "parameters": plan["parameters"]})
        + "</pre></details></section>"
    )
    if comparison:
        html += '<section><h2>执行概况</h2><div class="table-wrap"><table><thead><tr>'
        html += "<th>引擎</th><th>版本</th><th>完成 / 计划</th><th>状态</th>"
        html += "<th>客户端总耗时中位数 (ms)</th><th>首内容耗时中位数 (ms)</th>"
        html += "<th>完成请求端点输出 token 总和</th><th>边界观察 RSS 最大值 (MiB，非峰值)</th>"
        html += "<th>边界最低可用系统内存 (MiB)</th></tr></thead><tbody>"
        for data in runs:
            run = data["run"]
            cells = (
                _e(run["engine"]),
                _version(run),
                _e(f"{run['counts']['completed']} / {run['counts']['planned']}"),
                _e(run["completeness"]),
            )
            html += "<tr>" + "".join("<td>" + v + "</td>" for v in cells)
            for key, metric in observations(data).items():
                value = metric["value"]
                if value is not None and key.endswith("_bytes"):
                    value = round(value / 2**20, 2)
                html += "<td>" + _value(value, metric["reason"])
                html += '<small class="muted"> · 观察 ' + str(metric["sample_count"])
                html += "/" + str(metric["expected_count"]) + "</small></td>"
            html += "</tr>"
        html += '</tbody></table></div><p class="muted">耗时仅统计协议完成的请求；'
        html += "首内容中位数仅统计实际返回文本的请求。任一完成请求缺少输出 token，"
        html += "则总和缺测。资源来自请求边界采样，不代表请求内峰值。</p></section>"
    html += "".join(_run(data) for data in runs)
    return (
        html + '<footer class="muted">自包含离线报告 · engine-fit v1</footer></main></body></html>'
    )
