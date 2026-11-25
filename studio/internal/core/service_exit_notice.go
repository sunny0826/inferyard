package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	exitNoticeLogTailLines = 12
	// ExitHintOOM is the structured reason code indicating a likely out-of-memory exit.
	ExitHintOOM = "oom"
)

// ServiceExitNotice records an unexpected standalone service exit for UI presentation.
type ServiceExitNotice struct {
	ID          string   `json:"id"`
	Model       string   `json:"model"`
	Engine      string   `json:"engine"`
	Port        int      `json:"port,omitempty"`
	ContextSize int64    `json:"context_size,omitempty"`
	GPULayers   *int     `json:"gpu_layers,omitempty"`
	Threads     *int     `json:"threads,omitempty"`
	ExtraArgs   []string `json:"extra_args,omitempty"`
	LogPath     string   `json:"log_path,omitempty"`
	ExitedAt    string   `json:"exited_at,omitempty"`
	DetectedAt  string   `json:"detected_at,omitempty"`
	ExitCode    *int     `json:"exit_code,omitempty"`
	WhileAway   bool     `json:"while_away"`
	LogTail     []string `json:"log_tail,omitempty"`
	Hint        string   `json:"hint,omitempty"`
	Dismissed   bool     `json:"dismissed,omitempty"`
}

func exitNoticePath(workspace string) string {
	if workspace == "" {
		return ""
	}
	return filepath.Join(workspace, "service-exit-notice.json")
}

func (s *Service) exitNoticeFilePathLocked() string {
	w := workspaceRoot(s.settings)
	if w == nil || *w == "" {
		return ""
	}
	return exitNoticePath(*w)
}

func readLogTailLines(logPath string, maxLines int) []string {
	if strings.TrimSpace(logPath) == "" {
		return nil
	}
	tail, err := ReadLogTail(logPath, maxLines)
	if err != nil || strings.TrimSpace(tail) == "" {
		return nil
	}
	raw := strings.Split(tail, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	return lines
}

func detectServiceExitHint(lines []string) string {
	for _, line := range lines {
		lower := strings.ToLower(line)
		switch {
		case strings.Contains(lower, "out of memory"),
			strings.Contains(lower, "oom"),
			strings.Contains(lower, "sigkill"),
			strings.Contains(lower, "cudamalloc"),
			strings.Contains(lower, "failed to allocate"),
			strings.Contains(lower, "cannot allocate memory"),
			strings.Contains(lower, "insufficient memory"),
			strings.Contains(lower, "vk_error_out_of_device_memory"):
			return ExitHintOOM
		case strings.Contains(lower, "metal") &&
			(strings.Contains(lower, "memory") || strings.Contains(lower, "alloc") || strings.Contains(lower, "buffer")):
			return ExitHintOOM
		}
	}
	return ""
}

func writeExitNoticeFile(path string, notice ServiceExitNotice) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(notice, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "service-exit-notice-*.json")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func readExitNoticeFile(path string) *ServiceExitNotice {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var notice ServiceExitNotice
	if json.Unmarshal(raw, &notice) != nil || notice.ID == "" || notice.Dismissed {
		return nil
	}
	return &notice
}

func (s *Service) recordExitedWhileAwayLocked(workspace string, owner engineOwner) error {
	logPath := ""
	if owner.LogFile != "" {
		logPath = filepath.Join(workspace, "logs", owner.LogFile)
	}
	logTail := readLogTailLines(logPath, exitNoticeLogTailLines)
	model := owner.ModelName
	if model == "" {
		model = owner.Model
	}
	engine := owner.Engine
	if engine == "" {
		engine = owner.Family
	}
	port := owner.Port
	if port <= 0 {
		port = portFromEndpoint(owner.Endpoint)
	}
	notice := ServiceExitNotice{
		ID:          uuid(),
		Model:       model,
		Engine:      engine,
		Port:        port,
		ContextSize: owner.ContextSize,
		GPULayers:   cloneIntPtr(owner.GPULayers),
		Threads:     cloneIntPtr(owner.Threads),
		ExtraArgs:   append([]string(nil), owner.ExtraArgs...),
		LogPath:     logPath,
		DetectedAt:  now(),
		ExitCode:    nil,
		WhileAway:   true,
		LogTail:     logTail,
		Hint:        detectServiceExitHint(logTail),
		Dismissed:   false,
	}
	if err := writeExitNoticeFile(exitNoticePath(workspace), notice); err != nil {
		return err
	}
	cp := clone(notice)
	s.exitNotice = &cp
	s.Bus.Publish("service.exited", map[string]any{"notice": clone(notice)})
	return nil
}

func (s *Service) recordUnexpectedStandaloneExit(e *engineEntry, exitCode *int) {
	if normalizeEngineOrigin(e.state.Origin) != EngineOriginStandalone {
		return
	}
	logTail := readLogTailLines(e.state.LogPath, exitNoticeLogTailLines)
	model := e.state.ModelName
	if model == "" {
		model = e.state.Model
	}
	engine := e.state.EngineName
	if engine == "" {
		engine = e.state.Family
	}
	port := e.state.Port
	if port <= 0 {
		port = portFromEndpoint(e.state.Endpoint)
	}
	notice := ServiceExitNotice{
		ID:          uuid(),
		Model:       model,
		Engine:      engine,
		Port:        port,
		ContextSize: e.state.ContextSize,
		GPULayers:   cloneIntPtr(e.state.GPULayers),
		Threads:     cloneIntPtr(e.state.Threads),
		ExtraArgs:   append([]string(nil), e.state.ExtraArgs...),
		LogPath:     e.state.LogPath,
		ExitedAt:    now(),
		ExitCode:    cloneIntPtr(exitCode),
		WhileAway:   false,
		LogTail:     logTail,
		Hint:        detectServiceExitHint(logTail),
		Dismissed:   false,
	}
	s.mu.Lock()
	path := s.exitNoticeFilePathLocked()
	_ = writeExitNoticeFile(path, notice)
	cp := clone(notice)
	s.exitNotice = &cp
	s.Bus.Publish("service.exited", map[string]any{"notice": clone(notice)})
	s.mu.Unlock()
}

func (s *Service) clearExitNoticeLocked() {
	s.exitNotice = nil
	if path := s.exitNoticeFilePathLocked(); path != "" {
		_ = os.Remove(path)
	}
}

// ExitedServiceNotice returns the latest undismissed standalone exit notice, or nil.
func (s *Service) ExitedServiceNotice() *ServiceExitNotice {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exitNotice != nil && !s.exitNotice.Dismissed {
		cp := clone(*s.exitNotice)
		return &cp
	}
	loaded := readExitNoticeFile(s.exitNoticeFilePathLocked())
	if loaded == nil {
		return nil
	}
	s.exitNotice = loaded
	cp := clone(*loaded)
	return &cp
}

// DismissExitedServiceNotice clears the persisted standalone exit notice.
func (s *Service) DismissExitedServiceNotice() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exitNotice = nil
	path := s.exitNoticeFilePathLocked()
	if path != "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	s.Bus.Publish("service.exit_notice_cleared", map[string]any{})
	return nil
}
