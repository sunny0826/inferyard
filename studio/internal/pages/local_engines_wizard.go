package pages

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type localEngineWizardState struct {
	family          string
	executable      string
	workingDir      string
	modelsRoot      string
	dataDir         string
	runtimeRoots    []string
	candidateFamily string
	candidateExec   string
	detecting       bool
	detectedLoaded  bool
	detectError     *core.ApiError
	candidates      []inferyard.DetectedEngine
}

var localEngineFamilyOptions = []components.NativeSelectOption[string]{
	{Value: "polystrata", Label: "polystrata"},
	{Value: "llama-cpp", Label: "llama-cpp"},
	{Value: "mlx-lm", Label: "mlx-lm"},
	{Value: "vllm", Label: "vllm"},
	{Value: "sglang", Label: "sglang"},
	{Value: "lmstudio", Label: "lmstudio"},
}

func (s *viewState) localEngineWizardView(c *ui.Context) {
	t := tokens(c)
	container := ui.Column(c.Key("local-engine-wizard")).FillWidth().Gap(t.Density.CardGap)
	w := ui.Local(container, "wizard-state", func() localEngineWizardState {
		return localEngineWizardState{family: "llama-cpp"}
	})
	if s.wizardPresetFamily != "" {
		for _, opt := range localEngineFamilyOptions {
			if opt.Value == s.wizardPresetFamily {
				w.family = s.wizardPresetFamily
				break
			}
		}
		s.wizardPresetFamily = ""
	}
	if w.family == "" {
		w.family = "llama-cpp"
	}
	container.Children(func() {
		section(c, "添加本地引擎", func() {
			segmentedField(c, "引擎类型", "选择要添加的本地引擎类型。", &w.family, localEngineFamilyOptions)
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				if secondaryActionButton(c, "自动探测本机引擎", w.detecting).Disabled(s.busy || w.detecting).Clicked() {
					s.detectWizardEngines(w)
				}
			})
			if w.detectError != nil {
				errorView(c, w.detectError)
			} else if w.detectedLoaded && len(w.candidates) == 0 {
				ui.Text(c, "未在本机常用路径探测到引擎，可直接选择路径。").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			}
			if len(w.candidates) > 0 {
				ui.Text(c, "已探测到的本机引擎（点击自动填入）：").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					for _, cand := range w.candidates {
						cand := cand
						selected := w.family == cand.Family && strings.TrimSpace(w.executable) == cand.Executable
						var b ui.Element
						if selected {
							b = primaryButton(c, cand.Label)
						} else {
							b = outlineButton(c, cand.Label)
						}
						if b.Disabled(s.busy).Clicked() {
							applyDetectedEngine(w, cand)
						}
					}
				})
			}
			execLabel, execDesc := wizardExecutableLabels(w.family)
			s.pathField(c, execLabel, &w.executable, browseExecutable).
				Description(execDesc).
				Error(s.browseError(browseExecutable.key, pathError(w.executable)))
			if w.family == "lmstudio" {
				s.pathField(c, "LM Studio 模型目录", &w.modelsRoot, browseModelsRootDir).
					Description("填写或选择 LM Studio 本地模型目录绝对路径。").
					Error(s.browseError(browseModelsRootDir.key, pathError(w.modelsRoot)))
			}
			if w.family == "polystrata" {
				monoField(c, "PolyStrata 数据目录（可选）", "模型与 strata-*.json 配置目录（data_dir），留空则由安装目录自动解析。", "", pathError(w.dataDir), &w.dataDir)
			}
			monoField(c, "工作目录（可选）", "留空则默认使用可执行文件所在目录。", "", pathError(w.workingDir), &w.workingDir)
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				if wizardActionButton(c, "生成并注册引擎", "注册引擎", s.actionBusy("install-init")).Disabled(s.busy || !wizardCanSubmit(w)).Clicked() {
					s.initAndRegisterWizardEngine(w)
				}
				if secondaryActionButton(c, "重新核验引擎", s.actionBusy("install-recheck")).Disabled(s.busy).Clicked() {
					s.changeInstallation("recheck", "")
				}
			})
		})
	})
}

func wizardActionButton(c *ui.Context, visibleLabel, accessibleLabel string, active bool) ui.Element {
	theme := c.Theme()
	b := components.Button(c, "", components.ButtonPrimary).MaxWidthPercent(100).AlignSelf(ui.Start).Label(accessibleLabel)
	showSpinner := delayedVisible(c, b, "btn-spinner", active)
	b.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(theme.Space(1.5)).Children(func() {
			if showSpinner {
				components.Spinner(c, components.SpinnerRing).Size(14, 14).Label(visibleLabel + "处理中")
			}
			ui.Text(c, visibleLabel).SingleLine()
		})
	})
	return b
}

func wizardExecutableLabels(family string) (string, string) {
	switch family {
	case "polystrata":
		return "PolyStrata Python 路径", "选择 PolyStrata 虚拟环境中的 python 解释器（.venv/bin/python）。"
	case "mlx-lm", "vllm", "sglang":
		return "虚拟环境 Python 路径", "选择独立虚拟环境中的 python 解释器（venv/bin/python）。"
	case "lmstudio":
		return "LM Studio CLI (lms) 路径", "选择 lms 可执行文件绝对路径。"
	default:
		return "引擎可执行文件路径", "选择 llama-server 可执行文件绝对路径。"
	}
}

func wizardCanSubmit(w *localEngineWizardState) bool {
	exe := strings.TrimSpace(w.executable)
	if exe == "" || pathError(exe) != "" {
		return false
	}
	if strings.TrimSpace(w.workingDir) != "" && pathError(strings.TrimSpace(w.workingDir)) != "" {
		return false
	}
	if w.family == "polystrata" && strings.TrimSpace(w.dataDir) != "" && pathError(strings.TrimSpace(w.dataDir)) != "" {
		return false
	}
	if w.family == "lmstudio" {
		modelsRoot := strings.TrimSpace(w.modelsRoot)
		if modelsRoot == "" || pathError(modelsRoot) != "" {
			return false
		}
	}
	return true
}

func applyDetectedEngine(w *localEngineWizardState, cand inferyard.DetectedEngine) {
	w.family = cand.Family
	w.executable = cand.Executable
	w.workingDir = cand.WorkingDirectory
	w.runtimeRoots = slices.Clone(cand.RuntimeRoots)
	w.candidateFamily = cand.Family
	w.candidateExec = cand.Executable
	if cand.ModelsRoot != nil {
		w.modelsRoot = *cand.ModelsRoot
	} else {
		w.modelsRoot = ""
	}
	if cand.DataDir != nil {
		w.dataDir = *cand.DataDir
	} else {
		w.dataDir = ""
	}
}

func (s *viewState) detectWizardEngines(w *localEngineWizardState) {
	if s.preview != nil {
		w.candidates = s.preview.detectedEngines()
		w.detectedLoaded = true
		w.detectError = nil
		w.detecting = false
		s.enqueueStatusToast("已模拟探测本机引擎（仅预览会话）")
		return
	}
	if s.service == nil || w.detecting {
		return
	}
	w.detecting = true
	w.detectError = nil
	apply := func(res inferyard.EngineDetectResult, err *core.ApiError) {
		w.detecting = false
		w.detectedLoaded = true
		w.detectError = err
		if err == nil {
			w.candidates = res.Candidates
		} else {
			w.candidates = nil
		}
	}
	update := s.updateWindow
	if update == nil && s.win != nil {
		update = s.win.Update
	}
	if update == nil {
		ctx := s.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		res, err := s.service.DetectLocalEngines(ctx)
		apply(res, err)
		return
	}
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		res, err := s.service.DetectLocalEngines(ctx)
		if ctx.Err() != nil {
			return
		}
		update(func() { apply(res, err) })
	}()
}

func (s *viewState) initAndRegisterWizardEngine(w *localEngineWizardState) {
	if !wizardCanSubmit(w) {
		return
	}
	if s.preview != nil {
		slug := strings.TrimSpace(filepath.Base(strings.TrimSpace(w.executable)))
		if slug == "" || slug == "." || slug == string(filepath.Separator) {
			slug = "wizard"
		}
		path := "/preview/" + w.family + "-" + slug + ".json"
		if s.simulate(previewInstallation{action: "register", path: path}) && s.failure == nil {
			s.enqueueStatusToast("已模拟生成并注册本地引擎")
		}
		return
	}
	if s.service == nil {
		return
	}
	req := core.InitLocalEngineRequest{
		Family:           w.family,
		Executable:       strings.TrimSpace(w.executable),
		WorkingDirectory: strings.TrimSpace(w.workingDir),
	}
	if w.family == "lmstudio" {
		req.ModelsRoot = strings.TrimSpace(w.modelsRoot)
	}
	if w.family == "polystrata" {
		req.DataDir = strings.TrimSpace(w.dataDir)
	}
	if w.family == w.candidateFamily && strings.TrimSpace(w.executable) == w.candidateExec {
		req.RuntimeRoots = slices.Clone(w.runtimeRoots)
	}
	s.workAs("install-init", func() (string, *core.ApiError) {
		_, err := s.service.InitAndRegisterLocalEngine(s.ctx, req)
		return "已生成并注册本地引擎", err
	})
}
