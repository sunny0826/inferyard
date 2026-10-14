package pages

// deviceLabels 翻译 device_preflight.v1 快照中的机器码。
// blocked 原因码与 cli/src/inferyard/cli/device_output.py 的 REASONS 对齐。
var deviceLabels = map[string]string{
	// 加速方式
	"metal": "Metal 加速",
	"cuda":  "CUDA 加速",
	"cpu":   "CPU",
	// 推荐状态
	"blocked":     "暂不可运行",
	"recommended": "已推荐",
	// 推荐依据
	"unified_memory_fit_estimate": "统一内存容量估算可容纳",
	"gpu_and_memory_fit_estimate": "GPU 与内存容量估算可容纳",
	"host_memory_fit_estimate":    "主机内存容量估算可容纳",
	"host_memory_unavailable":     "可用内存缺测，无法估算模型容量",
	"insufficient_host_memory":    "主机可用内存不足",
	"no_local_model":              "未发现本地 GGUF 模型",
	"invalid_or_unreadable_gguf":  "模型不存在、不可读或 GGUF 元数据无效",
	"chat_template_unavailable":   "模型缺少聊天模板",
	"insufficient_output_disk":    "输出位置可用磁盘不足 5 GiB",
	"output_disk_unavailable":     "输出位置磁盘容量缺测",
	"nvidia_smi_not_found":        "未发现 nvidia-smi；NVIDIA GPU 未观测",
	"nvidia_driver_query_failed":  "NVIDIA 驱动查询失败",
	"probe_timeout":               "系统查询超时",
	// 限制
	"memory_estimates_require_live_preflight":                             "内存容量为估算，运行前需实时预检确认",
	"runtime_backend_and_model_compatibility_require_verification":        "后端与模型兼容性需实际运行验证",
	"accelerator_execution_requires_verified_backend_and_service_binding": "加速执行需绑定已验证的服务",
	// 缺测原因
	"shared_host_memory": "与主机共享内存，不单独计量",
	"not_measured":       "未测量",
	"not_reported":       "未报告",
	// 下一步
	"live_preflight_required":             "需先完成实时预检",
	"macos_live_not_supported":            "当前平台不支持真实运行",
	"provide_local_gguf":                  "提供本地 GGUF 模型",
	"resolve_blocker":                     "解决阻断项",
	"use_supported_live_platform":         "使用支持真实运行的平台",
	"configure_and_bind_existing_service": "绑定已有的模型服务",
	// 模型状态
	"unavailable": "不可用",
}

func deviceLabel(value string) string {
	if label, ok := deviceLabels[value]; ok {
		return label
	}
	return value
}
