package pages

import (
	"encoding/json"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"path/filepath"
	"regexp"
	"strings"
)

var directoryName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validName(s string) bool { return directoryName.MatchString(s) && strings.Trim(s, ".") != "" }
func quote(s string) string   { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func (s *viewState) modelsView(c *ui.Context) {
	s.header(c, "模型与下载")
	card(c, "本地资产", func() {
		ui.Text(c, "模型目录："+text(s.settings.Effective.ModelsRoot))
		ui.Text(c, "引擎目录："+filepath.Join(emptyText(s.settings.Effective.WorkspaceRoot), "engines"))
		var snap struct {
			Models any `json:"models"`
		}
		_ = json.Unmarshal(s.device.Snapshot, &snap)
		if snap.Models != nil {
			kvview(c, snap.Models)
		} else {
			ui.Text(c, "尚无资产快照，请到设备页刷新。")
		}
	})
	card(c, "创建模型下载", func() {
		field(c, "模型 URL（固定 40 位 revision 的 GGUF）", &s.modelURL)
		field(c, "新目录名", &s.modelName)
		field(c, "凭据环境变量名（可选）", &s.tokenEnv)
		ui.Text(c, "仅填写环境变量名。凭据由启动 Studio 的环境提供。目录名仅允许字母、数字、点、下划线与连字符，不能全为点。")
		if ui.PrimaryButton(c, "下载模型").AlignSelf(ui.Start).Disabled(s.busy || !validName(s.modelName) || strings.TrimSpace(s.modelURL) == "").Clicked() {
			s.acquireModel()
		}
	})
	card(c, "下载引擎", func() {
		field(c, "引擎 URL", &s.engineURL)
		field(c, "引擎新目录名", &s.engineName)
		if s.engineDownloadStatus != "" {
			ui.Text(c, s.engineDownloadStatus)
		}
		if ui.Button(c, "下载引擎").AlignSelf(ui.Start).Disabled(s.busy || !validName(s.engineName) || s.engineURL == "" || emptyText(s.settings.Effective.WorkspaceRoot) == "").Clicked() {
			s.fetchEngine()
		}
	})
	card(c, "下载队列", func() { s.downloadsView(c) })
}
func (s *viewState) acquireModel() {
	source, name, env := s.modelURL, s.modelName, s.tokenEnv
	if env != "" && !environmentName.MatchString(env) {
		s.failure = &core.ApiError{Code: "invalid_request", Message: "只接受环境变量名，请勿输入明文 token。"}
		return
	}
	root := emptyText(s.settings.Effective.ModelsRoot)
	if root == "" {
		s.failure = core.Conflict("settings_incomplete")
		return
	}
	source = strings.TrimSpace(source)
	if env == "" {
		s.work(func() (string, *core.ApiError) {
			_, e := s.service.CreateDownload(core.CreateDownloadRequest{SourceUrl: source, OutName: name})
			return "已加入下载队列。", e
		})
		return
	}
	command := "inferyard model acquire --source " + quote(source) + " --out " + quote(filepath.Join(root, name))
	if env != "" {
		command += " --token-env " + quote(env)
	}
	s.work(func() (string, *core.ApiError) {
		r, e := s.service.WizardExecute(s.ctx, core.WizardRequest{Command: command})
		if e == nil && r.ExitCode != 0 {
			e = &core.ApiError{Code: string(r.Status), Message: "下载未完成，输出目录保留；请检查来源并更换新目录。"}
		}
		return "下载完成。", e
	})
}

func (s *viewState) fetchEngine() {
	source := s.engineURL
	out := fresh(filepath.Join(emptyText(s.settings.Effective.WorkspaceRoot), "engines"), s.engineName)
	s.guideWork(func() (string, *core.ApiError) {
		defer s.win.Update(func() { s.engineDownloadStatus = "" })
		path, e := s.service.FetchEngine(s.ctx, source, out, func(status string) {
			s.win.Update(func() { s.engineDownloadStatus = status })
		})
		if e != nil {
			return "", e
		}
		s.win.Update(func() { s.engine = path; s.enginePreset = true })
		return "引擎已就绪：" + path, nil
	})
}
