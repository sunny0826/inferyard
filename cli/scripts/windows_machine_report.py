"""Markdown rendering for the Windows preparation inventory."""

REFERENCES = {
    "cpu_sets": "https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-system_cpu_set_information",
    "commit_memory": "https://learn.microsoft.com/en-us/windows/win32/api/psapi/ns-psapi-performance_information",
    "power": "https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-system_power_status",
    "gpu_wmi_limits": "https://learn.microsoft.com/en-us/windows/win32/cimwin32prov/win32-videocontroller",
    "feature_flags": "https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-isprocessorfeaturepresent",
}


def section_data(inventory, name):
    return inventory["sections"][name].get("data")


def gib(value):
    return f"{value / 1024**3:.2f} GiB" if type(value) in (int, float) else "未知"


def safe(value):
    return str(value).replace("|", "\\|").replace("\r", " ").replace("\n", " ")


def label(value):
    if value is None:
        return "未知"
    if type(value) is bool:
        return "是" if value else "否"
    return str(value)


def null_fields(value, prefix=""):
    result = []
    if value is None:
        return [prefix]
    if isinstance(value, dict):
        for key, item in value.items():
            result.extend(null_fields(item, f"{prefix}.{key}" if prefix else key))
    elif isinstance(value, list):
        for index, item in enumerate(value):
            result.extend(null_fields(item, f"{prefix}[{index}]"))
    return result


def render_report(inventory):
    cpu = section_data(inventory, "processors") or []
    system = section_data(inventory, "system") or []
    os_info = section_data(inventory, "os") or []
    memory = section_data(inventory, "memory") or {}
    topology = section_data(inventory, "cpu_topology") or {}
    power = section_data(inventory, "power_status") or {}
    load = section_data(inventory, "background_load") or {}
    os_label = " / ".join(
        str(item.get("Caption")) + " " + str(item.get("Version")) for item in os_info
    )
    lines = [
        "# Windows benchmark 准备阶段机器清单",
        "",
        f"采集时间：{inventory['started_utc']} 至 {inventory['finished_utc']}。",
        "这是准备阶段的静态信息和即时观测，动态资源会在启动前重新检查。",
        "",
        "| 项目 | 观测 |",
        "| --- | --- |",
        f"| 设备 | {safe(' / '.join(str(item.get('Model')) for item in system))} |",
        f"| 系统 | {safe(os_label)} |",
        f"| CPU | {safe(' / '.join(str(item.get('Name')) for item in cpu))} |",
        f"| CPU 拓扑 | {topology.get('physical_cores', '未知')} 物理核 / "
        f"{topology.get('logical_processors', '未知')} 逻辑处理器 |",
        f"| 系统可见物理内存 | {gib(memory.get('PhysicalTotal_bytes'))} |",
        f"| 当前可用物理内存 | {gib(memory.get('PhysicalAvailable_bytes'))} |",
        f"| 当前提交量 / 提交上限 | {gib(memory.get('CommitTotal_bytes'))} / "
        f"{gib(memory.get('CommitLimit_bytes'))} |",
        f"| 接通电源 / 电池存在 | {label(power.get('ac_online'))} / "
        f"{label(power.get('battery_present'))} |",
        f"| CPU 忙碌比例（1 秒采样） | {label(load.get('cpu_busy_percent_mean'))}% |",
        "",
        "## CPU 与内存",
        "",
    ]
    for kind, group in topology.get("efficiency_classes", {}).items():
        lines.append(
            f"- EfficiencyClass {kind}：{group['physical_cores']} 核，"
            f"逻辑处理器 {group['logical_processors']}。"
        )
    lines += ["", "类值越高表示更快、能效更低；保留 OS 类值，不猜测 P/E 标签。", ""]
    for item in section_data(inventory, "memory_modules") or []:
        lines.append(
            f"- {safe(item.get('DeviceLocator'))}：{gib(item.get('Capacity'))}，"
            f"配置速率 {item.get('ConfiguredClockSpeed')} MHz，{safe(item.get('PartNumber'))}。"
        )
    lines += ["", "## GPU、磁盘与后台负载", ""]
    for item in section_data(inventory, "video_controllers") or []:
        lines.append(
            f"- GPU：{safe(item.get('Name'))}，驱动 {safe(item.get('DriverVersion'))}。"
            "WMI AdapterRAM 仅保留原始值。"
        )
    nvidia = section_data(inventory, "nvidia") or {}
    for item in nvidia.get("gpus", []):
        lines.append(
            f"- NVIDIA：{safe(item['name'])}，总显存 {item['memory.total']} MiB，"
            f"可用 {item['memory.free']} MiB，温度 {item['temperature.gpu']} °C。"
        )
    plan = section_data(inventory, "power_plan") or {}
    lines += ["", f"当前电源方案：{safe(plan.get('stdout', '未知').strip())}", ""]
    for item in section_data(inventory, "physical_disks") or []:
        lines.append(
            f"- 物理盘：{safe(item.get('FriendlyName'))}，{safe(item.get('MediaType'))} / "
            f"{safe(item.get('BusType'))}，容量 {gib(item.get('Size'))}。"
        )
    for item in section_data(inventory, "volumes") or []:
        lines.append(
            f"- 卷 {safe(item['DeviceID'])}：{safe(item['FileSystem'])}，"
            f"总量 {gib(item['Size'])}，可用 {gib(item['FreeSpace'])}。"
        )
    lines += ["", "| 当前进程 | 工作集 |", "| --- | --- |"]
    for item in load.get("top_working_sets", []):
        lines.append(f"| {safe(item['name'])} ({item['pid']}) | {gib(item['working_set_bytes'])} |")
    lines += ["", "工作集含共享页，不能相加作为系统占用。", "", "## 当前候选配置参考", ""]
    candidate = section_data(inventory, "candidate_reference")
    if candidate:
        lines += [
            f"- 配置：`{candidate['config_path']}`。线程 {candidate['configured_threads']}，"
            f"上下文 {candidate['configured_context']}，槽位 {candidate['configured_slots']}。",
            f"- 加载后可用内存下限：{gib(candidate['loaded_model_memory_floor_bytes'])}；"
            f"模型大小：{gib(candidate['model_bytes'])}；工作空间预留：1 GiB。",
            f"- 启动前所需可用内存：{gib(candidate['required_before_model_start_bytes'])}；"
            f"本次观测满足：{label(candidate['preparation_memory_sufficient'])}；"
            f"缺口：{gib(candidate['memory_shortfall_bytes'])}。",
            f"- 输出磁盘预算满足：{label(candidate['preparation_disk_sufficient'])}；"
            f"电源条件满足：{label(candidate['ac_matches'])}；"
            f"目标端口已监听：{label(candidate['endpoint_port_listening'])}。",
            "- 原配置预算未调整；准备清单不授予性能或硬件资格。",
        ]
    else:
        lines.append("没有可用的候选配置参考，查看 JSON 中的缺失原因。")
    lines += [
        "",
        "## 采集覆盖与缺失",
        "",
        "| 类别 | 状态 | 来源 / 缺失原因 |",
        "| --- | --- | --- |",
    ]
    for name, section in inventory["sections"].items():
        lines.append(
            f"| {name} | {section['status']} | {safe(section['source'])} / "
            f"{safe(section.get('missing_reason') or '')} |"
        )
    lines += ["", "未获得数值的字段路径（见 JSON 的 null_fields）：", ""]
    lines += [f"- `{path}`" for path in inventory.get("null_fields", [])]
    lines += [
        "",
        *[f"- {note}" for note in inventory["limitations"]],
        "",
        "数值为 null 或提供者默认值（如 0）时，不推定对应能力成立。"
        "CIM 原始字段使用微软单位：内存条 Capacity 为 bytes，"
        "缓存和 OS 内存字段为 KiB，分页文件字段为 MiB；JSON 单位说明见 field_units。",
        "",
        "## API 参考",
        "",
        *[f"- [{key}]({url})" for key, url in REFERENCES.items()],
        "",
    ]
    return "\n".join(lines)
