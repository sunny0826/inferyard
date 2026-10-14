package pages

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestModelsUnavailableModel(t *testing.T) {
	s := deviceTestState()
	s.selected = "models"
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

func TestModelsSnapshot(t *testing.T) {
	s := deviceTestState()
	s.selected = "models"
	tt := ui.NewTester(s.view, 1120, 2600)
	for _, want := range []string{"推荐配置", "本地模型 1 个 GGUF", "100.57 MiB · llama · 上下文 8192", "可用", "≥ 2.83 GiB", "≥ 5.00 GiB", "统一内存容量估算可容纳", "模型目录：", "引擎目录：", "添加其他模型", "下载预置模型", "下载预置引擎", "引擎服务", "绑定存活提示"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	for _, want := range []string{"工作区 / 资产与服务", "预置测试模型", "SmolLM2-135M", "Instruct · Q4_K_M · 固定版本 GGUF"} {
		if !tt.HasText(want) {
			t.Fatal(want, tt.Texts())
		}
	}
	if tt.HasText("凭据环境变量名（可选）") {
		t.Fatal("custom form starts open")
	}
	click(t, tt, "添加其他模型")
	for _, want := range []string{"创建模型下载", "模型 URL（固定 40 位 revision 的 GGUF）", "新目录名", "凭据环境变量名（可选）", "仅填写环境变量名。凭据由启动 Studio 的环境提供。"} {
		if !tt.HasText(want) {
			t.Fatal(want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"model_path", "size_bytes", "unified_memory_fit_estimate", "尚无设备快照", "Other Model"} {
		if tt.HasText(unwanted) {
			t.Fatalf("unexpected %q: %v", unwanted, tt.Texts())
		}
	}
	texts := tt.Texts()
	if slices.Index(texts, "推荐配置") >= slices.Index(texts, "本地模型 1 个 GGUF") || slices.Index(texts, "本地模型 1 个 GGUF") >= slices.Index(texts, "添加其他模型") {
		t.Fatal("model card order", texts)
	}
	if err := tt.Click("模型文件与原始参数"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Other Model · CPU") || slices.Contains(tt.Texts(), "Fixture Model · Metal 加速") || !tt.HasText("/fixture/model.gguf") {
		t.Fatal("candidate deduplication", tt.Texts())
	}
}

func TestModelsWithoutSnapshot(t *testing.T) {
	for _, raw := range []string{"", "null"} {
		t.Run("empty "+raw, func(t *testing.T) {
			s := deviceTestState()
			s.selected = "models"
			s.device.Snapshot = json.RawMessage(raw)
			tt := ui.NewTester(s.view, 1120, 1600)
			if !tt.HasText("尚无设备快照，请到设备页刷新。") || tt.HasText("推荐配置") || tt.HasText("本地模型") {
				t.Fatal(tt.Texts())
			}
			if !tt.HasText("添加其他模型") || !tt.HasText("下载引擎") || !tt.HasText("绑定存活提示") {
				t.Fatal(tt.Texts())
			}
		})
	}
	for _, raw := range []string{"{", "[]", `{"hardware":{}} trailing`} {
		t.Run("invalid "+raw, func(t *testing.T) {
			s := deviceTestState()
			s.selected = "models"
			s.device.Snapshot = json.RawMessage(raw)
			tt := ui.NewTester(s.view, 1120, 1600)
			if !tt.HasText("设备快照无法读取。") || tt.HasText("推荐配置") || tt.HasText("本地模型") {
				t.Fatal(tt.Texts())
			}
			if !tt.HasText("添加其他模型") || !tt.HasText("下载引擎") || !tt.HasText("绑定存活提示") {
				t.Fatal(tt.Texts())
			}
		})
	}
}

func TestModelsBlockedAndUnknownFallback(t *testing.T) {
	for _, rec := range []string{
		`{"status":"blocked","reason":"fixture blocked","selected":{"model_name":"Hidden model"}}`,
		`{"status":"recommended","selected":null,"reason":"unified_memory_fit_estimate"}`,
	} {
		s := deviceTestState()
		s.selected = "models"
		s.device.Snapshot = json.RawMessage(`{"recommendation":` + rec + `}`)
		tt := ui.NewTester(s.view, 1120, 1800)
		if !tt.HasText("状态：") || !(tt.HasText("fixture blocked") || tt.HasText("统一内存容量估算可容纳")) {
			t.Fatal(tt.Texts())
		}
		for _, unwanted := range []string{"推荐方案", "上下文长度", "模型文件与原始参数", "Hidden model", "unified_memory_fit_estimate"} {
			if tt.HasText(unwanted) {
				t.Fatalf("unexpected %q: %v", unwanted, tt.Texts())
			}
		}
	}
}
