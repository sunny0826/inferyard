package pages

import (
	"encoding/json"
	"fmt"
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
			fields(c, snap.Models)
		} else {
			ui.Text(c, "尚无资产快照，请到设备页刷新。")
		}
	})
	card(c, "创建模型下载", func() {
		field(c, "模型 URL（固定 40 位 revision 的 GGUF）", &s.modelURL)
		field(c, "新目录名", &s.modelName)
		field(c, "凭据环境变量名（可选）", &s.tokenEnv)
		ui.Text(c, "仅填写环境变量名。凭据由启动 Studio 的环境提供。目录名仅允许字母、数字、点、下划线与连字符，不能全为点。")
		if ui.PrimaryButton(c, "下载模型").Disabled(s.busy || !validName(s.modelName) || strings.TrimSpace(s.modelURL) == "").Clicked() {
			s.acquire("model", s.modelURL, s.modelName, s.tokenEnv)
		}
	})
	card(c, "下载引擎（macOS）", func() {
		field(c, "引擎 URL", &s.engineURL)
		field(c, "引擎新目录名", &s.engineName)
		if ui.Button(c, "下载引擎").Disabled(s.busy || !validName(s.engineName) || s.engineURL == "").Clicked() {
			s.acquire("engine", s.engineURL, s.engineName, "")
		}
	})
	card(c, "下载队列", func() {
		ui.Text(c, "1 MiB = 1,048,576 bytes；速率为 bytes/s，总量未知。")
		if len(s.downloads) == 0 {
			ui.Text(c, "暂无下载任务。")
		}
		for _, task := range s.downloads {
			ui.Column(c).Gap(6).Key(task.TaskId).Children(func() {
				ui.Text(c, task.OutName+" · "+stateLabel(task.State)).Bold()
				ui.Text(c, "输出目录："+task.OutDir).Selectable()
				ui.Text(c, "文件："+text(task.FileName))
				ui.Text(c, fmt.Sprintf("已下载 %.2f MiB（%d bytes） · 速率 %s bytes/s", float64(task.DownloadedBytes)/1048576, task.DownloadedBytes, number(task.BytesPerSecond)))
				if task.Result != nil {
					ui.Text(c, fmt.Sprintf("实测大小：%d bytes · SHA-256：%s", task.Result.Bytes, task.Result.Sha256)).Selectable()
				}
				errorView(c, task.Error)
				if task.State == "failed" || task.State == "cancelled" {
					ui.Text(c, "输出目录保留；再次下载请更换目录名。")
				}
				if task.State == "downloading" && ui.Button(c, "取消下载 "+task.OutName).Disabled(s.busy).Clicked() {
					id := task.TaskId
					s.work(func() (string, *core.ApiError) {
						_, e := s.service.CancelDownload(id)
						return "已请求取消下载。", e
					})
				}
			})
		}
	})
}
func (s *viewState) acquire(kind, source, name, env string) {
	if env != "" && !environmentName.MatchString(env) {
		s.failure = &core.ApiError{Code: "invalid_request", Message: "只接受环境变量名，请勿输入明文 token。"}
		return
	}
	root := emptyText(s.settings.Effective.ModelsRoot)
	if kind == "engine" {
		workspace := emptyText(s.settings.Effective.WorkspaceRoot)
		if workspace == "" {
			root = ""
		} else {
			root = filepath.Join(workspace, "engines")
		}
	}
	if root == "" {
		s.failure = core.Conflict("settings_incomplete")
		return
	}
	source = strings.TrimSpace(source)
	if kind == "model" && env == "" {
		s.work(func() (string, *core.ApiError) {
			_, e := s.service.CreateDownload(core.CreateDownloadRequest{SourceUrl: source, OutName: name})
			return "已加入下载队列。", e
		})
		return
	}
	command := "inferyard " + kind + " acquire --source " + quote(source) + " --out " + quote(filepath.Join(root, name))
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
