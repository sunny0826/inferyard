package pages

import (
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

type pendingServicePrep struct {
	model       string
	engine      string
	contextSize int64
}

func (s *viewState) pendingStandalonePreparations() []pendingServicePrep {
	if len(s.standalone.preparing) == 0 {
		return nil
	}
	services := s.effectiveManagedServices()
	var out []pendingServicePrep
	for model, active := range s.standalone.preparing {
		if !active || strings.TrimSpace(model) == "" {
			continue
		}
		modelAsset, hasModel := s.findModelAsset(model)
		alreadyShown := false
		for _, st := range services {
			if (st.ModelName != "" && st.ModelName == model) || displayName(st.Model) == model ||
				(hasModel && standaloneServiceMatchesModel(st, modelAsset)) {
				alreadyShown = true
				break
			}
		}
		if alreadyShown {
			continue
		}
		attempt := s.standalone.lastParams[model]
		out = append(out, pendingServicePrep{
			model:       model,
			engine:      attempt.engine,
			contextSize: attempt.context,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].model < out[j].model
	})
	return out
}

func (s *viewState) renderPreparingServiceCard(c *ui.Context, prep pendingServicePrep) {
	t := tokens(c)
	outerR := t.Radius.Surface
	pad := t.Card.Padding
	innerR := concentricRadius(outerR, pad)
	cardKey := core.ServiceIDFor(prep.model, prep.engine)
	ctxStr := formatContextTokens(prep.contextSize)

	applyMaterial(c, ui.Column(c.Key("preparing-service-card-"+cardKey)).FillWidth().Padding(pad).Gap(t.Density.Row), materialCard).Radius(outerR).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			modelIconBox(c, prep.model, 28, innerR)
			ui.Text(c, prep.model).Bold().FontSize(t.Type.CardTitle).SingleLine()
			if prep.engine != "" {
				enginePill(c, prep.engine)
			}
			if ctxStr != "" {
				inlineBadge(c, ctxStr, stateNeutral, badgeOptions{Size: sizeSM, Style: badgeOutline})
			}
			inlineBadge(c, tr(c, "models.serve.badge.preparing"), stateActive, badgeOptions{Size: sizeSM, Style: badgeDot})
		})
		progressBar(c, -1, tr(c, "sidebar.status.service_starting"))
	})
}
