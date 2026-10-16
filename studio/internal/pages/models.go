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

// modelsView selects a pair for a new task and shows read-only service status.
func (s *viewState) modelsView(c *ui.Context) {
	s.header(c, "模型")
	card(c, "选择模型与引擎", func() { s.selectionView(c) })
	if s.engineState.State == "orphaned" || (s.model != "" && s.engine != "") {
		card(c, "组合状态", func() { s.runPanelView(c) })
	}
	card(c, "本机运行的服务", func() {
		if len(s.registry.Models) == 0 || len(s.registry.Engines) == 0 {
			s.managedServiceView(c)
			return
		}
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
