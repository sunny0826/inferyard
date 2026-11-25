package core

import (
	"context"
	"math"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultRuntimeMemSampleInterval = 4 * time.Second
	runtimeMemCommandTimeout        = 2 * time.Second
	kibPerGiB                       = 1024.0 * 1024.0
)

var sampleProcessTreeRSSFunc = defaultSampleProcessTreeRSSGB

// sampleProcessTreeRSSGB returns the total resident set size (RSS) in GiB across the
// given process IDs (rounded to 2 decimal places), or nil if sampling fails, no PIDs
// are alive, or the platform is unsupported. Missing values are never coerced to 0.
func sampleProcessTreeRSSGB(pids []int) *float64 {
	return sampleProcessTreeRSSFunc(pids)
}

func defaultSampleProcessTreeRSSGB(pids []int) *float64 {
	if runtime.GOOS == "windows" {
		return nil
	}
	clean := uniquePositivePIDs(pids)
	if len(clean) == 0 {
		return nil
	}
	parts := make([]string, len(clean))
	for i, pid := range clean {
		parts[i] = strconv.Itoa(pid)
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeMemCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ps", "-o", "rss=", "-p", strings.Join(parts, ","))
	var out boundedProcessOutput
	cmd.Stdout = &out
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil
	}
	if gb := parsePSRSSOutputGB(out.Bytes()); gb != nil {
		return gb
	}
	if err != nil {
		return nil
	}
	return nil
}

func parsePSRSSOutputGB(output []byte) *float64 {
	var totalKB int64
	found := false
	for _, line := range strings.Split(string(output), "\n") {
		field := strings.TrimSpace(line)
		if field == "" {
			continue
		}
		kb, err := strconv.ParseInt(field, 10, 64)
		if err != nil || kb < 0 {
			continue
		}
		if kb > 0 {
			totalKB += kb
			found = true
		}
	}
	if !found || totalKB <= 0 {
		return nil
	}
	rawGB := float64(totalKB) / kibPerGiB
	rounded := math.Round(rawGB*100) / 100
	if rounded <= 0 {
		rounded = 0.01
	}
	return &rounded
}

func uniquePositivePIDs(pids []int) []int {
	if len(pids) == 0 {
		return nil
	}
	seen := make(map[int]bool, len(pids))
	out := make([]int, 0, len(pids))
	for _, pid := range pids {
		if pid <= 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		out = append(out, pid)
	}
	return out
}

// entryProcessPIDsLocked returns the immutable PID snapshot for e. Caller must hold s.mu.
func entryProcessPIDsLocked(e *engineEntry) []int {
	if e == nil {
		return nil
	}
	if len(e.pidSnapshot) > 0 {
		return e.pidSnapshot
	}
	if e.state.Pid > 0 {
		return []int{int(e.state.Pid)}
	}
	return nil
}

func (s *Service) runtimeMemPeriod() time.Duration {
	if s.runtimeMemInterval > 0 {
		return s.runtimeMemInterval
	}
	return defaultRuntimeMemSampleInterval
}

func (s *Service) maybeSampleRuntimeMemory(e *engineEntry, lastSample *time.Time, force bool) {
	if e == nil {
		return
	}
	if !force && lastSample != nil && !lastSample.IsZero() && time.Since(*lastSample) < s.runtimeMemPeriod() {
		return
	}
	s.mu.Lock()
	running := e.state.State == "running"
	pids := entryProcessPIDsLocked(e)
	s.mu.Unlock()
	if !running || len(pids) == 0 {
		return
	}
	sampled := sampleProcessTreeRSSGB(pids)
	if lastSample != nil {
		*lastSample = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.state.State != "running" {
		return
	}
	if equalFloatPtr(e.state.RuntimeMemoryGB, sampled) {
		return
	}
	e.state.RuntimeMemoryGB = sampled
	s.Bus.Publish("services.memory", map[string]any{
		"services": s.managedServicesLocked(),
		"engine":   clone(e.state),
	})
}

func equalFloatPtr(a, b *float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
