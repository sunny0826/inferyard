package pages

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

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
		"✕ 暂不可运行", "推荐方案：Fixture Model · Metal 加速 · 上下文 4096 · 5 线程",
		"处理器", "Fixture CPU · 10 核", "统一内存容量估算可容纳", "需先完成实时预检",
		"GPU 显存", "缺测 — 与主机共享内存，不单独计量", "≥ 2.83 GiB", "≥ 5.00 GiB",
		"16.00 GiB（当前可用约 4.00 GiB，估算）", "已接通", "arm64（Apple 芯片）",
		"本地模型 1 个 GGUF", "100.57 MiB · llama · 上下文 8192", "可用",
		"或先用探测验证候选配置：", "支持", "未验证", "绑定服务后验证",
		"本次检查未发送模型请求", "刷新设备", "前往运行页", "复制",
	} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	captured, _ := time.Parse(time.RFC3339, *s.device.CapturedAt)
	if !tt.HasText("快照 " + captured.Local().Format("2006-01-02 15:04:05")) {
		t.Fatal(tt.Texts())
	}
	for _, unwanted := range []string{"ready_to_run", "unified_memory_fit_estimate", "可运行：false", " bytes", "Other Model"} {
		if tt.HasText(unwanted) {
			t.Fatalf("unexpected %q", unwanted)
		}
	}
	if err := tt.Click("复制"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != "inferyard probe --config CONFIG.toml" {
		t.Fatal(tt.Clipboard())
	}
	if err := tt.Click("模型文件与原始参数"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Other Model · CPU") || slices.Contains(tt.Texts(), "Fixture Model · Metal 加速") {
		t.Fatal("candidate deduplication", tt.Texts())
	}
	if err := tt.Click("数据来源与缺测明细"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"temperature：缺测 — unsupported", "cpu_model ← fixture:cpu", "检测位置：/fixture/workspace", "memory_free_bytes：缺测 — 未测量"} {
		if !tt.HasText(want) {
			t.Fatalf("missing inline detail %q: %v", want, tt.Texts())
		}
	}
	tt.SetDark(true)
	if !tt.HasText("✕ 暂不可运行") {
		t.Fatal("dark theme lost conclusion")
	}
	if err := tt.Click("前往运行页"); err != nil {
		t.Fatal(err)
	}
	if s.selected != "runs" {
		t.Fatal("run navigation failed")
	}
}

func TestDeviceReadyAndRefreshing(t *testing.T) {
	s := deviceTestState()
	s.device.Snapshot = json.RawMessage(strings.Replace(deviceFixture, `"ready_to_run":false`, `"ready_to_run":true`, 1))
	tt := ui.NewTester(s.view, 1120, 2600)
	if !tt.HasText("✓ 可运行") || !tt.HasText("设备检查已通过，前往运行页创建候选并运行题包") || tt.HasText("CONFIG.toml") {
		t.Fatal(tt.Texts())
	}
	s.device.Refreshing = true
	tt.Frame()
	if !tt.HasText("正在采集设备信息…") || tt.HasText("快照 2026") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("刷新设备"); err != nil {
		t.Fatal(err)
	}
	if s.busy {
		t.Fatal("refresh button must be disabled while refreshing")
	}
	s.device.Refreshing = false
	s.device.Snapshot = json.RawMessage(strings.Replace(string(s.device.Snapshot), `"model_requests_sent":0`, `"model_requests_sent":2`, 1))
	tt.Frame()
	if !tt.HasText("模型请求数：2") || tt.HasText("本次检查未发送模型请求") {
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
	for _, rec := range []string{
		`{"status":"blocked","reason":"fixture blocked","selected":{"model_name":"Hidden model"}}`,
		`{"status":"recommended","selected":null,"reason":"unified_memory_fit_estimate"}`,
	} {
		s := deviceTestState()
		s.device.Snapshot = json.RawMessage(`{"recommendation":` + rec + `,"hardware":{"cpu_model":"Fixture CPU","missing":{"temperature":"unsupported"}},"next_steps":[{"action":"future_action","reason":"future_reason"}]}`)
		tt := ui.NewTester(s.view, 1120, 1800)
		if tt.HasText("推荐方案") || tt.HasText("上下文长度") || tt.HasText("模型文件与原始参数") || tt.HasText("Hidden model") {
			t.Fatal(tt.Texts())
		}
		if !tt.HasText("状态：") || !tt.HasText("future_action · future_reason") {
			t.Fatal(tt.Texts())
		}
		want := "fixture blocked"
		if strings.Contains(rec, "unified_memory_fit_estimate") {
			want = "统一内存容量估算可容纳"
		}
		if !tt.HasText(want) {
			t.Fatal(tt.Texts())
		}
		if err := tt.Click("数据来源与缺测明细"); err != nil {
			t.Fatal(err)
		}
		if !tt.HasText("temperature：缺测 — unsupported") {
			t.Fatal(tt.Texts())
		}
	}
}

func TestDeviceInvalidSnapshot(t *testing.T) {
	for _, raw := range []string{`{`, `[]`, `{"hardware":{}} trailing`} {
		s := deviceTestState()
		s.device.Snapshot = json.RawMessage(raw)
		tt := ui.NewTester(s.view, 1120, 800)
		if !tt.HasText("设备快照无法读取。") || tt.HasText("推荐配置") {
			t.Fatal(tt.Texts())
		}
	}
}

func TestDeviceBlockedDeduplicatesReasons(t *testing.T) {
	s := deviceTestState()
	s.device.Snapshot = json.RawMessage(`{
	 "ready_to_run":false,"model_requests_sent":0,
	 "hardware":{"cpu_model":"Fixture CPU"},
	 "recommendation":{"status":"blocked","reason":"insufficient_output_disk","selected":null},
	 "platform_capabilities":{"device_inspection":true,"live_single_run":false,
	  "live_experiments":false,"metal_backend_verified":true},
	 "next_steps":[{"action":"resolve_blocker","reason":"insufficient_output_disk"},
	  {"action":"provide_local_gguf","reason":"no_local_model"}]
	}`)
	tt := ui.NewTester(s.view, 1120, 2600)
	for _, want := range []string{
		"原因：输出位置可用磁盘不足 5 GiB、未发现本地 GGUF 模型",
		"解决阻断项 · 输出位置可用磁盘不足 5 GiB",
		"提供本地 GGUF 模型（未发现本地 GGUF 模型）",
		"前往模型与下载页", "已验证", "不支持", "缺测",
	} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"insufficient_output_disk", "no_local_model", "缺测 — 缺测"} {
		if tt.HasText(unwanted) {
			t.Fatalf("unexpected %q", unwanted)
		}
	}
	if err := tt.Click("前往模型与下载页"); err != nil {
		t.Fatal(err)
	}
	if s.selected != "models" {
		t.Fatal("models navigation failed")
	}
}

func TestDeviceUnavailableModel(t *testing.T) {
	s := deviceTestState()
	s.device.Snapshot = json.RawMessage(`{
	 "ready_to_run":false,"model_requests_sent":0,
	 "hardware":{"cpu_model":"Fixture CPU"},
	 "recommendation":{"status":"blocked","reason":"invalid_or_unreadable_gguf","selected":null},
	 "models":[{"path":"/fixture/broken.gguf","status":"unavailable","reason":"invalid_or_unreadable_gguf"}],
	 "next_steps":[{"action":"provide_local_gguf","reason":"invalid_or_unreadable_gguf"}]
	}`)
	tt := ui.NewTester(s.view, 1120, 1800)
	for _, want := range []string{"broken.gguf", "不可用", "/fixture/broken.gguf", "模型不存在、不可读或 GGUF 元数据无效"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"上下文 缺测", "invalid_or_unreadable_gguf", "unavailable"} {
		if tt.HasText(unwanted) {
			t.Fatalf("unexpected %q", unwanted)
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
