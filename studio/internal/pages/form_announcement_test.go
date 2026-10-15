package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestFormFailureAnnouncedOnEverySubmission(t *testing.T) {
	for _, name := range []string{"settings", "resume", "model", "engine", "binding", "initialize"} {
		t.Run(name, func(t *testing.T) {
			s := testState()
			var render func(*ui.Context)
			var submit string
			switch name {
			case "settings":
				s.workspace = "relative/workspace"
				render, submit = s.settingsView, "保存设置"
			case "resume":
				s.resumeSource = &core.RunSummary{Path: "/results/run"}
				s.endpoint = "http://127.0.0.1:8080"
				render, submit = s.resumeView, "确认续测"
			case "model":
				s.modelURL, s.modelName = "invalid", "model"
				render, submit = s.modelDownloadForm, "下载模型"
			case "engine":
				s.engineURL, s.engineName = "invalid", "engine"
				render, submit = s.engineDownloadView, "下载引擎"
			case "binding":
				s.process, s.bindEndpoint = "42 · fixture", "invalid"
				render, submit = s.serviceView, "绑定进程"
			case "initialize":
				s.initDir = "relative/workspace"
				s.ready.CLI.Status = "ok"
				s.ready.Workspace.Initialized = false
				render, submit = s.setupGuide, "初始化工作区"
			}
			tt := ui.NewTester(func(c *ui.Context) {
				s.announceChanges(c)
				ui.Column(c).Children(func() { render(c) })
			}, 1120, 2400)
			if name == "binding" {
				tt.SetPreferences(ui.Preferences{ReduceMotion: true})
				click(t, tt, "高级：手动绑定")
			}
			tt.Announcements()
			for attempt := 1; attempt <= 2; attempt++ {
				if name == "initialize" {
					nodes := modelUINodes(tt, "text", submit)
					n := nodes[len(nodes)-1]
					tt.ClickAt(float32(n.FieldByName("x").Float()+n.FieldByName("w").Float()/2), float32(n.FieldByName("y").Float()+n.FieldByName("h").Float()/2))
				} else {
					click(t, tt, submit)
				}
				tt.Frame()
				if s.failure == nil || s.busy {
					t.Fatal("invalid form reached backend")
				}
				got := tt.Announcements()
				if len(got) != 1 || !strings.Contains(got[0], s.failure.Message) {
					t.Fatalf("submission %d: expected one error announcement, got %v", attempt, got)
				}
				for range 3 {
					tt.Frame()
				}
				if got := tt.Announcements(); len(got) != 0 {
					t.Fatal("idle frames repeated error", got)
				}
			}
		})
	}
}
