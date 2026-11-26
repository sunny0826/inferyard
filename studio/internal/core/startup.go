package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Startup struct {
	Path             string            `json:"path"`
	Args             []string          `json:"startup_args"`
	Kind             string            `json:"kind"`
	Family           string            `json:"family"`
	Adapter          string            `json:"adapter,omitempty"`
	Local            bool              `json:"local"`
	WorkingDirectory string            `json:"working_directory"`
	Environment      map[string]string `json:"environment"`
	Endpoint         string            `json:"endpoint"`
	ModelPath        string            `json:"model_path,omitempty"`
	ModelKind        string            `json:"model_kind,omitempty"`
	ContextSize      int64             `json:"context_size,omitempty"`
	WebUIDisabled    bool              `json:"webui_disabled,omitempty"`
	EngineName        string            `json:"engine_name,omitempty"`
	ModelName         string            `json:"model_name,omitempty"`
	ServedModel       string            `json:"served_model,omitempty"`
	ServiceID         string            `json:"service_id,omitempty"`
	RequestedPort     int               `json:"requested_port,omitempty"`
	LANAccess         bool              `json:"lan_access,omitempty"`
	EstimatedMemoryGB float64           `json:"estimated_memory_gb,omitempty"`
	GPULayers         *int              `json:"gpu_layers,omitempty"`
	Threads           *int              `json:"threads,omitempty"`
	ExtraArgs         []string          `json:"extra_args,omitempty"`
}
type ProcessCandidate struct {
	PID  int64  `json:"pid"`
	Name string `json:"name"`
}

func (s *Service) Startup(candidate string) (Startup, *ApiError) {
	description, failure := s.DescribeCandidate(s.ctx, candidate)
	if failure != nil {
		return Startup{}, failure
	}
	return startupFromDescription(description), nil
}

// StartupCached resolves candidate startup metadata using the fingerprinted display
// cache. Use this only for read-only UI refresh; execution and admission paths must
// call Startup or DescribeCandidate directly so CLI asset manifest checks run.
func (s *Service) StartupCached(candidate string) (Startup, *ApiError) {
	description, failure := s.describeCandidateCached(s.ctx, candidate)
	if failure != nil {
		return Startup{}, failure
	}
	return startupFromDescription(description), nil
}

func startupFromDescription(description inferyard.CandidateDescription) Startup {
	launch := description.Launch
	modelPath := description.Assets.Model.Path
	if modelPath == "" {
		modelPath = launch.ServedModel
	}
	result := Startup{
		Kind:             launch.Kind,
		Family:           description.Family,
		Adapter:          description.Adapter,
		Local:            description.Definition == "local-engine-description.v2",
		WorkingDirectory: str(launch.WorkingDirectory),
		Environment:      launch.Environment,
		Endpoint:         description.Endpoint.URL,
		ModelPath:        modelPath,
		ModelKind:        description.Assets.Model.Kind,
		ServedModel:      strings.TrimSpace(launch.ServedModel),
		ContextSize:      description.Conditions.ContextSize,
		Args:             []string{},
	}
	if launch.Kind == "direct_process" {
		result.Path, result.Args = launch.Argv[0], launch.Argv[1:]
	}
	if result.ServedModel == "" {
		result.ServedModel = extractServedModelFromArgs(result.Args)
	}
	for _, arg := range result.Args {
		if arg == "--no-webui" || strings.HasPrefix(arg, "--no-webui=") {
			result.WebUIDisabled = true
			break
		}
	}
	return result
}

func extractServedModelFromArgs(args []string) string {
	served := ""
	configPath := ""
	for i := 0; i < len(args); i++ {
		key, value, inline := strings.Cut(args[i], "=")
		switch key {
		case "--alias", "-a", "--served-model-name":
			if !inline && i+1 < len(args) {
				value = args[i+1]
				i++
			}
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				served = trimmed
			}
		case "--config":
			if !inline && i+1 < len(args) {
				value = args[i+1]
				i++
			}
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				configPath = trimmed
			}
		}
	}
	if served != "" {
		return served
	}
	if configPath != "" {
		return extractServedModelFromConfig(configPath)
	}
	return ""
}

func extractServedModelFromConfig(path string) string {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg struct {
		ModelName string `json:"model_name"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return ""
	}
	return strings.TrimSpace(cfg.ModelName)
}
func (s *Service) Processes(ctx context.Context) ([]ProcessCandidate, *ApiError) {
	result := []ProcessCandidate{}
	if runtime.GOOS == "windows" {
		raw, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Get-Process | Select-Object Id,ProcessName | ConvertTo-Json -Compress").Output()
		if err != nil {
			return nil, Err("internal_error")
		}
		var entries []struct {
			ID   int64  `json:"Id"`
			Name string `json:"ProcessName"`
		}
		if json.Unmarshal(raw, &entries) != nil {
			return nil, Err("internal_error")
		}
		for _, p := range entries {
			if p.ID > 0 {
				result = append(result, ProcessCandidate{p.ID, p.Name})
			}
		}
	} else {
		raw, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,comm=").Output()
		if err != nil {
			return nil, Err("internal_error")
		}
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			pid, err := strconv.ParseInt(fields[0], 10, 64)
			if err == nil && pid > 0 {
				result = append(result, ProcessCandidate{pid, filepath.Base(strings.Join(fields[1:], " "))})
			}
		}
	}
	return result, nil
}
