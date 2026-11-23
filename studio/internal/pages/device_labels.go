package pages

import "strings"

var deviceLabels = map[string]string{
	"metal":                                   "Metal 加速",
	"cuda":                                    "CUDA 加速",
	"hip":                                     "HIP (ROCm) 加速",
	"cpu":                                     "CPU",
	"polystrata":                              "PolyStrata",
	"llama-cpp":                               "llama.cpp",
	"fits":                                    "推荐适配",
	"tight":                                   "紧凑适配",
	"fallback":                                "轻量回退",
	"blocked":                                 "暂不可运行",
	"recommended":                             "已推荐",
	"device_snapshot_unavailable":             "尚未采集设备快照",
	"rocm_smi_not_found":                      "未检测到 rocm-smi",
	"unsupported_gpu":                         "未发现支持的加速 GPU",
	"insufficient_memory":                     "可用内存不足",
	"insufficient_disk":                       "可用磁盘空间不足",
	"insufficient_disk_space":                 "可用磁盘空间不足",
	"disk_space_unavailable":                  "磁盘可用空间缺测",
	"disk_usage_unavailable":                  "磁盘可用空间缺测",
	"unsupported_platform":                    "当前平台暂不支持",
	"unified_memory_fit_estimate":             "统一内存容量估算可容纳",
	"gpu_and_memory_fit_estimate":             "GPU 与内存容量估算可容纳",
	"host_memory_fit_estimate":                "主机内存容量估算可容纳",
	"host_memory_unavailable":                 "可用内存缺测，无法估算模型容量",
	"insufficient_host_memory":                "主机可用内存不足",
	"no_local_model":                          "未发现本地 GGUF 模型",
	"invalid_or_unreadable_gguf":              "模型不存在、不可读或 GGUF 元数据无效",
	"chat_template_unavailable":               "模型缺少聊天模板",
	"insufficient_output_disk":                "输出位置可用磁盘不足 5 GiB",
	"output_disk_unavailable":                 "输出位置磁盘容量缺测",
	"nvidia_smi_not_found":                    "未检测到 nvidia-smi",
	"nvidia_driver_query_failed":              "NVIDIA 驱动查询失败",
	"probe_timeout":                           "系统查询超时",
	"memory_estimates_require_live_preflight": "内存容量为估算值",
	"runtime_backend_and_model_compatibility_require_verification":        "后端与模型兼容性待运行验证",
	"accelerator_execution_requires_verified_backend_and_service_binding": "需绑定已验证服务后启用加速",
	"shared_host_memory":                  "共享系统内存",
	"not_measured":                        "未测量",
	"not_reported":                        "未报告",
	"live_preflight_required":             "需先完成实时预检",
	"provide_local_gguf":                  "提供本地 GGUF 模型",
	"resolve_blocker":                     "解决阻断项",
	"configure_and_bind_existing_service": "绑定已有的模型服务",
	"unavailable":                         "不可用",
	"polystrata_hardware_fit":             "硬件适配 PolyStrata",
	"fallback_llama_cpp_no_compatible_polystrata_gpu":     "当前 GPU 不满足 PolyStrata 要求，已切换为 llama.cpp",
	"fallback_llama_cpp_insufficient_ram_for_polystrata":  "当前内存不足以运行 PolyStrata 模型，已切换为 llama.cpp",
	"fallback_llama_cpp_insufficient_disk_for_polystrata": "当前磁盘空间不足以安装 PolyStrata，已切换为 llama.cpp",
	"fallback_llama_cpp_insufficient_vram_for_polystrata": "当前显存不足以运行 PolyStrata 模型，已切换为 llama.cpp",
	"apple_silicon_required":                              "需要 Apple Silicon 芯片",
	"macos_version_below_15":                              "需要 macOS 15 或更高版本",
	"macos_version_unverified":                            "macOS 版本未检测到",
	"metal_not_supported":                                 "当前设备不支持 Metal",
	"nvidia_driver_too_old":                               "NVIDIA 驱动版本过低",
	"nvidia_driver_version_unverified":                    "NVIDIA 驱动版本未检测到",
	"nvidia_compute_capability_unsupported":               "NVIDIA GPU 算力架构不受支持",
	"nvidia_compute_capability_unverified":                "NVIDIA GPU 算力未检测到",
	"nvidia_vram_unverified":                              "NVIDIA 显存容量未检测到",
	"amd_vram_unverified":                                 "AMD 显存容量未检测到",
	"amd_arch_unsupported":                                "AMD GPU 架构不受支持",
	"host_available_memory_tight":                         "当前可用内存偏紧，建议先关闭占用较大的程序",
	"windows_page_file_low":                               "Windows 页面文件偏小，建议不低于 16 GiB",
	"cpu_missing_avx2_experimental_build":                 "CPU 缺少 AVX2，将使用兼容构建",
	"total_memory_unverified":                             "系统总内存未检测到",
	"multiple_models_roots_unselected":                    "检测到多个模型目录，待确认目标目录",
	"xcode_clt_missing":                                   "缺少 Xcode Command Line Tools（构建 PolyStrata 所需）",
	"polystrata_config_model_missing":                     "配置文件引用的模型文件缺失",
}

func deviceLabel(value string) string {
	if label, ok := deviceLabels[value]; ok {
		return label
	}
	return value
}

func localizedDeviceNotes(notes []string) []string {
	if len(notes) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(notes))
	out := make([]string, 0, len(notes))
	for _, note := range notes {
		key := strings.TrimSpace(note)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		label := strings.TrimSpace(deviceLabel(key))
		if label != "" {
			out = append(out, label)
		}
	}
	return out
}
