"""Frozen model and machine details; never inspect current hardware or weight files."""

from pathlib import PureWindowsPath


def recorded_integer(value):
    return value if type(value) is int and value >= 0 else None


def item(label, value, *, source, reason="not_recorded", unit=None):
    if unit in ("bytes", "count") and value is not None and recorded_integer(value) is None:
        value, reason = None, "invalid_frozen_value"
    return {
        "label": label,
        "value": value,
        "unit": unit,
        "source": source,
        "missing_reason": reason if value is None else None,
    }


def profile_view(data):
    config, identity = data["config"], data.get("identity", {})
    model, engine = config["model"], config["engine"]
    environment = data.get("environment_start", {})
    filename = PureWindowsPath(model["local_path"]).name
    generic = model["display_name"].lower() in {"hf", "unknown", "local", "模型"}
    stem = PureWindowsPath(filename).stem
    if stem.endswith("-" + model["packing"]):
        stem = stem[: -len(model["packing"]) - 1]
    title = stem.replace("-", " ") if generic else model["display_name"]
    files = identity.get("files", [])
    sizes = {
        entry["size"]
        for entry in files
        if entry.get("path") == model["local_path"]
        and entry.get("sha256") == model["sha256"]
        and type(entry.get("size")) is int
        and entry["size"] >= 0
    }
    size = next(iter(sizes)) if len(sizes) == 1 else None
    model_rows = [
        item("配置显示名", model["display_name"], source="config.model.display_name"),
        item("模型文件", filename, source="config.model.local_path"),
        item("量化 / 打包", model["packing"], source="config.model.packing"),
        item("权重文件大小", size, source="identity.files:matching_path_and_sha256", unit="bytes"),
        item("模型来源", model["repo"], source="config.model.repo"),
        item("版本 / Revision", model["revision"], source="config.model.revision"),
        item("结构参数量", None, source="not_recorded", reason="parameter_count_not_frozen"),
        item("引擎版本", engine["release"], source="config.engine.release"),
        item("执行后端", engine["backend"], source="config.engine.backend"),
        item("适配器", engine["adapter"], source="config.engine.adapter"),
    ]
    os_release = environment.get("os_release") or {}
    hardware = identity.get("device_preflight", {}).get("hardware", {})
    env_source = "environment.start.json"
    machine_rows = [
        item("设备编号", config["device"]["id"], source="config.device.id"),
        item("系统", environment.get("platform"), source=env_source),
        item("系统版本", os_release.get("VERSION_ID"), source=env_source),
        item("架构", environment.get("architecture"), source=env_source),
        item("CPU", environment.get("cpu_model"), source=env_source),
        item("逻辑 CPU", environment.get("logical_cpus"), source=env_source, unit="count"),
        item("总内存", environment.get("memory_total_bytes"), source=env_source, unit="bytes"),
        item(
            "开始时可用内存",
            environment.get("mem_available_bytes"),
            source=env_source,
            unit="bytes",
        ),
        item("内核", environment.get("kernel"), source=env_source),
        item("接通电源", environment.get("ac_online"), source=env_source),
        item("电源配置", environment.get("profile"), source=env_source),
        item(
            "预检可用磁盘",
            hardware.get("disk_free_bytes"),
            source="identity.device_preflight",
            unit="bytes",
        ),
    ]
    gpu = environment.get("gpu") or {}
    graphics = [
        {
            "name": device.get("name"),
            "cores": recorded_integer(device.get("cores")),
            "memory_kind": device.get("memory_kind"),
            "memory_total_bytes": recorded_integer(device.get("memory_total_bytes")),
            "memory_missing_reason": (device.get("missing") or {}).get(
                "memory_total_bytes", "not_recorded"
            ),
            "source": gpu.get("source", "not_recorded"),
        }
        for device in gpu.get("devices", [])
    ]
    verified = identity.get("effective_parameters", {}).get("parameters", {})
    parameters = []
    for key, value in config["generation"].items():
        actual = verified.get(key) or {}
        parameters.append(
            {
                "name": key,
                "requested": value,
                "effective": actual.get("effective"),
                "verification": actual.get("verification", "unknown"),
                "source": actual.get("source", "not_recorded"),
            }
        )
    return {
        "title": title,
        "title_source": "frozen_model_filename" if generic else "config.model.display_name",
        "model_rows": model_rows,
        "machine_rows": machine_rows,
        "graphics": graphics,
        "machine": {
            "cpu": environment.get("cpu_model"),
            "memory_total_bytes": recorded_integer(environment.get("memory_total_bytes")),
            "logical_cpus": recorded_integer(environment.get("logical_cpus")),
            "os_version": os_release.get("VERSION_ID"),
            "platform": environment.get("platform"),
            "architecture": environment.get("architecture"),
        },
        "generation": parameters,
        "conditions": config["conditions"],
        "execution": config["execution"],
        "telemetry": config["telemetry"],
        "engine": engine,
        "model": model,
        "endpoint_identity": {
            key: config["endpoint"].get(key) for key in ("server_pid", "process_start_ticks")
        },
        "measurement_source_sha256": data["run"]["tool_source_sha256"],
        "identity_verification": identity.get("verification", "unknown"),
        "limitations": [
            "frozen_observations_not_current_hardware",
            "model_size_is_file_bytes_not_runtime_memory",
        ],
    }
