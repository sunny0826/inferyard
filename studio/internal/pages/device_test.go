package pages

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

const deviceFixture = `{
 "ready_to_run":false,"model_requests_sent":0,
 "hardware":{
  "cpu_model":"Fixture CPU","logical_cpus":10,"architecture":"arm64",
  "apple_silicon":true,"memory_total_bytes":17179869184,
  "memory_available_bytes":4294967296,"memory_available_is_estimate":true,
  "disk_free_bytes":37580963840,"disk_path":"/fixture/workspace","ac_online":true,
  "gpu":{"devices":[{"name":"Fixture GPU","cores":10,"metal_supported":true,
   "memory_total_bytes":null,"missing":{"memory_total_bytes":"shared_host_memory",
   "memory_free_bytes":"not_measured"}}]},
  "sources":{"cpu_model":"fixture:cpu"},"missing":{"temperature":"unsupported"}
 },
 "recommendation":{
  "status":"recommended","reason":null,
  "selected":{"model_name":"Fixture Model","model_path":"/fixture/model.gguf",
   "mode":"metal","gpu_name":"Fixture GPU","context_size":4096,"threads":5,
   "min_available_memory_bytes":3038689362,"min_disk_bytes":5368709120,
   "reason":"unified_memory_fit_estimate"},
  "candidates":[{"model_name":"Fixture Model","model_path":"/fixture/model.gguf",
   "mode":"metal","gpu_name":"Fixture GPU","context_size":4096,"threads":5,
   "min_available_memory_bytes":3038689362,"min_disk_bytes":5368709120,
   "reason":"unified_memory_fit_estimate"},
   {"model_name":"Other Model","model_path":"/fixture/other.gguf","mode":"cpu"}],
  "limitations":["memory_estimates_require_live_preflight",
   "runtime_backend_and_model_compatibility_require_verification",
   "accelerator_execution_requires_verified_backend_and_service_binding"]
 },
 "models":[{"name":"Fixture Model","path":"/fixture/model.gguf","size_bytes":105454144,
  "architecture":"llama","context_length":8192,"status":"available"}],
 "platform_capabilities":{"device_inspection":true,"live_single_run":true,
  "live_experiments":true,"metal_backend_verified":false},
 "next_steps":[{"action":"configure_and_bind_existing_service","reason":"live_preflight_required"},
  {"action":"probe","command":["inferyard","probe","--config","CONFIG.toml"]}]
}`

func deviceTestState() *viewState {
	s := testState()
	s.selected = "device"
	s.device.Snapshot = json.RawMessage(deviceFixture)
	s.device.CapturedAt = ptr("2026-10-14T02:22:22Z")
	return &s
}

func TestDeviceLocalizedSnapshot(t *testing.T) {
	s := deviceTestState()
	tt := ui.NewTester(s.view, 1120, 2600)
	for _, want := range []string{
		"设备", "刷新设备",
		"处理器", "Fixture CPU · 10 核",
		"GPU 显存", "缺测 — 与主机共享内存，不单独计量",
		"16.00 GiB（当前可用约 4.00 GiB，估算）", "已接通", "arm64（Apple 芯片）",
	} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"ready_to_run", "unified_memory_fit_estimate", "可运行：false", "✓ 可运行", "暂不可运行", "下一步", "快照 2026", "本次检查未发送模型请求", "前往 Dashboard", "复制", " bytes", "本地模型", "平台能力", "推荐配置", "推荐方案"} {
		if tt.HasText(unwanted) {
			t.Fatalf("unexpected %q", unwanted)
		}
	}
	for _, unwanted := range []string{"检查设备", "数据来源与缺测明细", "temperature：缺测 — unsupported", "cpu_model ← fixture:cpu", "检测位置：workspace", "memory_free_bytes：缺测 — 未测量"} {
		if slices.Contains(tt.Texts(), unwanted) {
			t.Fatalf("unexpected detail %q: %v", unwanted, tt.Texts())
		}
	}
	tt.SetDark(true)
	if !tt.HasText("硬件") {
		t.Fatal("dark theme lost hardware card")
	}
}

func TestDeviceReadyAndRefreshing(t *testing.T) {
	s := deviceTestState()
	s.device.Snapshot = json.RawMessage(strings.Replace(deviceFixture, `"ready_to_run":false`, `"ready_to_run":true`, 1))
	tt := ui.NewTester(s.view, 1120, 2600)
	if !tt.HasText("硬件") || tt.HasText("✓ 可运行") || tt.HasText("暂不可运行") || tt.HasText("CONFIG.toml") {
		t.Fatal(tt.Texts())
	}
	s.device.Refreshing = true
	tt.Frame()
	if !tt.HasText("正在采集设备信息…") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("刷新设备"); err != nil {
		t.Fatal(err)
	}
	if s.busy {
		t.Fatal("refresh button must be disabled while refreshing")
	}
	s.device.Refreshing = false
	tt.Frame()
	if tt.HasText("正在采集设备信息…") {
		t.Fatal(tt.Texts())
	}
}

func TestDeviceEmptyAndError(t *testing.T) {
	for _, raw := range []string{"", "null"} {
		t.Run("empty "+raw, func(t *testing.T) {
			s := deviceTestState()
			s.device.Snapshot = json.RawMessage(raw)
			tt := ui.NewTester(s.view, 1120, 800)
			for _, want := range []string{"尚无设备快照", "刷新设备", "前往设置配置模型目录"} {
				if !tt.HasText(want) {
					t.Fatalf("missing %q: %v", want, tt.Texts())
				}
			}
			if err := tt.Click("前往设置配置模型目录"); err != nil {
				t.Fatal(err)
			}
			if s.selected != "settings" {
				t.Fatal("settings navigation failed")
			}
		})
	}
	for _, raw := range []string{"", deviceFixture} {
		s := deviceTestState()
		s.device.Snapshot = json.RawMessage(raw)
		s.device.LastError = &core.ApiError{Code: "cli_unavailable", Message: "设备检查失败"}
		tt := ui.NewTester(s.view, 1120, 2600)
		for _, want := range []string{"设备检查失败", "重试", "前往设置"} {
			if !tt.HasText(want) {
				t.Fatalf("missing %q: %v", want, tt.Texts())
			}
		}
		if tt.HasText("尚无设备快照") {
			t.Fatal("error state must not display empty state")
		}
		if err := tt.Click("前往设置"); err != nil {
			t.Fatal(err)
		}
		if s.selected != "settings" {
			t.Fatal("error settings navigation failed")
		}
	}
}

func TestDeviceBlockedAndUnknownFallback(t *testing.T) {
	s := deviceTestState()
	s.device.Snapshot = json.RawMessage(`{"recommendation":{"status":"blocked","reason":"fixture blocked","selected":{"model_name":"Hidden model"}},"hardware":{"cpu_model":"Fixture CPU","missing":{"temperature":"unsupported"}},"next_steps":[{"action":"future_action","reason":"future_reason"}]}`)
	tt := ui.NewTester(s.view, 1120, 1800)
	for _, unwanted := range []string{"推荐方案", "上下文长度", "模型文件与原始参数", "Hidden model", "future_action", "future_reason", "fixture blocked", "暂不可运行"} {
		if tt.HasText(unwanted) {
			t.Fatalf("unexpected %q", unwanted)
		}
	}
	if slices.Contains(tt.Texts(), "数据来源与缺测明细") {
		t.Fatal(tt.Texts())
	}
}

func TestDeviceInvalidSnapshot(t *testing.T) {
	for _, raw := range []string{`{`, `[]`, `{"hardware":{}} trailing`} {
		s := deviceTestState()
		s.device.Snapshot = json.RawMessage(raw)
		tt := ui.NewTester(s.view, 1120, 800)
		if !tt.HasText("设备快照无法读取。") || tt.HasText("推荐配置") ||
			!slices.Contains(tt.Texts(), "检查设备设置") || !slices.Contains(tt.Texts(), "刷新设备") ||
			slices.Contains(tt.Texts(), "检查设备") {
			t.Fatal(tt.Texts())
		}
	}
}

func TestDeviceMultipleGPUs(t *testing.T) {
	s := deviceTestState()
	s.device.Snapshot = json.RawMessage(`{
	 "ready_to_run":false,"model_requests_sent":0,
	 "hardware":{"cpu_model":"Fixture CPU",
	  "gpu":{"devices":[
	   {"name":"GPU Alpha","memory_total_bytes":17179869184},
	   {"name":"GPU Beta","memory_total_bytes":8589934592}]}},
	 "recommendation":{"status":"blocked","reason":"no_local_model","selected":null},
	 "next_steps":[{"action":"provide_local_gguf","reason":"no_local_model"}]
	}`)
	tt := ui.NewTester(s.view, 1120, 1800)
	for _, want := range []string{"GPU 1", "GPU 2", "GPU 1 显存", "GPU 2 显存", "GPU Alpha", "GPU Beta", "16.00 GiB", "8.00 GiB"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
}

func TestDeviceGPUUnavailableReason(t *testing.T) {
	s := deviceTestState()
	s.device.Snapshot = json.RawMessage(`{
	 "ready_to_run":false,"model_requests_sent":0,
	 "hardware":{"cpu_model":"Fixture CPU",
	  "gpu":{"status":"unavailable","reason":"nvidia_smi_not_found"}},
	 "recommendation":{"status":"blocked","reason":"no_local_model","selected":null},
	 "next_steps":[{"action":"provide_local_gguf","reason":"no_local_model"}]
	}`)
	tt := ui.NewTester(s.view, 1120, 1800)
	if !tt.HasText("缺测 — 未发现 nvidia-smi；NVIDIA GPU 未观测") {
		t.Fatal(tt.Texts())
	}
}
