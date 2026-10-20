//go:build ui_debug && !release

package pages

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestPreviewFirstUseAcquiresAssetsBeforeDeviceCheck(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "首次使用 · 无资源"))
	tt := previewTester(p, 1120, 2400)
	click(t, tt, "准备环境")
	p.advance(time.Second)
	tt.Frame()
	if p.state.busy || !tt.HasText("准备模型与引擎") || p.state.progress.DevicePreflight.Status != "missing" {
		t.Fatal("empty environment ran preflight or skipped resource guidance", tt.Texts())
	}
	click(t, tt, "到下载页")
	click(t, tt, "下载引擎")
	click(t, tt, "下载模型")
	if !tt.HasText("下载中 2 项") {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "返回任务")
	if !tt.HasText("模型 · 下载中") || !tt.HasText("引擎 · 下载中") {
		t.Fatal("task lost active downloads", tt.Texts())
	}
	p.advance(3 * time.Second)
	tt.Frame()
	p.advance(time.Second)
	tt.Frame()
	if !p.state.preflightReady() || !tt.HasText("运行 benchmark") || p.state.model == "" || p.state.engine == "" {
		t.Fatal("downloaded assets did not unlock setup", tt.Texts())
	}
	if len(p.state.checkedBundles()) != 2 {
		t.Fatal("first benchmark omitted a default bundle")
	}
	click(t, tt, "运行 benchmark")
	p.advance(2 * time.Second)
	tt.Frame()
	if !tt.HasText("zh-core · 运行中") || !tt.HasText("zh-svg-pelican · 排队中") || tt.HasText("单次运行不支持断点续跑") {
		t.Fatal("live task used offline recovery state", tt.Texts())
	}
	if slices.Contains(tt.Texts(), "排队中") {
		t.Fatal("duplicate unlabelled queue status", tt.Texts())
	}
	p.advance(20 * time.Second)
	tt.Frame()
	click(t, tt, "查看报告")
	if !tt.HasText("各类任务表现") || p.state.service != nil || p.state.taskCancel != nil || p.shellOptions().StateKey != "" {
		t.Fatal("first-use flow failed or escaped preview isolation", tt.Texts())
	}
}
