package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

func (s *viewState) modelEvidenceFor(modelName string) (core.ModelEvidenceSummary, bool) {
	if len(s.modelEvidence) == 0 {
		return core.ModelEvidenceSummary{}, false
	}
	key := core.NormalizeModelEvidenceKey(modelName)
	if key == "" {
		return core.ModelEvidenceSummary{}, false
	}
	summary, ok := s.modelEvidence[key]
	if !ok || summary.Best == nil {
		return core.ModelEvidenceSummary{}, false
	}
	return summary, true
}

func (s *viewState) bestSpeedForService(svc core.EngineState) (*core.EngineSpeedRecord, bool) {
	candidates := make([]string, 0, 2)
	if m := strings.TrimSpace(svc.ModelName); m != "" {
		candidates = append(candidates, m)
	}
	if m := strings.TrimSpace(svc.Model); m != "" {
		candidates = append(candidates, m)
	}
	if len(candidates) == 0 {
		return nil, false
	}
	engine := strings.TrimSpace(svc.Family)
	if engine == "" {
		engine = strings.TrimSpace(svc.EngineName)
	}
	if engine == "" {
		engine = strings.TrimSpace(svc.Adapter)
	}
	wantEngine := core.NormalizeEvidenceEngineKey(engine)
	for _, model := range candidates {
		if summary, ok := s.modelEvidenceFor(model); ok {
			if wantEngine != "" {
				for _, rec := range summary.ByEngine {
					if core.NormalizeEvidenceEngineKey(rec.Engine) == wantEngine {
						cp := rec
						return &cp, true
					}
				}
				return nil, false
			}
			if summary.Best != nil {
				cp := *summary.Best
				return &cp, true
			}
		}
	}
	if s.service != nil && s.preview == nil && s.modelEvidence == nil {
		for _, model := range candidates {
			if rec, ok := s.service.BestSpeedFor(model, engine); ok {
				return rec, true
			}
		}
	}
	return nil, false
}

func (s *viewState) renderModelEvidenceInline(c *ui.Context, m core.ModelAsset) {
	summary, ok := s.modelEvidenceFor(m.Name)
	if !ok || summary.Best == nil {
		inlineBadge(c, tr(c, "models.evidence.untested"), stateNeutral, badgeOptions{Size: sizeSM, Style: badgeOutline})
		return
	}
	best := summary.Best
	label := trf(c, "models.evidence.best",
		"engine", best.Engine,
		"tps", fmt.Sprintf("%.1f", best.TokensPerSecond),
	)
	if len(summary.ByEngine) > 1 {
		second := summary.ByEngine[1]
		label = trf(c, "models.evidence.best_compare",
			"engine", best.Engine,
			"tps", fmt.Sprintf("%.1f", best.TokensPerSecond),
			"otherEngine", second.Engine,
			"otherTps", fmt.Sprintf("%.1f", second.TokensPerSecond),
		)
	}
	btn := inlineButton(c, components.ButtonGhost, label).
		Label(trf(c, "models.evidence.open_report_label", "model", modelLabel(m)))
	if best.ReportRef != "" {
		if btn.Clicked() {
			s.openReport(best.ReportRef)
		}
	}
}

func serviceMemoryDisplay(c *ui.Context, state core.EngineState) string {
	gb, measured, known := core.ServiceMemoryUsage(state)
	if !known {
		return tr(c, "common.missing")
	}
	if measured {
		return trf(c, "services.card.metric.memory_measured", "gb", fmt.Sprintf("%.1f", gb))
	}
	return trf(c, "services.card.metric.memory_estimated", "gb", fmt.Sprintf("%.1f", gb))
}

func serviceModelSpeedTile(c *ui.Context, radius float32, label, modelName, engineTitle string, rec *core.EngineSpeedRecord, onOpenReport func()) {
	if rec == nil {
		serviceMetricTile(c, radius, label, modelName, engineTitle, false)
		return
	}
	t := tokens(c)
	ui.Column(c).FillWidth().Padding(t.Density.Row).Radius(radius).
		Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).Gap(2).Children(func() {
		ui.Text(c, label).FontSize(t.Type.ShellLabel).TextColor(t.Color.FgSecondary).SingleLine()
		ui.Text(c, modelName).Bold().FontSize(t.Type.Section).TextColor(t.Color.FgPrimary).SingleLine()
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			speedText := trf(c, "services.card.metric.speed_value", "tps", fmt.Sprintf("%.1f", rec.TokensPerSecond))
			ui.Text(c, speedText).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(stateColor(c, stateSuccess)).SingleLine()
			if rec.ReportRef != "" && onOpenReport != nil {
				link := inlineButton(c, components.ButtonGhost, tr(c, "services.card.metric.view_report")).
					Label(trf(c, "services.card.metric.view_report_label", "model", modelName))
				if link.Clicked() {
					onOpenReport()
				}
			}
		})
	})
}

func exclusivityBadge(c *ui.Context, status string) {
	switch strings.TrimSpace(status) {
	case core.ExclusivityExclusive:
		inlineBadge(c, tr(c, "reports.exclusivity.exclusive"), stateSuccess, badgeOptions{Size: sizeSM})
	case core.ExclusivityExternal:
		inlineBadge(c, tr(c, "reports.exclusivity.external"), stateWarning, badgeOptions{Size: sizeSM})
	default:
		inlineBadge(c, tr(c, "reports.exclusivity.unknown"), stateNeutral, badgeOptions{Size: sizeSM})
	}
}

func (s *viewState) reportExclusivity(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return core.ExclusivityUnknown
	}
	if s.reportExclusivities != nil {
		if status, ok := s.reportExclusivities[ref]; ok && strings.TrimSpace(status) != "" {
			return status
		}
	}
	if s.preview != nil {
		if pm, ok := s.preview.(previewExclusivityMode); ok {
			if status := pm.reportExclusivity(ref); strings.TrimSpace(status) != "" {
				return status
			}
		}
	}
	return core.ExclusivityUnknown
}

func (s *viewState) renderEngineUpdateBadgeAndAction(c *ui.Context, eng core.EngineAsset) {
	upd, ok := s.engineUpdates[eng.Name]
	if !ok || !upd.HasUpdate || strings.TrimSpace(upd.LatestVersion) == "" {
		return
	}
	inlineBadge(c, trf(c, "models.engine.update_available", "version", upd.LatestVersion), stateInfo, badgeOptions{Size: sizeSM})
	btn := inlineButton(c, components.ButtonOutline, tr(c, "models.engine.action.update")).
		Label(trf(c, "models.engine.action.update_label", "name", eng.Name)).
		Disabled(s.busy)
	if btn.Clicked() {
		s.triggerEngineUpdate(eng, upd)
	}
}

func pickUpdateCatalogRelease(upd core.EngineUpdateInfo, preferred, all []core.EngineRelease) (core.EngineRelease, bool) {
	wantID := strings.TrimSpace(upd.ReleaseID)
	wantVer := strings.TrimSpace(upd.ReleaseVersion)
	if wantVer == "" {
		wantVer = strings.TrimSpace(upd.LatestVersion)
	}
	for _, list := range [][]core.EngineRelease{preferred, all} {
		if wantID != "" {
			for _, rel := range list {
				if rel.ID == wantID {
					return rel, true
				}
			}
		}
		if wantVer != "" {
			for _, rel := range list {
				if strings.EqualFold(strings.TrimSpace(rel.Version), wantVer) {
					return rel, true
				}
			}
		}
	}
	return core.EngineRelease{}, false
}

func (s *viewState) triggerEngineUpdate(eng core.EngineAsset, upd core.EngineUpdateInfo) {
	familyID := strings.TrimSpace(eng.Family)
	if familyID == "" {
		lower := strings.ToLower(eng.Name)
		switch {
		case strings.Contains(lower, "polystrata"):
			familyID = "polystrata"
		case strings.Contains(lower, "llama"):
			familyID = "llama-cpp"
		case strings.Contains(lower, "mlx"):
			familyID = "mlx-lm"
		case strings.Contains(lower, "vllm"):
			familyID = "vllm"
		case strings.Contains(lower, "sglang"):
			familyID = "sglang"
		case strings.Contains(lower, "lmstudio"):
			familyID = "lmstudio"
		}
	}
	fam, ok := s.engineCatalogRegistry().FamilyByID(familyID)
	if !ok {
		s.openLocalEngineWizard(familyID)
		return
	}
	st := s.ensureDownloadsUI()
	hostPlatform := s.downloadsHostPlatform()
	hostReleases := fam.ReleasesForPlatform(hostPlatform, false)
	if len(hostReleases) == 0 {
		hostReleases = fam.Releases
	}
	if len(hostReleases) == 0 {
		s.openLocalEngineWizard(familyID)
		return
	}
	rel, matched := pickUpdateCatalogRelease(upd, hostReleases, fam.Releases)
	if !matched {
		rel = resolveEngineRelease(st.selectedRelease[fam.ID], hostReleases)
	}
	if rel.Channel != core.ReleaseStable && rel.Channel != core.ReleaseLatest {
		st.drawerStableOnly = false
	}
	st.selectedRelease[fam.ID] = rel.ID
	st.selectedBuild[fam.ID] = ""
	build := pickEngineBuildForPlatform(st, fam.ID, rel, hostPlatform)
	if fam.ID == "polystrata" {
		s.openPolystrataConfigDrawer(fam.ID, rel.ID, build.Platform, build.ID)
		return
	}
	st.openEngineDrawer(fam.ID, rel.ID, build.Platform, build.ID)
}
