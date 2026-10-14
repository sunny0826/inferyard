package pages

var deviceLabels = map[string]string{
	"metal":                       "Metal 加速",
	"cpu":                         "CPU",
	"blocked":                     "暂不可运行",
	"recommended":                 "已推荐",
	"unified_memory_fit_estimate": "统一内存容量估算可容纳",
	"memory_estimates_require_live_preflight":                             "内存容量为估算，运行前需实时预检确认",
	"runtime_backend_and_model_compatibility_require_verification":        "后端与模型兼容性需实际运行验证",
	"accelerator_execution_requires_verified_backend_and_service_binding": "加速执行需绑定已验证的服务",
	"shared_host_memory":      "与主机共享内存，不单独计量",
	"not_measured":            "未测量",
	"live_preflight_required": "需先完成实时预检",
}

func deviceLabel(value string) string {
	if label, ok := deviceLabels[value]; ok {
		return label
	}
	return value
}
