package core

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"time"
)

const bytesPerGiB = 1024.0 * 1024.0 * 1024.0

// ServiceMemoryBudget summarizes available inference memory against active services
// and an optional target service launch estimate.
type ServiceMemoryBudget struct {
	TotalGB     float64 `json:"total_gb"`
	AvailableGB float64 `json:"available_gb"`
	UsedGB      float64 `json:"used_gb"`
	EstimatedGB float64 `json:"estimated_gb"`
	RemainingGB float64 `json:"remaining_gb"`
	Enough      bool    `json:"enough"`
	Known       bool    `json:"known"`
}

type hardwareMemoryInfo struct {
	TotalGB         float64
	AvailableGB     float64
	CapGB           float64
	SnapshotFreeGB  float64
	HasSnapshotFree bool
	Unified         bool
	UsingHostPool   bool
	Known           bool
}

func roundMemoryGB(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Round(v*100) / 100
}

func isCPUOnlyGPULayers(gpuLayers *int) bool {
	return gpuLayers != nil && *gpuLayers == 0
}

func serviceUsesMemoryPool(hw hardwareMemoryInfo, svcGPULayers *int) bool {
	if hw.Unified {
		return true
	}
	return isCPUOnlyGPULayers(svcGPULayers) == hw.UsingHostPool
}

func parseMemoryTimestamp(raw string) (time.Time, bool) {
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

func serviceReflectedInSnapshot(startedAt string, capturedAt *string) bool {
	if capturedAt == nil {
		return false
	}
	capTime, okCap := parseMemoryTimestamp(*capturedAt)
	if !okCap {
		return false
	}
	startTime, okStart := parseMemoryTimestamp(startedAt)
	if !okStart {
		return false
	}
	return !startTime.After(capTime)
}

func parseHardwareMemoryBudget(snapshot json.RawMessage, forceHostMemory bool) hardwareMemoryInfo {
	trimmed := bytes.TrimSpace(snapshot)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return hardwareMemoryInfo{}
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
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return hardwareMemoryInfo{}
	}

	var discreteVRAMBytes int64
	var discreteFreeBytes int64
	hasDiscreteFree := false
	if !doc.Hardware.AppleSilicon {
		for _, dev := range doc.Hardware.GPU.Devices {
			if dev.MemoryKind == "shared_host" {
				continue
			}
			if dev.MemoryTotalBytes != nil && *dev.MemoryTotalBytes > 0 {
				discreteVRAMBytes += *dev.MemoryTotalBytes
			}
			if dev.MemoryFreeBytes != nil && *dev.MemoryFreeBytes >= 0 {
				discreteFreeBytes += *dev.MemoryFreeBytes
				hasDiscreteFree = true
			}
		}
	}
	hasDiscreteGPU := !doc.Hardware.AppleSilicon && discreteVRAMBytes > 0
	if hasDiscreteGPU && !forceHostMemory {
		total := float64(discreteVRAMBytes) / bytesPerGiB
		capGB := total * 0.90
		avail := capGB
		var snapFree float64
		if hasDiscreteFree {
			snapFree = float64(discreteFreeBytes) / bytesPerGiB
			if snapFree < avail {
				avail = snapFree
			}
		}
		return hardwareMemoryInfo{
			TotalGB:         roundMemoryGB(total),
			AvailableGB:     roundMemoryGB(avail),
			CapGB:           roundMemoryGB(capGB),
			SnapshotFreeGB:  roundMemoryGB(snapFree),
			HasSnapshotFree: hasDiscreteFree,
			Unified:         false,
			UsingHostPool:   false,
			Known:           true,
		}
	}
	if doc.Hardware.MemoryTotalBytes != nil && *doc.Hardware.MemoryTotalBytes > 0 {
		total := float64(*doc.Hardware.MemoryTotalBytes) / bytesPerGiB
		capGB := total * 0.75
		avail := capGB
		var snapFree float64
		hasHostFree := doc.Hardware.MemoryAvailableBytes != nil && *doc.Hardware.MemoryAvailableBytes >= 0
		if hasHostFree {
			snapFree = float64(*doc.Hardware.MemoryAvailableBytes) / bytesPerGiB
			if snapFree < avail {
				avail = snapFree
			}
		}
		return hardwareMemoryInfo{
			TotalGB:         roundMemoryGB(total),
			AvailableGB:     roundMemoryGB(avail),
			CapGB:           roundMemoryGB(capGB),
			SnapshotFreeGB:  roundMemoryGB(snapFree),
			HasSnapshotFree: hasHostFree,
			Unified:         !hasDiscreteGPU,
			UsingHostPool:   true,
			Known:           true,
		}
	}
	if doc.Hardware.MemoryAvailableBytes != nil && *doc.Hardware.MemoryAvailableBytes > 0 {
		avail := float64(*doc.Hardware.MemoryAvailableBytes) / bytesPerGiB
		return hardwareMemoryInfo{
			TotalGB:         roundMemoryGB(avail),
			AvailableGB:     roundMemoryGB(avail),
			CapGB:           roundMemoryGB(avail),
			SnapshotFreeGB:  roundMemoryGB(avail),
			HasSnapshotFree: true,
			Unified:         !hasDiscreteGPU,
			UsingHostPool:   true,
			Known:           true,
		}
	}
	return hardwareMemoryInfo{}
}

func estimateModelMemoryGB(sizeBytes int64, contextSize int64) (float64, bool) {
	if sizeBytes <= 0 {
		return 0, false
	}
	modelGB := float64(sizeBytes) / bytesPerGiB
	if contextSize <= 0 {
		contextSize = 4096
	}
	kvBase := modelGB * 0.08
	if kvBase < 0.25 {
		kvBase = 0.25
	}
	kvGB := (float64(contextSize) / 8192.0) * kvBase
	total := modelGB*1.15 + kvGB
	return roundMemoryGB(total), true
}

func modelSizeBytesFromRegistry(reg registryData, model string) (int64, bool) {
	target := strings.TrimSpace(model)
	if target == "" {
		return 0, false
	}
	normTarget := normalizedPath(target)
	for _, entry := range reg.models {
		if entry.model.Name == target || entry.path == target || (normTarget != "" && normalizedPath(entry.path) == normTarget) {
			if entry.model.SizeBytes != nil && *entry.model.SizeBytes > 0 {
				return *entry.model.SizeBytes, true
			}
			return 0, false
		}
	}
	return 0, false
}

func effectiveAvailableMemoryGB(hw hardwareMemoryInfo, reflectedUsedGB, excludedReflectedGB float64) float64 {
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
	return roundMemoryGB(avail)
}

func (s *Service) activeServicesMemoryForPoolLocked(reg registryData, hw hardwareMemoryInfo, excludeServiceID string, capturedAt *string) (usedGB, reflectedUsedGB, excludedReflectedGB float64) {
	seen := map[*engineEntry]bool{}
	entryUsage := func(e *engineEntry) float64 {
		if e.state.EstimatedMemoryGB > 0 {
			return e.state.EstimatedMemoryGB
		}
		modelKey := e.state.ModelName
		if modelKey == "" {
			modelKey = e.state.Model
		}
		if sizeBytes, ok := modelSizeBytesFromRegistry(reg, modelKey); ok {
			if est, ok := estimateModelMemoryGB(sizeBytes, e.state.ContextSize); ok {
				return est
			}
		}
		return 0
	}
	accumulate := func(e *engineEntry) {
		if e == nil || seen[e] {
			return
		}
		seen[e] = true
		if e.state.State != "starting" && e.state.State != "running" {
			return
		}
		if hw.Known && !serviceUsesMemoryPool(hw, e.state.GPULayers) {
			return
		}
		usage := entryUsage(e)
		if usage <= 0 {
			return
		}
		reflected := serviceReflectedInSnapshot(e.state.StartedAt, capturedAt)
		if excludeServiceID != "" && e.state.ServiceID == excludeServiceID {
			if reflected {
				excludedReflectedGB += usage
			}
			return
		}
		usedGB += usage
		if reflected {
			reflectedUsedGB += usage
		}
	}
	for _, e := range s.services {
		accumulate(e)
	}
	accumulate(s.engine)
	return roundMemoryGB(usedGB), roundMemoryGB(reflectedUsedGB), roundMemoryGB(excludedReflectedGB)
}

// CheckServiceMemoryBudget evaluates whether there is enough remaining inference memory
// to start the requested (model, engine, contextSize) alongside currently running services.
func (s *Service) CheckServiceMemoryBudget(model, engine string, contextSize int64) ServiceMemoryBudget {
	reg := s.registry()
	return s.checkServiceMemoryBudgetWithRegistry(model, engine, contextSize, nil, reg)
}

// CheckServiceMemoryBudgetForRequest evaluates inference memory budget for a full start request,
// routing CPU-only launches (gpuLayers == 0) against host memory instead of discrete GPU VRAM.
func (s *Service) CheckServiceMemoryBudgetForRequest(req StandaloneStartRequest) ServiceMemoryBudget {
	reg := s.registry()
	return s.checkServiceMemoryBudgetWithRegistry(req.Model, req.Engine, req.ContextSize, req.GPULayers, reg)
}

func (s *Service) checkServiceMemoryBudgetWithRegistry(model, engine string, contextSize int64, gpuLayers *int, reg registryData) ServiceMemoryBudget {
	model = strings.TrimSpace(model)
	engine = strings.TrimSpace(engine)
	excludeID := ""
	if model != "" && engine != "" {
		excludeID = ServiceIDFor(model, engine)
	}

	s.mu.Lock()
	hw := parseHardwareMemoryBudget(s.device.Snapshot, isCPUOnlyGPULayers(gpuLayers))
	used, reflectedUsed, excludedReflected := s.activeServicesMemoryForPoolLocked(reg, hw, excludeID, s.device.CapturedAt)
	s.mu.Unlock()

	availGB := effectiveAvailableMemoryGB(hw, reflectedUsed, excludedReflected)
	var est float64
	var estKnown bool
	if sizeBytes, ok := modelSizeBytesFromRegistry(reg, model); ok {
		est, estKnown = estimateModelMemoryGB(sizeBytes, contextSize)
	}

	remaining := roundMemoryGB(math.Max(0, availGB-used))
	if !hw.Known || !estKnown {
		return ServiceMemoryBudget{
			TotalGB:     hw.TotalGB,
			AvailableGB: availGB,
			UsedGB:      used,
			EstimatedGB: est,
			RemainingGB: remaining,
			Enough:      true,
			Known:       false,
		}
	}
	return ServiceMemoryBudget{
		TotalGB:     hw.TotalGB,
		AvailableGB: availGB,
		UsedGB:      used,
		EstimatedGB: est,
		RemainingGB: remaining,
		Enough:      est <= remaining+1e-6,
		Known:       true,
	}
}

// ServiceMemoryUsage returns the best available memory usage in GB for a service,
// distinguishing live measured RSS (measured=true) from a static launch estimate (measured=false).
// When neither is available, known is false and gb is 0.
func ServiceMemoryUsage(state EngineState) (gb float64, measured bool, known bool) {
	if state.RuntimeMemoryGB != nil && *state.RuntimeMemoryGB > 0 {
		return roundMemoryGB(*state.RuntimeMemoryGB), true, true
	}
	if state.EstimatedMemoryGB > 0 {
		return roundMemoryGB(state.EstimatedMemoryGB), false, true
	}
	return 0, false, false
}

// CurrentServiceMemorySummary returns the overall hardware memory ceiling and
// current estimated usage across all active managed services.
func (s *Service) CurrentServiceMemorySummary() ServiceMemoryBudget {
	reg := s.registry()
	s.mu.Lock()
	hw := parseHardwareMemoryBudget(s.device.Snapshot, false)
	used, reflectedUsed, _ := s.activeServicesMemoryForPoolLocked(reg, hw, "", s.device.CapturedAt)
	s.mu.Unlock()

	availGB := effectiveAvailableMemoryGB(hw, reflectedUsed, 0)
	remaining := roundMemoryGB(math.Max(0, availGB-used))
	if !hw.Known {
		return ServiceMemoryBudget{
			UsedGB: used,
			Enough: true,
			Known:  false,
		}
	}
	return ServiceMemoryBudget{
		TotalGB:     hw.TotalGB,
		AvailableGB: availGB,
		UsedGB:      used,
		RemainingGB: remaining,
		Enough:      remaining > 0,
		Known:       true,
	}
}
