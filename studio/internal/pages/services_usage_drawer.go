package pages

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
)

const serviceUsageDefaultAPIKey = "not-needed"

type serviceUsageDrawerState struct {
	open        bool
	baseURL     string
	model       string
	engine      string
	tab         string
	testing     bool
	testResult  *core.ServiceTestResult
	testSeq     uint64
	testProbeFn func(ctx context.Context, baseURL, model string) core.ServiceTestResult
}

func normalizeOpenAIBaseURL(endpoint string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if trimmed == "" {
		return ""
	}
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed
	}
	return trimmed + "/v1"
}

func (s *viewState) openServiceUsageDrawer(endpoint, model, engine string) {
	s.serviceUsage.open = true
	s.serviceUsage.baseURL = normalizeOpenAIBaseURL(endpoint)
	s.serviceUsage.model = strings.TrimSpace(model)
	s.serviceUsage.engine = strings.TrimSpace(engine)
	if s.serviceUsage.tab == "" {
		s.serviceUsage.tab = "curl"
	}
	s.serviceUsage.testing = false
	s.serviceUsage.testResult = nil
	s.serviceUsage.testSeq++
}

func (s *viewState) serviceUsageDrawer(c *ui.Context) {
	drawer(c, &s.serviceUsage.open, drawerOptions{
		Title: tr(c, "services.usage.title"),
		Width: c.Theme().Space(140),
	}, func(_ ui.Element) {
		t := tokens(c)
		st := &s.serviceUsage
		if st.tab == "" {
			st.tab = "curl"
		}
		ui.Scroll(c.Key("service-usage-scroll")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				s.usageCopyRow(c, "base-url", tr(c, "services.usage.base_url"), st.baseURL, "")
				s.usageCopyRow(c, "model", tr(c, "services.usage.model"), st.model, "")
				s.usageCopyRow(c, "api-key", tr(c, "services.usage.api_key"), serviceUsageDefaultAPIKey, tr(c, "services.usage.api_key_hint"))

				ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
					ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						segmentedControl(c, tr(c, "services.usage.snippet_label"), &st.tab, []components.NativeSelectOption[string]{
							{Value: "curl", Label: tr(c, "services.usage.tab.curl")},
							{Value: "python", Label: tr(c, "services.usage.tab.python")},
							{Value: "env", Label: tr(c, "services.usage.tab.env")},
						})
						ui.Box(c).Grow(1)
						snippet := buildUsageSnippet(st.tab, st.baseURL, st.model)
						if inlineButton(c, components.ButtonOutline, tr(c, "services.usage.copy_code")).Clicked() {
							s.copyUsageText(c, snippet)
						}
					})
					snippet := buildUsageSnippet(st.tab, st.baseURL, st.model)
					ui.Column(c).FillWidth().Padding(t.Density.Stack).Radius(t.Radius.Control).
						Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).Children(func() {
						ui.Text(c, snippet).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).Selectable()
					})
				})

				s.usageConnectivitySection(c)
			})
		})
	})
}

func (s *viewState) usageCopyRow(c *ui.Context, key, label, value, hint string) {
	t := tokens(c)
	ui.Column(c.Key("usage-row-"+key)).FillWidth().Gap(t.Density.Hairline).
		Padding(t.Density.Row).Radius(t.Radius.Control).
		Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, label).FontSize(t.Type.ShellLabel).TextColor(t.Color.FgSecondary).SingleLine()
				ui.Text(c, nonEmptyOrMissing(c, value)).FontSize(t.Type.Body).FontFeatures("tnum").TextColor(t.Color.FgPrimary).Selectable()
			})
			copyBtn := inlineButton(c, components.ButtonOutline, tr(c, "services.usage.copy")).
				Label(tr(c, "services.usage.copy") + " " + label).
				Disabled(strings.TrimSpace(value) == "")
			if copyBtn.Clicked() {
				s.copyUsageText(c, value)
			}
		})
		if hint != "" {
			ui.Text(c, hint).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}
	})
}

func (s *viewState) usageConnectivitySection(c *ui.Context) {
	t := tokens(c)
	st := &s.serviceUsage
	ui.Column(c.Key("service-usage-test")).FillWidth().Gap(t.Density.Row).
		Padding(t.Density.Stack).Radius(t.Radius.Surface).
		Background(t.Color.Surface).Border(1, t.Color.BorderSubtle).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Text(c, tr(c, "services.usage.test.title")).Bold().FontSize(t.Type.Section).Grow(1)
			btn := primaryActionButton(c, tr(c, "services.usage.test.action"), st.testing).
				Disabled(st.testing || strings.TrimSpace(st.baseURL) == "")
			if btn.Clicked() {
				s.runServiceConnectivityTest()
			}
		})
		if st.testing {
			ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				components.Spinner(c, components.SpinnerRing).Size(14, 14)
				ui.Text(c, tr(c, "services.usage.test.running")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			})
			return
		}
		if st.testResult == nil {
			return
		}
		res := *st.testResult
		if res.OK {
			scale := stateScale(c, stateSuccess)
			ui.Column(c).FillWidth().Gap(t.Density.Hairline).
				Padding(t.Density.Row).Radius(t.Radius.Control).
				Background(scale.SubtleBg).Border(1, scale.Border).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					components.Icon(c, reicon.CircleCheck).Size(14, 14).TextColor(scale.Fg)
					ui.Text(c, trf(c, "services.usage.test.ok", "code", fmt.Sprintf("%d", res.StatusCode), "ms", fmt.Sprintf("%d", res.LatencyMs))).
						Bold().FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(scale.Fg)
				})
				if strings.TrimSpace(res.Reply) != "" {
					ui.Text(c, trf(c, "services.usage.test.reply", "reply", res.Reply)).
						FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).Selectable()
				}
			})
			return
		}
		scale := stateScale(c, stateDanger)
		msg := trf(c, "services.usage.test.failed", "error", res.Error)
		if res.StatusCode > 0 {
			msg = trf(c, "services.usage.test.failed_status",
				"code", fmt.Sprintf("%d", res.StatusCode),
				"ms", fmt.Sprintf("%d", res.LatencyMs),
				"error", res.Error,
			)
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Tight).
			Padding(t.Density.Row).Radius(t.Radius.Control).
			Background(scale.SubtleBg).Border(1, scale.Border).Children(func() {
			components.Icon(c, reicon.CircleAlert).Size(14, 14).TextColor(scale.Fg)
			ui.Text(c, msg).FontSize(t.Type.Caption).TextColor(scale.Fg).Grow(1).Selectable()
		})
	})
}

func (s *viewState) runServiceConnectivityTest() {
	st := &s.serviceUsage
	if st.testing || strings.TrimSpace(st.baseURL) == "" {
		return
	}
	st.testing = true
	st.testResult = nil
	st.testSeq++
	seq := st.testSeq
	baseURL := st.baseURL
	model := st.model

	if s.simulate(previewTestServiceEndpoint{baseURL: baseURL, model: model, seq: seq}) {
		return
	}

	probe := st.testProbeFn
	if probe == nil {
		if s.service != nil {
			probe = s.service.TestServiceEndpoint
		} else {
			probe = core.TestServiceEndpoint
		}
	}
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		res := probe(ctx, baseURL, model)
		if ctx.Err() != nil {
			return
		}
		s.runOnUI(func() {
			if s.serviceUsage.testSeq != seq {
				return
			}
			s.serviceUsage.testing = false
			cp := res
			s.serviceUsage.testResult = &cp
		})
	}()
}

func (s *viewState) copyUsageText(c *ui.Context, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if s.preview != nil {
		s.enqueueStatusToast(tr(c, "services.usage.toast.copied_preview"))
		return
	}
	c.WriteClipboardThen(text, func(err error) {
		if err != nil {
			s.enqueueStatusToast(l10n.TR("services.usage.toast.copy_failed"))
			return
		}
		s.enqueueStatusToast(l10n.TR("services.usage.toast.copied"))
	})
}

func shellSingleQuote(raw string) string {
	return "'" + strings.ReplaceAll(raw, "'", `'\''`) + "'"
}

func marshalJSONNoEscape(v any, indent string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	if err := enc.Encode(v); err != nil {
		return `""`
	}
	return strings.TrimRight(buf.String(), "\n")
}

func buildUsageSnippet(tab, baseURL, model string) string {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "http://127.0.0.1:8080/v1"
	}
	if strings.TrimSpace(model) == "" {
		model = "local-model"
	}
	switch tab {
	case "python":
		return fmt.Sprintf(`from openai import OpenAI

client = OpenAI(
    base_url=%s,
    api_key=%s,
)

resp = client.chat.completions.create(
    model=%s,
    messages=[{"role": "user", "content": "Hello!"}],
)
print(resp.choices[0].message.content)`,
			marshalJSONNoEscape(baseURL, ""),
			marshalJSONNoEscape(serviceUsageDefaultAPIKey, ""),
			marshalJSONNoEscape(model, ""),
		)
	case "env":
		return fmt.Sprintf(`export OPENAI_BASE_URL=%s
export OPENAI_API_KEY=%s
export OPENAI_MODEL=%s`,
			shellSingleQuote(baseURL),
			shellSingleQuote(serviceUsageDefaultAPIKey),
			shellSingleQuote(model),
		)
	default:
		payload := struct {
			Model    string              `json:"model"`
			Messages []map[string]string `json:"messages"`
		}{
			Model:    model,
			Messages: []map[string]string{{"role": "user", "content": "Hello!"}},
		}
		body := marshalJSONNoEscape(payload, "  ")
		return fmt.Sprintf(`curl %s \
  -H "Content-Type: application/json" \
  -H %s \
  -d %s`,
			shellSingleQuote(baseURL+"/chat/completions"),
			shellSingleQuote("Authorization: Bearer "+serviceUsageDefaultAPIKey),
			shellSingleQuote(body),
		)
	}
}
