package core

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ServicePreset remembers launch parameters for a (Model, Engine) combination.
type ServicePreset struct {
	Model         string   `json:"model"`
	Engine        string   `json:"engine"`
	Port          int      `json:"port,omitempty"`
	ContextSize   int64    `json:"context_size"`
	GPULayers     *int     `json:"gpu_layers,omitempty"`
	Threads       *int     `json:"threads,omitempty"`
	ExtraArgs     []string `json:"extra_args,omitempty"`
	LastStartedAt string   `json:"last_started_at,omitempty"`
}

// RecentServiceRecord aliases ServicePreset for callers referencing recent launches.
type RecentServiceRecord = ServicePreset

type servicePresetStore struct {
	Version int             `json:"version"`
	Presets []ServicePreset `json:"presets"`
}

func cloneIntPtr(v *int) *int {
	if v == nil {
		return nil
	}
	cp := *v
	return &cp
}

func sanitizeExtraArgs(raw []string) ([]string, bool) {
	if len(raw) == 0 {
		return nil, true
	}
	out := make([]string, 0, len(raw))
	for _, arg := range raw {
		if strings.ContainsRune(arg, 0) {
			return nil, false
		}
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil, true
	}
	return out, true
}

func validateStandaloneStartRequest(req StandaloneStartRequest) *ApiError {
	if strings.TrimSpace(req.Model) == "" || strings.TrimSpace(req.Engine) == "" || req.ContextSize <= 0 {
		return Err("invalid_input")
	}
	if req.Port < 0 || req.Port > 65535 {
		return Err("invalid_input")
	}
	if req.GPULayers != nil && *req.GPULayers < -1 {
		return Err("invalid_input")
	}
	if req.Threads != nil && *req.Threads <= 0 {
		return Err("invalid_input")
	}
	cleaned, ok := sanitizeExtraArgs(req.ExtraArgs)
	if !ok {
		return Err("invalid_input")
	}
	if apiErr := validateEngineArgsCredentials(cleaned); apiErr != nil {
		return apiErr
	}
	return nil
}

func replaceOrAppendFlag(args []string, names []string, flag string, value string) []string {
	match := func(token string) bool {
		for _, n := range names {
			if token == n {
				return true
			}
		}
		return false
	}
	matchPrefix := func(token string) bool {
		for _, n := range names {
			if strings.HasPrefix(token, n+"=") {
				return true
			}
		}
		return false
	}
	out := make([]string, 0, len(args)+2)
	replaced := false
	for i := 0; i < len(args); i++ {
		if match(args[i]) {
			if !replaced {
				out = append(out, flag, value)
				replaced = true
			}
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if matchPrefix(args[i]) {
			if !replaced {
				out = append(out, flag, value)
				replaced = true
			}
			continue
		}
		out = append(out, args[i])
	}
	if !replaced {
		out = append(out, flag, value)
	}
	return out
}

func applyStandaloneArgs(startup Startup, req StandaloneStartRequest) Startup {
	args := append([]string(nil), startup.Args...)
	if req.GPULayers != nil {
		args = replaceOrAppendFlag(args, []string{"--n-gpu-layers", "-ngl"}, "--n-gpu-layers", strconv.Itoa(*req.GPULayers))
	}
	if req.Threads != nil {
		args = replaceOrAppendFlag(args, []string{"--threads", "-t"}, "--threads", strconv.Itoa(*req.Threads))
	}
	if cleaned, ok := sanitizeExtraArgs(req.ExtraArgs); ok && len(cleaned) > 0 {
		args = append(args, cleaned...)
	}
	startup.Args = args
	startup.EngineName = req.Engine
	startup.ModelName = req.Model
	startup.GPULayers = cloneIntPtr(req.GPULayers)
	startup.Threads = cloneIntPtr(req.Threads)
	if cleaned, ok := sanitizeExtraArgs(req.ExtraArgs); ok {
		startup.ExtraArgs = cleaned
	}
	if served := extractServedModelFromArgs(startup.Args); served != "" {
		startup.ServedModel = served
	}
	return startup
}

func portFromEndpoint(endpoint string) int {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}

func (s *Service) presetsPath() string {
	u := s.userSettings()
	if w := workspaceRoot(u); w != nil && *w != "" {
		return filepath.Join(*w, "service-presets.json")
	}
	if s.settingsPath != "" {
		return filepath.Join(filepath.Dir(s.settingsPath), "service-presets.json")
	}
	return ""
}

func readServicePresetsFile(path string) []ServicePreset {
	if path == "" {
		return []ServicePreset{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return []ServicePreset{}
	}
	var store servicePresetStore
	if json.Unmarshal(raw, &store) != nil || len(store.Presets) == 0 {
		return []ServicePreset{}
	}
	out := make([]ServicePreset, 0, len(store.Presets))
	for _, p := range store.Presets {
		p.Model = strings.TrimSpace(p.Model)
		p.Engine = strings.TrimSpace(p.Engine)
		if p.Model == "" || p.Engine == "" {
			continue
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastStartedAt > out[j].LastStartedAt
	})
	return out
}

func writeServicePresetsFile(path string, presets []ServicePreset) error {
	if path == "" {
		return errors.New("presets path unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	sort.SliceStable(presets, func(i, j int) bool {
		return presets[i].LastStartedAt > presets[j].LastStartedAt
	})
	raw, err := json.MarshalIndent(servicePresetStore{Version: 1, Presets: presets}, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "service-presets-*.json")
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

// RecentServices returns all remembered (Model, Engine) service presets sorted by most recent first.
func (s *Service) RecentServices() []ServicePreset {
	s.presetsMu.Lock()
	defer s.presetsMu.Unlock()
	return readServicePresetsFile(s.presetsPath())
}

// ServicePresetFor looks up the remembered launch preset for a (model, engine) pair.
func (s *Service) ServicePresetFor(model, engine string) (ServicePreset, bool) {
	model = strings.TrimSpace(model)
	engine = strings.TrimSpace(engine)
	if model == "" || engine == "" {
		return ServicePreset{}, false
	}
	for _, p := range s.RecentServices() {
		if p.Model == model && p.Engine == engine {
			return p, true
		}
	}
	return ServicePreset{}, false
}

func normalizeServicePreset(preset ServicePreset) (ServicePreset, error) {
	preset.Model = strings.TrimSpace(preset.Model)
	preset.Engine = strings.TrimSpace(preset.Engine)
	if preset.Model == "" || preset.Engine == "" {
		return ServicePreset{}, errors.New("model and engine are required")
	}
	if preset.Port < 0 || preset.Port > 65535 {
		return ServicePreset{}, Err("invalid_input")
	}
	if preset.GPULayers != nil && *preset.GPULayers < -1 {
		return ServicePreset{}, Err("invalid_input")
	}
	if preset.Threads != nil && *preset.Threads <= 0 {
		return ServicePreset{}, Err("invalid_input")
	}
	cleaned, ok := sanitizeExtraArgs(preset.ExtraArgs)
	if !ok {
		return ServicePreset{}, errors.New("invalid extra_args")
	}
	if apiErr := validateEngineArgsCredentials(cleaned); apiErr != nil {
		return ServicePreset{}, apiErr
	}
	if preset.LastStartedAt == "" {
		preset.LastStartedAt = now()
	}
	preset.GPULayers = cloneIntPtr(preset.GPULayers)
	preset.Threads = cloneIntPtr(preset.Threads)
	preset.ExtraArgs = cleaned
	return preset, nil
}

// SaveServicePreset persists or updates launch parameters for a (Model, Engine) combination.
func (s *Service) SaveServicePreset(preset ServicePreset) error {
	normalized, err := normalizeServicePreset(preset)
	if err != nil {
		return err
	}
	preset = normalized

	s.presetsMu.Lock()
	defer s.presetsMu.Unlock()
	path := s.presetsPath()
	existing := readServicePresetsFile(path)
	updated := make([]ServicePreset, 0, len(existing)+1)
	replaced := false
	for _, item := range existing {
		if item.Model == preset.Model && item.Engine == preset.Engine {
			updated = append(updated, preset)
			replaced = true
		} else {
			updated = append(updated, item)
		}
	}
	if !replaced {
		updated = append(updated, preset)
	}
	return writeServicePresetsFile(path, updated)
}
