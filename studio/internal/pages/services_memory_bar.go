package pages

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

const uiBytesPerGiB = 1024.0 * 1024.0 * 1024.0

func roundUIMemoryGB(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Round(v*100) / 100
}

func (s *viewState) localHardwareMemoryBudget() (totalGB, availableGB float64, known bool) {
	trimmed := bytes.TrimSpace(s.device.Snapshot)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return 0, 0, false
	}
	var doc struct {
		Hardware struct {
			AppleSilicon     bool   `json:"apple_silicon"`
			MemoryTotalBytes *int64 `json:"memory_total_bytes"`
			GPU              struct {
				Devices []struct {
					MemoryKind       string `json:"memory_kind"`
					MemoryTotalBytes *int64 `json:"memory_total_bytes"`
				} `json:"devices"`
			} `json:"gpu"`
		} `json:"hardware"`
	}
	if json.Unmarshal(trimmed, &doc) != nil {
		return 0, 0, false
	}
	var discreteVRAM int64
	if !doc.Hardware.AppleSilicon {
		for _, dev := range doc.Hardware.GPU.Devices {
			if dev.MemoryKind == "shared_host" {
				continue
			}
			if dev.MemoryTotalBytes != nil && *dev.MemoryTotalBytes > 0 {
				discreteVRAM += *dev.MemoryTotalBytes
			}
		}
	}
	if discreteVRAM > 0 {
		total := float64(discreteVRAM) / uiBytesPerGiB
		return roundUIMemoryGB(total), roundUIMemoryGB(total * 0.90), true
	}
	if doc.Hardware.MemoryTotalBytes != nil && *doc.Hardware.MemoryTotalBytes > 0 {
		total := float64(*doc.Hardware.MemoryTotalBytes) / uiBytesPerGiB
		return roundUIMemoryGB(total), roundUIMemoryGB(total * 0.75), true
	}
	return 0, 0, false
}

func estimateLocalModelMemoryGB(sizeBytes, contextSize int64) (float64, bool) {
	if sizeBytes <= 0 {
		return 0, false
	}
	modelGB := float64(sizeBytes) / uiBytesPerGiB
	if contextSize <= 0 {
		contextSize = 4096
	}
	kvBase := modelGB * 0.08
	if kvBase < 0.25 {
		kvBase = 0.25
	}
	kvGB := (float64(contextSize) / 8192.0) * kvBase
	return roundUIMemoryGB(modelGB*1.15 + kvGB), true
}

func (s *viewState) activeLocalServicesMemoryGB(excludeServiceID string) float64 {
	var used float64
	for _, st := range s.activeManagedServices() {
		if st.State != "starting" && st.State != "running" {
			continue
		}
		if excludeServiceID != "" && (st.ServiceID == excludeServiceID || serviceKey(st) == excludeServiceID) {
			continue
		}
		if st.EstimatedMemoryGB > 0 {
			used += st.EstimatedMemoryGB
			continue
		}
		modelKey := strings.TrimSpace(st.ModelName)
		if modelKey == "" {
			modelKey = displayName(st.Model)
		}
		if m, ok := s.findModelAsset(modelKey); ok && m.SizeBytes != nil && *m.SizeBytes > 0 {
			if est, ok := estimateLocalModelMemoryGB(*m.SizeBytes, st.ContextSize); ok {
				used += est
			}
		}
	}
	return roundUIMemoryGB(used)
}

func (s *viewState) estimateLocalMemoryBudget(model, engine string, contextSize int64) core.ServiceMemoryBudget {
	totalGB, availGB, hwKnown := s.localHardwareMemoryBudget()
	excludeID := ""
	if strings.TrimSpace(model) != "" && strings.TrimSpace(engine) != "" {
		excludeID = core.ServiceIDFor(model, engine)
	}
	used := s.activeLocalServicesMemoryGB(excludeID)
	var est float64
	var estKnown bool
	if m, ok := s.findModelAsset(model); ok && m.SizeBytes != nil && *m.SizeBytes > 0 {
		est, estKnown = estimateLocalModelMemoryGB(*m.SizeBytes, contextSize)
	}
	remaining := roundUIMemoryGB(math.Max(0, availGB-used))
	if !hwKnown || !estKnown {
		return core.ServiceMemoryBudget{
			TotalGB:     totalGB,
			AvailableGB: availGB,
			UsedGB:      used,
			EstimatedGB: est,
			RemainingGB: remaining,
			Enough:      true,
			Known:       false,
		}
	}
	return core.ServiceMemoryBudget{
		TotalGB:     totalGB,
		AvailableGB: availGB,
		UsedGB:      used,
		EstimatedGB: est,
		RemainingGB: remaining,
		Enough:      est <= remaining+1e-6,
		Known:       true,
	}
}

func (s *viewState) checkMemoryBudget(model, engine string, contextSize int64) core.ServiceMemoryBudget {
	if s.service != nil && s.preview == nil {
		budget := s.service.CheckServiceMemoryBudget(model, engine, contextSize)
		if budget.Known {
			return budget
		}
	}
	return s.estimateLocalMemoryBudget(model, engine, contextSize)
}

func (s *viewState) currentMemorySummary() core.ServiceMemoryBudget {
	if s.memorySummary.Known {
		return s.memorySummary
	}
	totalGB, availGB, hwKnown := s.localHardwareMemoryBudget()
	used := s.activeLocalServicesMemoryGB("")
	if !hwKnown {
		return core.ServiceMemoryBudget{UsedGB: used, Enough: true, Known: false}
	}
	remaining := roundUIMemoryGB(math.Max(0, availGB-used))
	return core.ServiceMemoryBudget{
		TotalGB:     totalGB,
		AvailableGB: availGB,
		UsedGB:      used,
		RemainingGB: remaining,
		Enough:      remaining > 0,
		Known:       true,
	}
}

func (s *viewState) serviceMemoryBudgetBar(c *ui.Context, budget core.ServiceMemoryBudget) {
	if !budget.Known || budget.AvailableGB <= 0 {
		return
	}
	t := tokens(c)
	projected := budget.UsedGB + budget.EstimatedGB
	ratio := projected / budget.AvailableGB
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	barColor := t.Color.Success.Solid
	if !budget.Enough {
		barColor = t.Color.Danger.Solid
	} else if ratio >= 0.85 {
		barColor = t.Color.Warning.Solid
	}
	ui.Column(c.Key("service-memory-budget-bar")).FillWidth().Gap(t.Density.Tight).
		Padding(t.Density.Row).Radius(t.Radius.Control).
		Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).Children(func() {
		ui.Text(c, tr(c, "services.params.memory.title")).Bold().FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary)
		ui.Box(c).FillWidth().Height(6).Radius(t.Radius.Full).Background(t.Color.BorderSubtle).Children(func() {
			if ratio > 0 {
				ui.Box(c).WidthPercent(float32(ratio * 100)).Height(6).Radius(t.Radius.Full).Background(barColor)
			}
		})
		ui.Text(c, trf(c, "services.params.memory.summary",
			"used", fmt.Sprintf("%.1f", budget.UsedGB),
			"est", fmt.Sprintf("%.1f", budget.EstimatedGB),
			"remaining", fmt.Sprintf("%.1f", budget.RemainingGB),
			"available", fmt.Sprintf("%.1f", budget.AvailableGB),
		)).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
	})
	if !budget.Enough {
		noticeBanner(c, noticeBannerOptions{
			Kind: stateDanger,
			Size: bannerCompact,
			Message: trf(c, "services.params.memory.insufficient",
				"remaining", fmt.Sprintf("%.1f", budget.RemainingGB),
				"est", fmt.Sprintf("%.1f", budget.EstimatedGB),
			),
		})
	}
}

func serviceMetricTile(c *ui.Context, radius float32, label, value, sub string, tabular bool) {
	t := tokens(c)
	ui.Column(c).FillWidth().Padding(t.Density.Row).Radius(radius).
		Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).Gap(2).Children(func() {
		ui.Text(c, label).FontSize(t.Type.ShellLabel).TextColor(t.Color.FgSecondary).SingleLine()
		v := ui.Text(c, value).Bold().FontSize(t.Type.Section).TextColor(t.Color.FgPrimary).SingleLine()
		if tabular {
			v.FontFeatures("tnum")
		}
		if sub != "" {
			ui.Text(c, sub).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
		}
	})
}

func nonEmptyOrMissing(c *ui.Context, v string) string {
	if strings.TrimSpace(v) == "" {
		return tr(c, "common.missing")
	}
	return v
}
