package pages

import (
	"net/url"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

type downloadsUIState struct {
	activeTab               int
	hostPlatform            string
	selectedRelease         map[string]string
	selectedPlatform        map[string]string
	selectedBuild           map[string]string
	drawerStableOnly        bool
	engineDrawerOpen        bool
	engineDrawerFamilyID    string
	polystrataDrawerModelID string
	modelSource             string
	polystrataModelSource   string
	starterChoice           int
	starterAltOpen          bool
	starterExpandedOverride *bool
	modelDrawerOpen         bool
	modelDrawerID           string
	customEngineURL         string
	customModelURL          string
	customModelResolve      *modelResolveState
}

func (s *viewState) ensureDownloadsUI() *downloadsUIState {
	if s.downloadsUI == nil {
		s.downloadsUI = &downloadsUIState{modelSource: "huggingface", polystrataModelSource: "auto"}
	}
	st := s.downloadsUI
	if st.selectedRelease == nil {
		st.selectedRelease = map[string]string{}
	}
	if st.selectedPlatform == nil {
		st.selectedPlatform = map[string]string{}
	}
	if st.selectedBuild == nil {
		st.selectedBuild = map[string]string{}
	}
	if st.modelSource != "modelscope" {
		st.modelSource = "huggingface"
	}
	if st.polystrataModelSource != "modelscope" && st.polystrataModelSource != "huggingface" {
		st.polystrataModelSource = "auto"
	}
	if st.customModelResolve == nil {
		st.customModelResolve = &modelResolveState{mailbox: &modelResolveMailbox{}}
	} else if st.customModelResolve.mailbox == nil {
		st.customModelResolve.mailbox = &modelResolveMailbox{}
	}
	return st
}

func (s *viewState) downloadsHostPlatform() string {
	st := s.ensureDownloadsUI()
	if strings.TrimSpace(st.hostPlatform) != "" {
		return st.hostPlatform
	}
	return core.CurrentPlatform()
}

func (s *viewState) engineCatalogRegistry() *core.EngineCatalogRegistry {
	if s.engineCatalog != nil {
		return s.engineCatalog
	}
	return core.DefaultEngineCatalog()
}

func (st *downloadsUIState) openEngineDrawer(familyID, releaseID, platform, buildID string) {
	st.modelDrawerOpen = false
	st.engineDrawerOpen = true
	st.engineDrawerFamilyID = familyID
	if releaseID != "" {
		st.selectedRelease[familyID] = releaseID
	}
	if platform != "" {
		st.selectedPlatform[familyID] = platform
	}
	if buildID != "" {
		st.selectedBuild[familyID] = buildID
	}
}

func (st *downloadsUIState) openModelDrawer(entryID string) {
	st.engineDrawerOpen = false
	st.modelDrawerOpen = true
	st.modelDrawerID = entryID
}

func (st *downloadsUIState) setDirectModelSource(source string) {
	if source != "modelscope" {
		source = "huggingface"
	}
	st.modelSource = source
	st.polystrataModelSource = source
}

func (st *downloadsUIState) setPolystrataModelSource(source string) {
	if source != "modelscope" && source != "huggingface" {
		source = "auto"
	}
	st.polystrataModelSource = source
	if source == "modelscope" || source == "huggingface" {
		st.modelSource = source
	}
}

func (s *viewState) downloadsPageView(c *ui.Context) {
	st := s.ensureDownloadsUI()
	t := tokens(c)

	ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Children(func() { s.header(c, tr(c, "downloads.page.title")) })
		button(c, tr(c, "downloads.page.settings")).OnClick(func() { s.selected = "settings" })
	})
	s.downloadTaskGuide(c)
	s.downloadsStarterCard(c)
	if len(s.downloads) > 0 {
		card(c, tr(c, "downloads.section.tasks"), func() { s.downloadsView(c) })
	}
	ui.Column(c.Key("downloads-tabs")).FillWidth().Gap(t.Density.Row).Children(func() {
		components.Tabs(c, &st.activeTab, components.TabsUnderline, tr(c, "downloads.kind.engine"), tr(c, "downloads.kind.model"))
		if st.activeTab == 1 {
			s.downloadsModelsTab(c)
		} else {
			s.downloadsEnginesTab(c)
		}
	})
	s.downloadsEngineDrawer(c)
	s.downloadsModelDrawer(c)
}

func (s *viewState) downloadPrimaryKind() string {
	assets := s.availableTaskAssets()
	if !assets.engine && !s.downloadActive("engine") && !s.downloadActive("polystrata") {
		return "engine"
	}
	if !assets.model && !s.downloadActive("model") && !s.downloadActive("polystrata") {
		return "model"
	}
	return ""
}

func (s *viewState) downloadActive(kind string) bool {
	for _, task := range s.downloads {
		if task.Kind == kind && task.State == "downloading" {
			return true
		}
	}
	return false
}

func (s *viewState) downloadAction(c *ui.Context, kind, label string) ui.Element {
	if kind == "engine" && s.starterCardVisible() {
		return button(c, label)
	}
	if s.downloadPrimaryKind() == kind {
		return primaryButton(c, label)
	}
	return button(c, label)
}

func (s *viewState) workDownload(fn func() (string, *core.ApiError)) {
	if s.previewAction() || s.busy {
		return
	}
	s.busy = true
	s.busyAction = ""
	s.failure = nil
	s.pendingToast = ""
	go func() {
		message, e := fn()
		if s.ctx != nil && s.ctx.Err() != nil {
			return
		}
		var downloads []core.DownloadTask
		if s.service != nil {
			downloads = s.service.Downloads()
		}
		apply := func() {
			if downloads != nil {
				s.downloads = downloads
			}
			s.finishWork(message, e)
		}
		if s.updateWindow != nil {
			s.updateWindow(apply)
		} else if s.win != nil {
			s.win.Update(apply)
		} else {
			return
		}
		if s.win != nil {
			s.refresh(true)
		}
	}()
}

func (s *viewState) acquireModelSource(source, tokenEnv string) {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" || !core.IsSupportedModelURL(trimmed) {
		return
	}
	if tokenEnv != "" && !environmentName.MatchString(tokenEnv) {
		return
	}
	if s.simulate(previewDownload{kind: "model", source: trimmed}) {
		return
	}
	s.workDownload(func() (string, *core.ApiError) {
		_, e := s.service.CreateOrResolveModelDownload(s.ctx, core.CreateDownloadRequest{SourceUrl: trimmed}, tokenEnv)
		return l10n.TR("downloads.toast.queued"), e
	})
}

func (s *viewState) acquireEngine(source, name string) {
	if downloadURLError(source) != "" {
		return
	}
	if s.simulate(previewDownload{kind: "engine", source: source, name: name}) {
		return
	}
	s.workDownload(func() (string, *core.ApiError) {
		_, e := s.service.CreateEngineDownload(source, name)
		return l10n.TR("downloads.toast.queued"), e
	})
}

func (s *viewState) copyDownloadText(c *ui.Context, label, value string) {
	if value == "" {
		return
	}
	if s.preview != nil {
		s.enqueueStatusToast(trf(c, "downloads.toast.copied_preview", "label", label))
		return
	}
	c.WriteClipboardThen(value, func(err error) {
		if err != nil {
			s.enqueueStatusToast(l10n.TRf("downloads.toast.copy_failed", "label", label))
			return
		}
		s.enqueueStatusToast(l10n.TRf("downloads.toast.copied", "label", label))
	})
}

func (s *viewState) openDownloadURL(c *ui.Context, label, rawURL string) {
	if !strings.HasPrefix(rawURL, "https://") {
		return
	}
	if s.preview != nil {
		s.enqueueStatusToast(trf(c, "downloads.toast.opened_url_preview", "label", label))
		return
	}
	c.OpenURLThen(rawURL, func(err error) {
		if err != nil {
			s.enqueueStatusToast(l10n.TR("downloads.toast.open_url_failed"))
			return
		}
		s.enqueueStatusToast(l10n.TRf("downloads.toast.opened_url", "label", label))
	})
}

func downloadURLError(value string) string {
	if value == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return l10n.TR("downloads.validation.https_url")
	}
	return ""
}
