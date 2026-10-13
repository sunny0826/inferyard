"""Human device summaries and explicit JSON; presentation never enters application requests."""

import json
import unicodedata
from dataclasses import asdict

GIB = 1024**3
REASONS = {
    "no_local_model": "未发现本地 GGUF；可用 --model 指定已有模型",
    "invalid_or_unreadable_gguf": "模型不存在、不可读或 GGUF 元数据无效",
    "chat_template_unavailable": "模型缺少聊天模板",
    "host_memory_unavailable": "可用内存缺测，无法估算模型容量",
    "insufficient_host_memory": "主机可用内存不足",
    "insufficient_output_disk": "输出位置可用磁盘不足 5 GiB",
    "output_disk_unavailable": "输出位置磁盘容量缺测",
    "nvidia_smi_not_found": "未发现 nvidia-smi；NVIDIA GPU 未观测",
    "nvidia_driver_query_failed": "NVIDIA 驱动查询失败",
    "probe_timeout": "系统查询超时",
    "invalid_input": "参数无效；使用 device-check --help 查看用法",
    "io_error": "本地文件读写失败；检查路径、权限及输出目录是否已存在",
    "internal_error": "工具内部错误",
    "evidence_error": "产物完整性或存储错误",
    "cancelled": "检测已取消",
    "preflight_blocked": "预检阻断",
}


def _reason(code):
    return REASONS.get(code, code or "缺测")


def _capacity(value):
    return "缺测" if value is None else f"{value / GIB:.2f} GiB"


def _display(line):
    return "".join(
        char.encode("unicode_escape").decode("ascii")
        if unicodedata.category(char) in ("Cc", "Cf", "Cs")
        else char
        for char in line
    )


def _human(result):
    status = {
        "inspected": "完成",
        "recommended": "完成",
        "blocked": "阻断",
        "error": "错误",
        "interrupted": "已取消",
    }.get(result.status, result.status)
    lines = [f"设备检测：{status}"]
    report = result.details or {}
    hardware = report.get("hardware")
    if not hardware:
        lines.extend(_reason(reason) for reason in result.limitations)
        return "\n".join(_display(line) for line in lines)
    system = {"Darwin": "macOS"}.get(hardware.get("platform"), hardware.get("platform", "未知"))
    lines.append(f"系统：{system} / {hardware.get('architecture', '未知')}")
    lines.append(
        f"CPU：{hardware.get('cpu_model') or '缺测'}；"
        f"逻辑核 {hardware.get('logical_cpus') or '缺测'}"
    )
    estimate = "（可用量为估算）" if hardware.get("memory_available_is_estimate") else ""
    lines.append(
        f"内存：总量 {_capacity(hardware.get('memory_total_bytes'))}，"
        f"可用 {_capacity(hardware.get('memory_available_bytes'))}{estimate}"
    )
    if hardware.get("memory_available_bytes") is None:
        lines.append(
            "  原因：" + _reason(hardware.get("missing", {}).get("memory_available_bytes"))
        )
    lines.append(
        f"磁盘：可用 {_capacity(hardware.get('disk_free_bytes'))}；"
        f"{hardware.get('disk_path', '当前目录')}"
    )
    gpu = hardware.get("gpu", {})
    for device in gpu.get("devices", []):
        if device.get("memory_kind") == "shared_host":
            memory = "共享主机内存"
        else:
            memory = (
                f"显存 {_capacity(device.get('memory_total_bytes'))}，"
                f"可用 {_capacity(device.get('memory_free_bytes'))}"
            )
        metal = "；Metal 已观察（后端未验证）" if device.get("metal_supported") is True else ""
        lines.append(f"GPU：{device.get('name', '未知')}；{memory}{metal}")
    if not gpu.get("devices"):
        lines.append("GPU：" + _reason(gpu.get("reason")))
    recommendation = report.get("recommendation", {})
    selected = recommendation.get("selected")
    lines.append(f"本地模型：发现 {len(report.get('models', []))} 个")
    if selected:
        mode = "CUDA" if selected["mode"] == "cuda" else "CPU"
        lines.append(
            f"容量建议：{mode}；{selected['model_name']}；"
            f"context {selected['context_size']}；threads {selected['threads']}"
        )
        lines.append(f"模型：{selected['model_path']}")
        if report.get("platform_capabilities", {}).get("live_single_run"):
            lines.append("下一步：准备配置、绑定外部服务，再执行 probe；容量建议需实时预检确认")
    else:
        lines.append("模型建议：" + _reason(recommendation.get("reason")))
    capabilities = report.get("platform_capabilities", {})
    if not capabilities.get("live_single_run", False):
        lines.append(f"平台限制：{system} 实时测评未支持；可使用离线报告流程")
    elif not capabilities.get("live_experiments", False):
        lines.append("平台限制：批量实时实验未支持")
    if result.evidence_dir:
        lines.append(f"保存：{result.evidence_dir}/device-preflight.json")
    lines.append("本次未发送模型请求；检测结果不表示服务已就绪。")
    return "\n".join(_display(line) for line in lines)


def format_result(result, output_format):
    if result.command == "device-check" and output_format == "human":
        return _human(result)
    return json.dumps(asdict(result), ensure_ascii=False, allow_nan=False)


def presentation_context(argv):
    """Retain explicit JSON even when the sanitized parser rejects other device arguments."""
    if not argv or argv[0] != "device-check":
        return "unknown", "json"
    output_format = "human"
    for index, token in enumerate(argv[1:], start=1):
        if token == "--":
            break
        option, separator, value = token.partition("=")
        if len(option) <= 2 or not option.startswith("--"):
            continue
        if "--json".startswith(option) and not separator:
            output_format = "json"
        elif "--format".startswith(option):
            value = value if separator else argv[index + 1] if index + 1 < len(argv) else None
            if value in ("human", "json"):
                output_format = value
    return "device-check", output_format
