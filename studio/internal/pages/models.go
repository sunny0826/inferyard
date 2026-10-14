package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
)

var directoryName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validName(s string) bool { return directoryName.MatchString(s) && strings.Trim(s, ".") != "" }
func quote(s string) string   { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func (s *viewState) modelsView(c *ui.Context) {
	ui.Text(c, "工作区 / 资产与服务").FontSize(11).TextColor(c.Theme().TextMuted)
	s.header(c, "模型与下载")
	s.adoptDownloadedEngine()
	ui.Row(c).Gap(20).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Width(0).Grow(1.2).Gap(20).Children(func() {
			card(c, "模型", func() { s.modelAssetsView(c) })
			card(c, "下载", func() {
				s.downloadsView(c)
				card(c, "引擎下载", func() { s.engineDownloadView(c) })
			})
		})
		ui.Column(c).Width(0).Grow(1).MinWidth(310).Gap(20).Children(func() {
			s.modelEngineServiceView(c)
			s.modelBindingView(c)
		})
	})
}

func (s *viewState) modelAssetsView(c *ui.Context) {
	ui.Column(c).Gap(8).Padding(16).Radius(10).Border(1, c.Theme().Accent.Alpha(0.20)).Gradient(c.Theme().Accent.Mix(c.Theme().Surface, 0.92), c.Theme().Surface, 120).Children(func() {
		statusBadge(c, "预置测试模型", c.Theme().Accent)
		ui.Text(c, "SmolLM2-135M").Bold().FontSize(20)
		ui.Text(c, "Instruct · Q4_K_M · 固定版本 GGUF").TextColor(c.Theme().TextMuted)
		ui.Text(c, "适合走通首次使用流程").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.PrimaryButton(c, "下载预置模型").AlignSelf(ui.Start).Disabled(s.busy).OnClick(func() {
			name := fmt.Sprintf("model-%d", uniqueID())
			s.work(func() (string, *core.ApiError) {
				_, e := s.service.CreateDownload(core.CreateDownloadRequest{SourceUrl: core.DefaultModelURL, OutName: name})
				return "已加入下载队列。", e
			})
		})
	})
	snapshot, err := parseDeviceSnapshot(s.device.Snapshot)
	switch {
	case err != nil:
		ui.Text(c, "设备快照无法读取。").TextColor(c.Theme().Danger)
	case snapshot != nil:
		deviceRecommendation(c, snapshot.object("recommendation"))
		deviceModels(c, snapshot.list("models"))
	default:
		ui.Text(c, "尚无设备快照，请到设备页刷新。")
	}
	ui.Text(c, "模型目录："+text(s.settings.Effective.ModelsRoot))

	ui.Text(c, "创建模型下载").Bold()
	field(c, "模型 URL（固定 40 位 revision 的 GGUF）", &s.modelURL)
	field(c, "新目录名", &s.modelName)
	field(c, "凭据环境变量名（可选）", &s.tokenEnv)
	ui.Text(c, "仅填写环境变量名。凭据由启动 Studio 的环境提供。目录名仅允许字母、数字、点、下划线与连字符，不能全为点。")
	if ui.PrimaryButton(c, "下载模型").AlignSelf(ui.Start).Disabled(s.busy || !validName(s.modelName) || strings.TrimSpace(s.modelURL) == "").Clicked() {
		s.acquireModel()
	}
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

func deviceRecommendation(c *ui.Context, rec deviceObject) {
	card(c, "推荐配置", func() {
		selected := rec.object("selected")
		if rec.value("status") == "blocked" || selected == nil {
			ui.Text(c, "状态："+rec.label("status"))
			ui.Text(c, "原因："+rec.label("reason")).TextColor(c.Theme().Warning)
			return
		}
		mode := selected.label("mode")
		if selected["gpu_name"] != nil {
			mode += "（" + selected.value("gpu_name") + "）"
		}
		for _, row := range [][2]string{{"模型", selected.value("model_name")}, {"加速方式", mode}, {"上下文长度", selected.value("context_size")}, {"线程数", selected.value("threads")}, {"所需可用内存", "≥ " + deviceBytes(selected["min_available_memory_bytes"])}, {"所需磁盘空间", "≥ " + deviceBytes(selected["min_disk_bytes"])}, {"推荐依据", selected.label("reason")}} {
			deviceRow(c, row[0], row[1])
		}
		for _, reason := range rec.list("limitations") {
			ui.Text(c, "⚠ "+deviceLabel(fmt.Sprint(reason))).TextColor(c.Theme().Warning)
		}
		ui.Text(c, "模型文件与原始参数").FontSize(12).TextColor(c.Theme().TextMuted)
		deviceMono(c, selected.value("model_path"))
		for _, candidate := range rec.list("candidates") {
			other := deviceItem(candidate)
			if !reflect.DeepEqual(other, selected) {
				ui.Text(c, other.value("model_name")+" · "+other.label("mode"))
			}
		}
	})
}

func deviceModels(c *ui.Context, models []any) {
	card(c, fmt.Sprintf("本地模型 %d 个 GGUF", len(models)), func() {
		for _, item := range models {
			model := deviceItem(item)
			ui.Column(c).Gap(6).Children(func() {
				ui.Row(c).Gap(10).Children(func() {
					name := model.value("name")
					if model["name"] == nil {
						name = filepath.Base(model.value("path"))
					}
					ui.Text(c, name).Bold()
					if model.value("status") == "available" {
						ui.Text(c, deviceBytes(model["size_bytes"])+" · "+model.value("architecture")+" · 上下文 "+model.value("context_length")).TextColor(c.Theme().TextMuted)
						statusBadge(c, "可用", c.Theme().Success)
					} else {
						statusBadge(c, model.label("status"), c.Theme().Warning)
					}
				})
				if model["reason"] != nil {
					ui.Text(c, model.label("reason")).TextColor(c.Theme().Warning)
				}
				deviceMono(c, model.value("path"))
			})
		}
	})
}
