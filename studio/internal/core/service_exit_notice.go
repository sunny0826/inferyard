package core

import (
	"bytes"
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
	ID            string   `json:"id"`
	ServiceID     string   `json:"service_id,omitempty"`
	Model         string   `json:"model"`
	Engine        string   `json:"engine"`
	Port          int      `json:"port,omitempty"`
	RequestedPort int      `json:"requested_port,omitempty"`
	LANAccess     bool     `json:"lan_access,omitempty"`
	ContextSize   int64    `json:"context_size,omitempty"`
	GPULayers     *int     `json:"gpu_layers,omitempty"`
	Threads       *int     `json:"threads,omitempty"`
	ExtraArgs     []string `json:"extra_args,omitempty"`
	LogPath       string   `json:"log_path,omitempty"`
	ExitedAt      string   `json:"exited_at,omitempty"`
	DetectedAt    string   `json:"detected_at,omitempty"`
	ExitCode      *int     `json:"exit_code,omitempty"`
	WhileAway     bool     `json:"while_away"`
	LogTail       []string `json:"log_tail,omitempty"`
	Hint          string   `json:"hint,omitempty"`
	Dismissed     bool     `json:"dismissed,omitempty"`
}

func exitNoticePath(workspace string) string {
	if workspace == "" {
		return ""
	}
	return filepath.Join(workspace, "service-exit-notices.json")
}

func legacyExitNoticePath(workspace string) string {
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

func (s *Service) legacyExitNoticeFilePathLocked() string {
	w := workspaceRoot(s.settings)
	if w == nil || *w == "" {
		return ""
	}
	return legacyExitNoticePath(*w)
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
	return writeExitNoticesFile(path, []ServiceExitNotice{notice})
}

func writeExitNoticesFile(path string, notices []ServiceExitNotice) error {
	if path == "" {
		return nil
	}
	if len(notices) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(notices, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "service-exit-notices-*.json")
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

func parseExitNoticesBytes(raw []byte) []ServiceExitNotice {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	var list []ServiceExitNotice
	if trimmed[0] == '[' {
		if json.Unmarshal(trimmed, &list) != nil {
			return nil
		}
	} else {
		var single ServiceExitNotice
		if json.Unmarshal(trimmed, &single) != nil {
			return nil
		}
		list = []ServiceExitNotice{single}
	}
	out := make([]ServiceExitNotice, 0, len(list))
	for _, item := range list {
		if item.ID == "" || item.Dismissed {
			continue
		}
		out = append(out, item)
	}
	return out
}

func readExitNoticesFromWorkspace(workspace string) []ServiceExitNotice {
	if workspace == "" {
		return nil
	}
	if raw, err := os.ReadFile(exitNoticePath(workspace)); err == nil {
		return parseExitNoticesBytes(raw)
	}
	if raw, err := os.ReadFile(legacyExitNoticePath(workspace)); err == nil {
		return parseExitNoticesBytes(raw)
	}
	return nil
}

func (s *Service) ensureExitNoticesLoadedLocked() {
	if s.exitNoticesLoaded {
		return
	}
	w := workspaceRoot(s.settings)
	if w != nil && *w != "" {
		s.exitNotices = readExitNoticesFromWorkspace(*w)
	}
	s.exitNoticesLoaded = true
}

func (s *Service) appendExitNoticeLocked(workspace string, notice ServiceExitNotice) error {
	s.ensureExitNoticesLoadedLocked()
	updated := make([]ServiceExitNotice, 0, len(s.exitNotices)+1)
	updated = append(updated, clone(notice))
	for _, existing := range s.exitNotices {
		if existing.ID == notice.ID {
			continue
		}
		updated = append(updated, existing)
	}
	s.exitNotices = updated
	s.exitNoticesLoaded = true
	if workspace == "" {
		return nil
	}
	return writeExitNoticesFile(exitNoticePath(workspace), updated)
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
		ID:            uuid(),
		ServiceID:     owner.ServiceID,
		Model:         model,
		Engine:        engine,
		Port:          port,
		RequestedPort: owner.RequestedPort,
		LANAccess:     owner.LANAccess,
		ContextSize:   owner.ContextSize,
		GPULayers:     cloneIntPtr(owner.GPULayers),
		Threads:       cloneIntPtr(owner.Threads),
		ExtraArgs:     append([]string(nil), owner.ExtraArgs...),
		LogPath:       logPath,
		DetectedAt:    now(),
		ExitCode:      nil,
		WhileAway:     true,
		LogTail:       logTail,
		Hint:          detectServiceExitHint(logTail),
		Dismissed:     false,
	}
	if err := s.appendExitNoticeLocked(workspace, notice); err != nil {
		return err
	}
	s.Bus.Publish("service.exited", map[string]any{"notice": clone(notice)})
	return nil
}

func (s *Service) recordUnexpectedStandaloneExit(e *engineEntry, exitCode *int) error {
	if normalizeEngineOrigin(e.state.Origin) != EngineOriginStandalone {
		return nil
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
		ID:            uuid(),
		ServiceID:     e.state.ServiceID,
		Model:         model,
		Engine:        engine,
		Port:          port,
		RequestedPort: e.state.RequestedPort,
		LANAccess:     e.state.LANAccess,
		ContextSize:   e.state.ContextSize,
		GPULayers:     cloneIntPtr(e.state.GPULayers),
		Threads:       cloneIntPtr(e.state.Threads),
		ExtraArgs:     append([]string(nil), e.state.ExtraArgs...),
		LogPath:       e.state.LogPath,
		ExitedAt:      now(),
		ExitCode:      cloneIntPtr(exitCode),
		WhileAway:     false,
		LogTail:       logTail,
		Hint:          detectServiceExitHint(logTail),
		Dismissed:     false,
	}
	s.mu.Lock()
	var persistErr error
	if w := workspaceRoot(s.settings); w != nil && *w != "" {
		persistErr = s.appendExitNoticeLocked(*w, notice)
	} else {
		persistErr = s.appendExitNoticeLocked("", notice)
	}
	s.Bus.Publish("service.exited", map[string]any{"notice": clone(notice)})
	s.mu.Unlock()
	return persistErr
}

func (s *Service) clearExitNoticesForServiceLocked(serviceID, model, engine string) {
	s.ensureExitNoticesLoadedLocked()
	if len(s.exitNotices) == 0 {
		return
	}
	remaining := make([]ServiceExitNotice, 0, len(s.exitNotices))
	for _, n := range s.exitNotices {
		if (serviceID != "" && n.ServiceID == serviceID) || (model != "" && engine != "" && n.Model == model && n.Engine == engine) {
			continue
		}
		remaining = append(remaining, n)
	}
	if len(remaining) == len(s.exitNotices) {
		return
	}
	s.exitNotices = remaining
	if path := s.exitNoticeFilePathLocked(); path != "" {
		_ = writeExitNoticesFile(path, remaining)
	}
	if len(remaining) == 0 {
		if legacy := s.legacyExitNoticeFilePathLocked(); legacy != "" {
			_ = os.Remove(legacy)
		}
	}
}

// ExitedServiceNotices returns all undismissed standalone exit notices, newest first.
func (s *Service) ExitedServiceNotices() []ServiceExitNotice {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureExitNoticesLoadedLocked()
	if len(s.exitNotices) == 0 {
		return []ServiceExitNotice{}
	}
	out := make([]ServiceExitNotice, len(s.exitNotices))
	for i, n := range s.exitNotices {
		out[i] = clone(n)
	}
	return out
}

// ExitedServiceNotice returns the latest undismissed standalone exit notice, or nil.
func (s *Service) ExitedServiceNotice() *ServiceExitNotice {
	notices := s.ExitedServiceNotices()
	if len(notices) == 0 {
		return nil
	}
	cp := clone(notices[0])
	return &cp
}

// DismissExitedServiceNoticeByID dismisses a single persisted exit notice by ID.
func (s *Service) DismissExitedServiceNoticeByID(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureExitNoticesLoadedLocked()
	remaining := make([]ServiceExitNotice, 0, len(s.exitNotices))
	for _, n := range s.exitNotices {
		if n.ID == id {
			continue
		}
		remaining = append(remaining, n)
	}
	path := s.exitNoticeFilePathLocked()
	if path != "" {
		if err := writeExitNoticesFile(path, remaining); err != nil {
			return err
		}
	}
	if len(remaining) == 0 {
		if legacy := s.legacyExitNoticeFilePathLocked(); legacy != "" {
			if err := os.Remove(legacy); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	s.exitNotices = remaining
	s.Bus.Publish("service.exit_notice_cleared", map[string]any{"id": id})
	return nil
}

// DismissExitedServiceNotice clears all persisted standalone exit notices.
func (s *Service) DismissExitedServiceNotice() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exitNotices = nil
	s.exitNoticesLoaded = true
	if path := s.exitNoticeFilePathLocked(); path != "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if legacy := s.legacyExitNoticeFilePathLocked(); legacy != "" {
		if err := os.Remove(legacy); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	s.Bus.Publish("service.exit_notice_cleared", map[string]any{})
	return nil
}
