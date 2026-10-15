package pages

import (
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validName(s string) bool { return core.ValidDownloadName(s) }
func quote(s string) string   { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// modelsView is the S8 models page: a selection zone (two searchable
// pickers), a run zone for the selected combination and a read-only services
// card. Downloads live on their own page; no binding UI appears here.
func (s *viewState) modelsView(c *ui.Context) {
	s.header(c, "模型")
	card(c, "选择模型与引擎", func() { s.selectionView(c) })
	if s.model != "" && s.engine != "" {
		card(c, "运行", func() { s.runPanelView(c) })
	}
	card(c, "本机运行的服务", func() {
		s.discoveryView(c, false)
	})
}

// adoptDownloadedEngine picks the first verified engine as the default
// candidate engine. While the CLI is too old to verify, the registry default
// stays selectable (config create is the final gate); an engine that has not
// passed verification is never auto-selected.
func (s *viewState) adoptDownloadedEngine() {
	if s.engine != "" {
		return
	}
	for _, e := range s.registry.Engines {
		if s.checks.Engines[e.Name].State == core.EngineCheckEligible {
			s.engine = e.Name
			return
		}
	}
	if !s.checks.Supported && len(s.registry.Engines) > 0 {
		s.engine = s.registry.DefaultEngine
	}
}
