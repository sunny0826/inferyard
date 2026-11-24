package pages

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

type polystrataSelectionTarget struct {
	installationPath string
	modelConfigPath  string
	initialEngine    string
	initialModel     string
}

func cleanMatchPath(p string) string {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}

var polystrataPhaseLabelKeys = []string{
	"downloads.polystrata.phase.preflight",
	"downloads.polystrata.phase.runtime",
	"downloads.polystrata.phase.setup",
	"downloads.polystrata.phase.model_verify",
}

func polystrataPhaseLabels(c *ui.Context) []string {
	out := make([]string, len(polystrataPhaseLabelKeys))
	for i, key := range polystrataPhaseLabelKeys {
		out[i] = tr(c, key)
	}
	return out
}

func polystrataPhaseIndex(task core.DownloadTask) int {
	if task.State == "done" {
		return len(polystrataPhaseLabelKeys)
	}
	if task.Step != nil && (task.StepTotal == nil || *task.StepTotal == len(polystrataPhaseLabelKeys)) &&
		*task.Step >= 1 && *task.Step <= len(polystrataPhaseLabelKeys) {
		return *task.Step - 1
	}
	switch task.Stage {
	case "preflight":
		return 0
	case "downloading_runtime", "extracting":
		return 1
	case "setup_engine":
		return 2
	case "downloading_model", "verifying", "verifying_installation":
		return 3
	default:
		if task.Step != nil && *task.Step > 0 {
			return min(max(*task.Step-1, 0), len(polystrataPhaseLabelKeys)-1)
		}
		return 0
	}
}

func polystrataStateLabel(state string) string {
	if state == "downloading" {
		return l10n.TR("downloads.badge.installing")
	}
	return stateLabel(state)
}

func blockerBytesPtr(v any) *int64 {
	switch n := v.(type) {
	case int64:
		if n >= 0 {
			val := n
			return &val
		}
	case int:
		if n >= 0 {
			val := int64(n)
			return &val
		}
	case float64:
		if !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n <= math.MaxInt64 {
			val := int64(n)
			return &val
		}
	case json.Number:
		if val, err := n.Int64(); err == nil && val >= 0 {
			return &val
		}
		if f, err := n.Float64(); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) && f >= 0 && f <= math.MaxInt64 {
			val := int64(f)
			return &val
		}
	}
	return nil
}

func preflightBlockerBaseText(item preflightBlockerItem) string {
	if item.code != "" {
		if msg, ok := humanKnown(item.code); ok {
			return msg
		}
		if label := deviceLabel(item.code); label != "" && label != item.code {
			return label
		}
	}
	if item.message != "" {
		return item.message
	}
	return item.code
}

func formatPreflightBlockerText(item preflightBlockerItem) string {
	base := strings.TrimSpace(preflightBlockerBaseText(item))
	if item.requiredBytes == nil && item.freeBytes == nil {
		return base
	}
	prefix := strings.TrimSuffix(base, ".")
	prefix = strings.TrimSuffix(prefix, l10n.T(l10n.LanguageZhHans, "downloads.punctuation.period"))
	switch {
	case item.freeBytes != nil && item.requiredBytes != nil:
		return l10n.TRf("downloads.blocker.disk_free_and_required", "prefix", prefix, "free", formatDownloadBytes(*item.freeBytes), "required", formatDownloadBytes(*item.requiredBytes))
	case item.requiredBytes != nil:
		return l10n.TRf("downloads.blocker.disk_required", "prefix", prefix, "required", formatDownloadBytes(*item.requiredBytes))
	default:
		return l10n.TRf("downloads.blocker.disk_free", "prefix", prefix, "free", formatDownloadBytes(*item.freeBytes))
	}
}

func (s *viewState) polystrataDownloadTaskCard(c *ui.Context, task core.DownloadTask) {
	t := tokens(c)
	stageText := downloadStageLabel(task.Stage)
	if stageText == "" {
		stageText = polystrataStateLabel(task.State)
	}
	stepVal, stepSub := stageText, ""
	if task.Step != nil && task.StepTotal != nil && *task.StepTotal > 0 {
		stepVal = fmt.Sprintf("%d / %d", *task.Step, *task.StepTotal)
		stepSub = stageText
	}
	hasByteProgress := task.TotalBytes != nil && *task.TotalBytes > 0
	hasPercent := task.Percent != nil
	progressVal, progressSub := stageText, ""
	switch {
	case hasByteProgress:
		pct := progressValue(task.DownloadedBytes, task.TotalBytes) * 100
		if hasPercent {
			pct = math.Max(0, math.Min(100, *task.Percent))
		} else if pct < 0 {
			pct = 0
		}
		progressVal = fmt.Sprintf("%s / %s", formatDownloadBytes(task.DownloadedBytes), formatDownloadBytes(*task.TotalBytes))
		progressSub = fmt.Sprintf("%.1f%%", pct)
	case hasPercent:
		progressVal = fmt.Sprintf("%.0f%%", math.Max(0, math.Min(100, *task.Percent)))
	case task.DownloadedBytes > 0:
		progressVal = formatDownloadBytes(task.DownloadedBytes)
	}
	metrics := []taskMetric{
		{tr(c, "downloads.metric.step"), stepVal, stepSub},
		{tr(c, "downloads.metric.progress"), progressVal, progressSub},
	}
	if task.BytesPerSecond != nil {
		rateSub := ""
		if task.State == "downloading" && hasByteProgress && *task.BytesPerSecond > 0 && task.DownloadedBytes < *task.TotalBytes {
			rateSub = trf(c, "downloads.eta.remaining", "eta", formatDownloadETA(float64(*task.TotalBytes-task.DownloadedBytes)/(*task.BytesPerSecond)))
		}
		metrics = append(metrics, taskMetric{tr(c, "downloads.metric.rate"), formatRate(*task.BytesPerSecond), rateSub})
	}

	active := task.State == "downloading" && !hasByteProgress && !hasPercent
	color := downloadStateColor(c, task.State)
	pendingHere := s.pendingDestructive.kind == string(destructiveCancelDownload) && s.pendingDestructive.id == task.TaskId
	var headerRight func()
	if task.State == "downloading" || pendingHere {
		headerRight = func() { s.polystrataInstallHeaderAction(c, task) }
	}
	var actions func()
	if task.State == "done" || task.State == "failed" || task.State == "cancelled" {
		actions = func() { s.polystrataDownloadActions(c, task) }
	}
	ui.Column(c.Key(task.TaskId)).Children(func() {
		taskCard(c, taskCardOptions{
			kind: tr(c, "downloads.polystrata.task_kind"), name: downloadDisplayName(task), state: polystrataStateLabel(task.State), color: color,
			active:      active,
			metrics:     metrics,
			headerRight: headerRight,
			notice: func() {
				if task.State == "downloading" {
					phaseProgress(c, phaseProgressOptions{
						Labels:   polystrataPhaseLabels(c),
						Current:  polystrataPhaseIndex(task),
						RowLabel: tr(c, "downloads.polystrata.phase_row_label"),
						HideBar:  true,
					})
					if hasPercent {
						ratio := math.Max(0, math.Min(1, *task.Percent/100))
						barLabel := tr(c, "downloads.progress.install_label")
						if hasByteProgress {
							barLabel = tr(c, "downloads.progress.download_label")
						}
						progressBar(c, ratio, barLabel).Height(t.Density.Tight)
					} else if hasByteProgress {
						progressBar(c, progressValue(task.DownloadedBytes, task.TotalBytes), tr(c, "downloads.progress.download_label")).Height(t.Density.Tight)
					}
				}
				if task.Detail != nil && strings.TrimSpace(*task.Detail) != "" {
					ui.Text(c, strings.TrimSpace(*task.Detail)).SingleLine().FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				}
				if task.LogPath != nil && strings.TrimSpace(*task.LogPath) != "" {
					s.polystrataInstallLogSection(c, task.TaskId, strings.TrimSpace(*task.LogPath))
				}
				s.downloadErrorView(c, task.Error)
				s.polystrataRuntimeProxyGuidance(c, task)
			},
			actions: actions,
		})
	})
}

func (s *viewState) polystrataDownloadActions(c *ui.Context, task core.DownloadTask) {
	t := tokens(c)
	if task.State == "done" {
		if primaryButton(c, tr(c, "downloads.polystrata.go_benchmark")).Disabled(s.busy).Clicked() {
			s.startBenchmarkFromPolystrataTask(task)
		}
		return
	}
	if task.State != "failed" && task.State != "cancelled" {
		return
	}
	retryDisabled := s.busy || s.downloadRetryActive(task)
	retryLabel := tr(c, "downloads.polystrata.retry_install")
	retryDoneKey := "downloads.polystrata.toast.retried"
	if task.State == "cancelled" {
		retryLabel = tr(c, "downloads.polystrata.resume_install")
		retryDoneKey = "downloads.polystrata.toast.resumed"
	}
	ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
		if primaryButton(c, retryLabel).Disabled(retryDisabled).Clicked() {
			if s.simulate(previewRetryDownload{id: task.TaskId}) {
				return
			}
			s.workDownload(func() (string, *core.ApiError) {
				_, e := s.service.RetryDownload(task.TaskId)
				return l10n.TR(retryDoneKey), e
			})
		}
		if outlineButton(c, tr(c, "downloads.polystrata.change_option")).Disabled(s.busy).Clicked() {
			s.openPolystrataRecoveryDrawer(task)
		}
	})
}

func (s *viewState) startBenchmarkFromPolystrataTask(task core.DownloadTask) {
	run := func() {
		wantInst := cleanMatchPath(emptyText(task.InstallationPath))
		if wantInst == "" {
			wantInst = cleanMatchPath(emptyText(task.EnginePath))
		}
		wantModel := cleanMatchPath(emptyText(task.ModelConfigPath))
		engineMatched, modelMatched := s.applyPolystrataSelection(wantInst, wantModel)
		if (!engineMatched || !modelMatched) && s.service != nil && s.preview == nil {
			s.pendingPolystrataSelection = &polystrataSelectionTarget{
				installationPath: wantInst,
				modelConfigPath:  wantModel,
				initialEngine:    s.engine,
				initialModel:     s.model,
			}
			go s.refresh(true)
		} else {
			s.pendingPolystrataSelection = nil
		}
		if !s.roundWorkActive() && s.running() == nil {
			s.clearBenchmark()
			if s.speed != nil {
				s.speed.current, s.speed.opened = nil, nil
				s.speed.selected = false
			}
			s.task.stage = "prepare_candidate"
			s.focusedRun = ""
			s.resumeSource = nil
			s.detailMu.Lock()
			s.requestedDetail = ""
			s.detailMu.Unlock()
			s.failure = nil
		}
		s.navigateToNow("dashboard", "")
	}
	if s.guardLeaveSettings("dashboard", run) {
		return
	}
	run()
}

func (s *viewState) applyPendingPolystrataSelection(prevEngineName, prevModelName string) bool {
	pending := s.pendingPolystrataSelection
	if pending == nil {
		return false
	}
	if prevEngineName != pending.initialEngine || prevModelName != pending.initialModel {
		s.pendingPolystrataSelection = nil
		return false
	}
	engineMatched, modelMatched := s.applyPolystrataSelection(pending.installationPath, pending.modelConfigPath)
	if engineMatched && modelMatched {
		s.pendingPolystrataSelection = nil
	} else {
		pending.initialEngine = s.engine
		pending.initialModel = s.model
	}
	return true
}

func (s *viewState) applyPolystrataSelection(wantInst, wantModel string) (bool, bool) {
	engineMatched := wantInst == ""
	modelMatched := wantModel == ""

	selectedEngine := ""
	fallbackEligible := ""
	fallbackPoly := ""
	for _, eng := range s.registry.Engines {
		if wantInst != "" && cleanMatchPath(eng.Path) == wantInst {
			selectedEngine = eng.Name
			engineMatched = true
			break
		}
		if eng.Family == "polystrata" {
			if s.eligibility(eng.Name).State == core.EngineCheckEligible && fallbackEligible == "" {
				fallbackEligible = eng.Name
			} else if fallbackPoly == "" {
				fallbackPoly = eng.Name
			}
		}
	}
	if selectedEngine == "" {
		if fallbackEligible != "" {
			selectedEngine = fallbackEligible
		} else if fallbackPoly != "" {
			selectedEngine = fallbackPoly
		}
	}
	if selectedEngine != "" {
		s.engine = selectedEngine
	}

	selectedModel := ""
	fallbackPack := ""
	for _, m := range s.registry.Models {
		if !m.Available {
			continue
		}
		if wantModel != "" && cleanMatchPath(m.Path) == wantModel {
			selectedModel = m.Name
			modelMatched = true
			break
		}
		if m.Format == "strata_pack" && fallbackPack == "" {
			fallbackPack = m.Name
		}
	}
	if selectedModel == "" && wantModel == "" && fallbackPack != "" {
		selectedModel = fallbackPack
		modelMatched = true
	}
	if selectedModel != "" {
		s.model = selectedModel
	}
	return engineMatched, modelMatched
}

func (s *viewState) openPolystrataRecoveryDrawer(task core.DownloadTask) {
	fam, ok := s.engineCatalogRegistry().FamilyByID("polystrata")
	if !ok || len(fam.Releases) == 0 {
		return
	}
	st := s.ensureDownloadsUI()
	if src := strings.TrimSpace(task.ModelSource); src != "" {
		st.setPolystrataModelSource(src)
	}
	hostPlatform := s.downloadsHostPlatform()
	releases := fam.ReleasesForPlatform(hostPlatform, false)
	if len(releases) == 0 {
		releases = fam.Releases
	}
	rel := resolveEngineRelease(st.selectedRelease[fam.ID], releases)
	build := pickEngineBuildForPlatform(st, fam.ID, rel, hostPlatform)

	parts := strings.Split(task.DisplayName, " · ")
	if len(parts) >= 2 && strings.TrimSpace(parts[0]) == "PolyStrata" {
		wantFamily := strings.TrimSpace(parts[1])
		wantModel := ""
		if len(parts) >= 3 {
			wantModel = strings.TrimSpace(parts[2])
		}
		for _, opt := range s.polystrataDrawerOptions(hostPlatform) {
			if strings.EqualFold(opt.Family, wantFamily) && (wantModel == "" || strings.EqualFold(opt.Size, wantModel)) {
				st.polystrataDrawerModelID = opt.ModelID
				break
			}
		}
	}
	s.openPolystrataConfigDrawer(fam.ID, rel.ID, build.Platform, build.ID)
}
