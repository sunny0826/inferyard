package pages

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

const uiBytesPerGiB = 1024.0 * 1024.0 * 1024.0

type uiHardwareMemoryInfo struct {
	TotalGB         float64
	CapGB           float64
	SnapshotFreeGB  float64
	HasSnapshotFree bool
	Unified         bool
	UsingHostPool   bool
	Known           bool
}

type uiHardwareMemoryCache struct {
	valid      bool
	hash       [32]byte
	rawLen     int
	host       uiHardwareMemoryInfo
	discrete   uiHardwareMemoryInfo
	parseCount int
}

func roundUIMemoryGB(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Round(v*100) / 100
}

func isUICPUOnlyGPULayers(gpuLayers *int) bool {
	return gpuLayers != nil && *gpuLayers == 0
}

func uiServiceUsesMemoryPool(hw uiHardwareMemoryInfo, svcGPULayers *int) bool {
	if hw.Unified {
		return true
	}
	return isUICPUOnlyGPULayers(svcGPULayers) == hw.UsingHostPool
}

func parseUIMemoryTimestamp(raw string) (time.Time, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, false
	}
	if ts, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
		return ts, true
	}
	if ts, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return ts, true
	}
	return time.Time{}, false
}

func uiServiceReflectedInSnapshot(startedAt string, capturedAt *string) bool {
	if capturedAt == nil {
		return false
	}
	capTime, okCap := parseUIMemoryTimestamp(*capturedAt)
	if !okCap {
		return false
	}
	startTime, okStart := parseUIMemoryTimestamp(startedAt)
	if !okStart {
		return false
	}
	return !startTime.After(capTime)
}

func parseUIHardwareMemory(raw []byte) (hostInfo, discreteInfo uiHardwareMemoryInfo) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return uiHardwareMemoryInfo{}, uiHardwareMemoryInfo{}
	}
	var doc struct {
		Hardware struct {
			AppleSilicon         bool   `json:"apple_silicon"`
			MemoryTotalBytes     *int64 `json:"memory_total_bytes"`
			MemoryAvailableBytes *int64 `json:"memory_available_bytes"`
			GPU                  struct {
				Devices []struct {
					MemoryKind       string `json:"memory_kind"`
					MemoryTotalBytes *int64 `json:"memory_total_bytes"`
					MemoryFreeBytes  *int64 `json:"memory_free_bytes"`
				} `json:"devices"`
			} `json:"gpu"`
		} `json:"hardware"`
	}
	if json.Unmarshal(trimmed, &doc) != nil {
		return uiHardwareMemoryInfo{}, uiHardwareMemoryInfo{}
	}
	var discreteVRAM int64
	var discreteFree int64
	hasDiscreteFree := false
	if !doc.Hardware.AppleSilicon {
		for _, dev := range doc.Hardware.GPU.Devices {
			if dev.MemoryKind == "shared_host" {
				continue
			}
			if dev.MemoryTotalBytes != nil && *dev.MemoryTotalBytes > 0 {
				discreteVRAM += *dev.MemoryTotalBytes
			}
			if dev.MemoryFreeBytes != nil && *dev.MemoryFreeBytes >= 0 {
				discreteFree += *dev.MemoryFreeBytes
				hasDiscreteFree = true
			}
		}
	}
	hasDiscreteGPU := !doc.Hardware.AppleSilicon && discreteVRAM > 0
	if doc.Hardware.MemoryTotalBytes != nil && *doc.Hardware.MemoryTotalBytes > 0 {
		total := float64(*doc.Hardware.MemoryTotalBytes) / uiBytesPerGiB
		var snapFree float64
		hasHostFree := doc.Hardware.MemoryAvailableBytes != nil && *doc.Hardware.MemoryAvailableBytes >= 0
		if hasHostFree {
			snapFree = float64(*doc.Hardware.MemoryAvailableBytes) / uiBytesPerGiB
		}
		hostInfo = uiHardwareMemoryInfo{
			TotalGB:         roundUIMemoryGB(total),
			CapGB:           roundUIMemoryGB(total * 0.75),
			SnapshotFreeGB:  roundUIMemoryGB(snapFree),
			HasSnapshotFree: hasHostFree,
			Unified:         !hasDiscreteGPU,
			UsingHostPool:   true,
			Known:           true,
		}
	} else if doc.Hardware.MemoryAvailableBytes != nil && *doc.Hardware.MemoryAvailableBytes > 0 {
		avail := float64(*doc.Hardware.MemoryAvailableBytes) / uiBytesPerGiB
		hostInfo = uiHardwareMemoryInfo{
			TotalGB:         roundUIMemoryGB(avail),
			CapGB:           roundUIMemoryGB(avail),
			SnapshotFreeGB:  roundUIMemoryGB(avail),
			HasSnapshotFree: true,
			Unified:         !hasDiscreteGPU,
			UsingHostPool:   true,
			Known:           true,
		}
	}
	if hasDiscreteGPU {
		total := float64(discreteVRAM) / uiBytesPerGiB
		var snapFree float64
		if hasDiscreteFree {
			snapFree = float64(discreteFree) / uiBytesPerGiB
		}
		discreteInfo = uiHardwareMemoryInfo{
			TotalGB:         roundUIMemoryGB(total),
			CapGB:           roundUIMemoryGB(total * 0.90),
			SnapshotFreeGB:  roundUIMemoryGB(snapFree),
			HasSnapshotFree: hasDiscreteFree,
			Unified:         false,
			UsingHostPool:   false,
			Known:           true,
		}
	} else {
		discreteInfo = hostInfo
	}
	return hostInfo, discreteInfo
}

func (s *viewState) localHardwareMemoryBudget(forceHostMemory bool) uiHardwareMemoryInfo {
	rawLen := len(s.device.Snapshot)
	hash := sha256.Sum256(s.device.Snapshot)
	if !s.hwMemoryCache.valid || s.hwMemoryCache.rawLen != rawLen || s.hwMemoryCache.hash != hash {
		hostInfo, discreteInfo := parseUIHardwareMemory(s.device.Snapshot)
		s.hwMemoryCache = uiHardwareMemoryCache{
			valid:      true,
			hash:       hash,
			rawLen:     rawLen,
			host:       hostInfo,
			discrete:   discreteInfo,
			parseCount: s.hwMemoryCache.parseCount + 1,
		}
	}
	if forceHostMemory {
		return s.hwMemoryCache.host
	}
	return s.hwMemoryCache.discrete
}

func uiEffectiveAvailableMemoryGB(hw uiHardwareMemoryInfo, reflectedUsedGB, excludedReflectedGB float64) float64 {
	if !hw.Known {
		return 0
	}
	avail := hw.CapGB
	if hw.HasSnapshotFree {
		snapCeiling := hw.SnapshotFreeGB + reflectedUsedGB + excludedReflectedGB
		if snapCeiling < avail {
			avail = snapCeiling
		}
	}
	return roundUIMemoryGB(avail)
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

func (s *viewState) activeLocalServicesMemoryForPool(hw uiHardwareMemoryInfo, excludeServiceID string) (usedGB, reflectedUsedGB, excludedReflectedGB float64) {
	for _, st := range s.activeManagedServices() {
		if st.State != "starting" && st.State != "running" {
			continue
		}
		if hw.Known && !uiServiceUsesMemoryPool(hw, st.GPULayers) {
			continue
		}
		var usage float64
		if st.EstimatedMemoryGB > 0 {
			usage = st.EstimatedMemoryGB
		} else {
			modelKey := strings.TrimSpace(st.ModelName)
			if modelKey == "" {
				modelKey = displayName(st.Model)
			}
			if m, ok := s.findModelAsset(modelKey); ok {
				if memBytes := m.EffectiveMemoryBytes(); memBytes != nil && *memBytes > 0 {
					if est, ok := estimateLocalModelMemoryGB(*memBytes, st.ContextSize); ok {
						usage = est
					}
				}
			}
		}
		if usage <= 0 {
			continue
		}
		reflected := uiServiceReflectedInSnapshot(st.StartedAt, s.device.CapturedAt)
		if excludeServiceID != "" && (st.ServiceID == excludeServiceID || serviceKey(st) == excludeServiceID) {
			if reflected {
				excludedReflectedGB += usage
			}
			continue
		}
		usedGB += usage
		if reflected {
			reflectedUsedGB += usage
		}
	}
	return roundUIMemoryGB(usedGB), roundUIMemoryGB(reflectedUsedGB), roundUIMemoryGB(excludedReflectedGB)
}

func (s *viewState) estimateLocalMemoryBudget(model, engine string, contextSize int64, gpuLayers *int) core.ServiceMemoryBudget {
	forceHost := isUICPUOnlyGPULayers(gpuLayers)
	hw := s.localHardwareMemoryBudget(forceHost)
	excludeID := ""
	if strings.TrimSpace(model) != "" && strings.TrimSpace(engine) != "" {
		excludeID = core.ServiceIDFor(model, engine)
	}
	used, reflectedUsed, excludedReflected := s.activeLocalServicesMemoryForPool(hw, excludeID)
	var totalGB, availGB float64
	var hwKnown bool
	if hw.Known {
		totalGB = hw.TotalGB
		availGB = uiEffectiveAvailableMemoryGB(hw, reflectedUsed, excludedReflected)
		hwKnown = true
	} else if !forceHost && s.memorySummary.Known {
		totalGB, availGB, hwKnown = s.memorySummary.TotalGB, s.memorySummary.AvailableGB, true
	}
	var est float64
	var estKnown bool
	if m, ok := s.findModelAsset(model); ok {
		if memBytes := m.EffectiveMemoryBytes(); memBytes != nil && *memBytes > 0 {
			est, estKnown = estimateLocalModelMemoryGB(*memBytes, contextSize)
		}
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

func (s *viewState) checkMemoryBudget(model, engine string, contextSize int64, gpuLayers *int) core.ServiceMemoryBudget {
	return s.estimateLocalMemoryBudget(model, engine, contextSize, gpuLayers)
}

func (s *viewState) currentMemorySummary() core.ServiceMemoryBudget {
	if s.memorySummary.Known {
		return s.memorySummary
	}
	hw := s.localHardwareMemoryBudget(false)
	used, reflectedUsed, _ := s.activeLocalServicesMemoryForPool(hw, "")
	if !hw.Known {
		return core.ServiceMemoryBudget{UsedGB: used, Enough: true, Known: false}
	}
	availGB := uiEffectiveAvailableMemoryGB(hw, reflectedUsed, 0)
	remaining := roundUIMemoryGB(math.Max(0, availGB-used))
	return core.ServiceMemoryBudget{
		TotalGB:     hw.TotalGB,
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

func serviceMetricTile(c *ui.Context, _ float32, label, value, sub string, tabular bool) {
	t := tokens(c)
	ui.Column(c).FillWidth().Padding(t.Density.Tight, 0).Gap(t.Density.Hairline).Children(func() {
		ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
		v := ui.Text(c, value).FontWeight(600).FontSize(t.Type.Primary).TextColor(t.Color.FgPrimary).SingleLine()
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
