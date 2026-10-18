//go:build ui_debug && !release

package pages

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func assertSpeedIdentity(t *testing.T, tt *ui.Tester, task *speedTask) {
	t.Helper()
	for _, want := range []string{"模型 / 引擎", task.model, task.engine} {
		if !tt.HasText(want) {
			t.Fatalf("missing identity %q: %v", want, tt.Texts())
		}
	}
	for _, text := range tt.Texts() {
		for _, forbidden := range []string{"Card", "GPU", "显存", "GiB 共享"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("hardware identity in result: %s", text)
			}
		}
	}
}

func saveSpeedImage(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	if dir := os.Getenv("STUDIO_SPEED_SCREENSHOTS"); dir != "" {
		f, err := os.Create(filepath.Join(dir, name+"-software.png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		closeErr := f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
}

func TestPreviewSpeedHistoricalIdentity(t *testing.T) {
	p, tt := prepareSpeed(t, "128K · 131,072 tokens")
	click(t, tt, "开始专项测评")
	p.advance(10 * time.Second)
	tt.Frame()
	original := p.state.speed.current
	click(t, tt, "查看结果")
	assertSpeedIdentity(t, tt, original)
	saveSpeedImage(t, tt, "result")
	click(t, tt, "查看逐次测量")
	assertSpeedIdentity(t, tt, original)
	// A second recorded engine and changed current selection must not rewrite history.
	other := *original
	other.id, other.engine = "preview-other-engine", "Preview alternate engine"
	p.state.speed.history = append(p.state.speed.history, &other)
	p.state.model, p.state.engine = "Different current model", "Different current engine"
	tt.Frame()
	assertSpeedIdentity(t, tt, original)
	click(t, tt, "返回历史任务")
	assertSpeedIdentity(t, tt, original)
	if !tt.HasText(other.engine) {
		t.Fatal("same-model engine distinction missing", tt.Texts())
	}
	saveSpeedImage(t, tt, "history")
	click(t, tt, other.id)
	click(t, tt, "打开专项结果")
	assertSpeedIdentity(t, tt, &other)
	click(t, tt, "返回历史任务")
	click(t, tt, original.id)
	click(t, tt, "打开专项结果")
	assertSpeedIdentity(t, tt, original)
	click(t, tt, "用此组合新开任务")
	if !tt.HasText(original.model) || !tt.HasText(original.engine) || !tt.HasText("128K · 131,072 tokens") {
		t.Fatal("rerun form did not restore recorded combination", tt.Texts())
	}
	click(t, tt, "停止当前服务")
	p.advance(time.Second)
	tt.Frame()
	click(t, tt, "开始专项测评")
	p.advance(10 * time.Second)
	tt.Frame()
	rerun := p.state.speed.current
	if rerun == original || rerun.model != original.model || rerun.engine != original.engine || rerun.context != original.context || len(p.state.speed.history) != 3 || original.summary == nil {
		t.Fatal("rerun changed frozen identity or original record")
	}
	click(t, tt, "查看结果")
	assertSpeedIdentity(t, tt, rerun)
}
