package core

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
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
	TotalGB     float64
	AvailableGB float64
	Known       bool
}

func roundMemoryGB(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Round(v*100) / 100
}

func parseHardwareMemoryBudget(snapshot json.RawMessage) hardwareMemoryInfo {
	trimmed := bytes.TrimSpace(snapshot)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return hardwareMemoryInfo{}
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
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return hardwareMemoryInfo{}
	}

	var discreteVRAMBytes int64
	if !doc.Hardware.AppleSilicon {
		for _, dev := range doc.Hardware.GPU.Devices {
			if dev.MemoryKind == "shared_host" {
				continue
			}
			if dev.MemoryTotalBytes != nil && *dev.MemoryTotalBytes > 0 {
				discreteVRAMBytes += *dev.MemoryTotalBytes
			}
		}
	}
	if discreteVRAMBytes > 0 {
		total := float64(discreteVRAMBytes) / bytesPerGiB
		return hardwareMemoryInfo{
			TotalGB:     roundMemoryGB(total),
			AvailableGB: roundMemoryGB(total * 0.90),
			Known:       true,
		}
	}
	if doc.Hardware.MemoryTotalBytes != nil && *doc.Hardware.MemoryTotalBytes > 0 {
		total := float64(*doc.Hardware.MemoryTotalBytes) / bytesPerGiB
		return hardwareMemoryInfo{
			TotalGB:     roundMemoryGB(total),
			AvailableGB: roundMemoryGB(total * 0.75),
			Known:       true,
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

func (s *Service) activeServicesMemoryGBLocked(reg registryData, excludeServiceID string) float64 {
	var used float64
	seen := map[*engineEntry]bool{}
	accumulate := func(e *engineEntry) {
		if e == nil || seen[e] {
			return
		}
		seen[e] = true
		if e.state.State != "starting" && e.state.State != "running" {
			return
		}
		if excludeServiceID != "" && e.state.ServiceID == excludeServiceID {
			return
		}
		if e.state.EstimatedMemoryGB > 0 {
			used += e.state.EstimatedMemoryGB
			return
		}
		modelKey := e.state.ModelName
		if modelKey == "" {
			modelKey = e.state.Model
		}
		if sizeBytes, ok := modelSizeBytesFromRegistry(reg, modelKey); ok {
			if est, ok := estimateModelMemoryGB(sizeBytes, e.state.ContextSize); ok {
				used += est
			}
		}
	}
	for _, e := range s.services {
		accumulate(e)
	}
	accumulate(s.engine)
	return roundMemoryGB(used)
}

// CheckServiceMemoryBudget evaluates whether there is enough remaining inference memory
// to start the requested (model, engine, contextSize) alongside currently running services.
func (s *Service) CheckServiceMemoryBudget(model, engine string, contextSize int64) ServiceMemoryBudget {
	reg := s.registry()
	return s.checkServiceMemoryBudgetWithRegistry(model, engine, contextSize, reg)
}

func (s *Service) checkServiceMemoryBudgetWithRegistry(model, engine string, contextSize int64, reg registryData) ServiceMemoryBudget {
	model = strings.TrimSpace(model)
	engine = strings.TrimSpace(engine)
	excludeID := ""
	if model != "" && engine != "" {
		excludeID = ServiceIDFor(model, engine)
	}

	s.mu.Lock()
	hw := parseHardwareMemoryBudget(s.device.Snapshot)
	used := s.activeServicesMemoryGBLocked(reg, excludeID)
	s.mu.Unlock()

	var est float64
	var estKnown bool
	if sizeBytes, ok := modelSizeBytesFromRegistry(reg, model); ok {
		est, estKnown = estimateModelMemoryGB(sizeBytes, contextSize)
	}

	remaining := roundMemoryGB(math.Max(0, hw.AvailableGB-used))
	if !hw.Known || !estKnown {
		return ServiceMemoryBudget{
			TotalGB:     hw.TotalGB,
			AvailableGB: hw.AvailableGB,
			UsedGB:      used,
			EstimatedGB: est,
			RemainingGB: remaining,
			Enough:      true,
			Known:       false,
		}
	}
	return ServiceMemoryBudget{
		TotalGB:     hw.TotalGB,
		AvailableGB: hw.AvailableGB,
		UsedGB:      used,
		EstimatedGB: est,
		RemainingGB: remaining,
		Enough:      est <= remaining+1e-6,
		Known:       true,
	}
}

// CurrentServiceMemorySummary returns the overall hardware memory ceiling and
// current estimated usage across all active managed services.
func (s *Service) CurrentServiceMemorySummary() ServiceMemoryBudget {
	reg := s.registry()
	s.mu.Lock()
	hw := parseHardwareMemoryBudget(s.device.Snapshot)
	used := s.activeServicesMemoryGBLocked(reg, "")
	s.mu.Unlock()

	remaining := roundMemoryGB(math.Max(0, hw.AvailableGB-used))
	if !hw.Known {
		return ServiceMemoryBudget{
			UsedGB: used,
			Enough: true,
			Known:  false,
		}
	}
	return ServiceMemoryBudget{
		TotalGB:     hw.TotalGB,
		AvailableGB: hw.AvailableGB,
		UsedGB:      used,
		RemainingGB: remaining,
		Enough:      remaining > 0,
		Known:       true,
	}
}
