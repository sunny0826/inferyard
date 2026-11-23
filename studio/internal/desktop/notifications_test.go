package desktop

import (
	"encoding/json"
	"strings"
	"testing"

	"inferyard/studio/internal/core"
)

func TestDesktopNotificationMessagesAreLocalizedChinese(t *testing.T) {
	mustJSON := func(v any) json.RawMessage {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	bundle := "core"
	cases := []struct {
		name      string
		event     core.Event
		wantTitle string
	}{
		{
			name:      "report created",
			event:     core.Event{Type: "report.created", Data: mustJSON(map[string]any{"report": map[string]any{"report_id": "rep-1"}})},
			wantTitle: "报告已生成",
		},
		{
			name:      "benchmark done",
			event:     core.Event{Type: "run.state", Data: mustJSON(map[string]any{"task": map[string]any{"kind": "benchmark", "state": "done", "bundle": &bundle}})},
			wantTitle: "测评题集 core已完成",
		},
		{
			name:      "context speed failed",
			event:     core.Event{Type: "run.state", Data: mustJSON(map[string]any{"task": map[string]any{"kind": "context_speed", "state": "failed"}})},
			wantTitle: "长上下文专项失败",
		},
		{
			name:      "model download done",
			event:     core.Event{Type: "download.state", Data: mustJSON(map[string]any{"task": map[string]any{"kind": "model", "state": "done", "out_name": "qwen"}})},
			wantTitle: "模型下载完成",
		},
		{
			name:      "polystrata install done",
			event:     core.Event{Type: "download.state", Data: mustJSON(map[string]any{"task": map[string]any{"kind": "polystrata", "state": "done", "display_name": "PolyStrata · qwen36", "out_name": "polystrata-1"}})},
			wantTitle: "PolyStrata 安装完成",
		},
		{
			name:      "polystrata install failed",
			event:     core.Event{Type: "download.state", Data: mustJSON(map[string]any{"task": map[string]any{"kind": "polystrata", "state": "failed", "display_name": "PolyStrata · qwen36"}})},
			wantTitle: "PolyStrata 安装失败",
		},
		{
			name:      "polystrata install cancelled",
			event:     core.Event{Type: "download.state", Data: mustJSON(map[string]any{"task": map[string]any{"kind": "polystrata", "state": "cancelled"}})},
			wantTitle: "PolyStrata 安装已取消",
		},
		{
			name:      "engine orphaned",
			event:     core.Event{Type: "engine.state", Data: mustJSON(map[string]any{"engine": map[string]any{"state": "orphaned"}})},
			wantTitle: "检测到遗留模型服务",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, ok := messageFor(tc.event)
			if !ok {
				t.Fatalf("expected message for %s", tc.name)
			}
			if msg.Title != tc.wantTitle {
				t.Fatalf("title = %q, want %q", msg.Title, tc.wantTitle)
			}
			if strings.TrimSpace(msg.Body) == "" {
				t.Fatalf("empty body for %s", tc.name)
			}
		})
	}
}
