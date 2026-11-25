package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type StandaloneStartRequest struct {
	Model       string   `json:"model"`
	Engine      string   `json:"engine"`
	ContextSize int64    `json:"context_size"`
	Port        int      `json:"port,omitempty"`
	LANAccess   bool     `json:"lan_access,omitempty"`
	GPULayers   *int     `json:"gpu_layers,omitempty"`
	Threads     *int     `json:"threads,omitempty"`
	ExtraArgs   []string `json:"extra_args,omitempty"`
}

func sanitizeSlugPart(raw string, limit int) string {
	var b strings.Builder
	prevDash := false
	for _, r := range raw {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if isAlphaNum || r == '_' {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
		if b.Len() >= limit {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "item"
	}
	return out
}

func standaloneSlug(model, engine string, contextSize int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", model, engine, contextSize)))
	tag := hex.EncodeToString(sum[:4])
	return fmt.Sprintf("%s-%s-%d-%s", sanitizeSlugPart(model, 32), sanitizeSlugPart(engine, 24), contextSize, tag)
}

func servePlanToStartup(plan inferyard.ServePlan) Startup {
	args := []string{}
	if len(plan.Argv) > 1 {
		args = append(args, plan.Argv[1:]...)
	}
	result := Startup{
		Kind:             "direct_process",
		Path:             plan.Argv[0],
		Args:             args,
		Family:           plan.Family,
		Adapter:          plan.Adapter,
		Local:            plan.Local,
		WorkingDirectory: plan.WorkingDirectory,
		Environment:      plan.Environment,
		Endpoint:         plan.Endpoint,
		ModelPath:        plan.ModelPath,
		ModelKind:        plan.ModelKind,
		ServedModel:      extractServedModelFromArgs(args),
		ContextSize:      plan.ContextSize,
	}
	for _, arg := range result.Args {
		if arg == "--no-webui" || strings.HasPrefix(arg, "--no-webui=") {
			result.WebUIDisabled = true
			break
		}
	}
	return result
}

func normalizedPath(p string) string {
	if p == "" {
		return ""
	}
	if eval, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(eval)
	}
	return filepath.Clean(p)
}

func expectedEngineExecutable(eng registryEntry) (string, bool) {
	if eng.installation != "" {
		raw, err := os.ReadFile(eng.installation)
		if err != nil {
			return "", false
		}
		var decl struct {
			Executable string `json:"executable"`
		}
		if json.Unmarshal(raw, &decl) != nil || decl.Executable == "" {
			return "", false
		}
		return decl.Executable, true
	}
	if runtime.GOOS == "windows" {
		if eng.receipt == "" {
			return "", false
		}
		raw, err := os.ReadFile(eng.receipt)
		if err != nil {
			return "", false
		}
		var rec struct {
			EngineBinary string `json:"engine_binary"`
		}
		if json.Unmarshal(raw, &rec) != nil || rec.EngineBinary == "" {
			return "", false
		}
		return filepath.Join(filepath.Dir(eng.receipt), rec.EngineBinary), true
	}
	if eng.path == "" {
		return "", false
	}
	return eng.path, true
}

func safeServeDir(workspace, slug string) (string, *ApiError) {
	serveRoot := filepath.Join(workspace, "serve")
	if info, err := os.Lstat(serveRoot); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", Err("invalid_input")
		}
	} else if !os.IsNotExist(err) {
		return "", Err("invalid_input")
	}
	rawTarget := filepath.Join(serveRoot, slug)
	if info, err := os.Lstat(rawTarget); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", Err("invalid_input")
		}
	} else if !os.IsNotExist(err) {
		return "", Err("invalid_input")
	}
	resolved, err := Resolve(workspace, filepath.Join("serve", slug))
	if err != nil {
		return "", Err("invalid_input")
	}
	return resolved, nil
}

func isRegularNonSymlinkFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func (s *Service) StartStandalone(ctx context.Context, req StandaloneStartRequest) (EngineState, *ApiError) {
	req.Model = strings.TrimSpace(req.Model)
	req.Engine = strings.TrimSpace(req.Engine)
	if apiErr := validateStandaloneStartRequest(req); apiErr != nil {
		return EngineState{}, apiErr
	}
	if ctx.Err() != nil {
		return EngineState{}, Err("cancelled")
	}

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if ctx.Err() != nil {
		return EngineState{}, Err("cancelled")
	}

	s.mu.Lock()
	closing := s.closing
	deleting := s.deletingAssets > 0
	runsActive := s.runsActiveLocked()
	s.mu.Unlock()
	if closing {
		return EngineState{}, Err("service_closing")
	}
	if deleting {
		return EngineState{}, workspaceBusyError()
	}
	if runsActive {
		return EngineState{}, Err("run_active")
	}

	u := s.userSettings()
	w := workspaceRoot(u)
	if w == nil {
		return EngineState{}, Conflict("settings_incomplete")
	}

	reg := s.registry()
	var modelEntry *registryEntry
	for i := range reg.models {
		if reg.models[i].model.Name == req.Model {
			modelEntry = &reg.models[i]
			break
		}
	}
	if modelEntry == nil {
		return EngineState{}, Err("not_found")
	}
	if !modelEntry.model.Available {
		return EngineState{}, Err("model_unavailable")
	}
	var engEntry *registryEntry
	for i := range reg.engines {
		if reg.engines[i].engine.Name == req.Engine {
			engEntry = &reg.engines[i]
			break
		}
	}
	if engEntry == nil {
		return EngineState{}, Err("not_found")
	}

	slug := standaloneSlug(req.Model, req.Engine, req.ContextSize)
	serviceID := ServiceIDFor(req.Model, req.Engine)

	// Check active running state for this serviceID first so same-combination idempotency never touches CLI.
	s.mu.Lock()
	if s.orphan != nil {
		state := clone(s.orphan.state)
		s.mu.Unlock()
		return state, Err("engine_orphaned")
	}
	if orphan, exists := s.orphanedServices[serviceID]; exists && orphan != nil {
		state := clone(orphan.state)
		s.mu.Unlock()
		return state, Err("engine_orphaned")
	}
	if existing, exists := s.services[serviceID]; exists && existing != nil && isActiveServiceState(existing.state.State) {
		state := clone(existing.state)
		s.mu.Unlock()
		if state.Candidate == slug && state.LANAccess == req.LANAccess {
			runningPort := state.Port
			if runningPort <= 0 {
				runningPort = portFromEndpoint(state.Endpoint)
			}
			if req.Port == 0 || req.Port == runningPort {
				return state, nil
			}
		}
		return state, Err("engine_candidate_mismatch")
	}
	occupied := s.occupiedPortsLocked()
	s.mu.Unlock()

	budget := s.checkServiceMemoryBudgetWithRegistry(req.Model, req.Engine, req.ContextSize, req.GPULayers, reg)
	if budget.Known && !budget.Enough {
		return EngineState{}, &ApiError{
			Code: "insufficient_memory",
			Message: l10n.TRf("core.engine.err.insufficient_memory",
				"required", fmt.Sprintf("%.1f", budget.EstimatedGB),
				"remaining", fmt.Sprintf("%.1f", budget.RemainingGB),
			),
			Details: map[string]any{
				"required_gb":  budget.EstimatedGB,
				"remaining_gb": budget.RemainingGB,
				"available_gb": budget.AvailableGB,
				"used_gb":      budget.UsedGB,
			},
		}
	}

	bindHost := "127.0.0.1"
	if req.LANAccess {
		bindHost = "0.0.0.0"
	}
	if req.Port > 0 {
		if occupied[req.Port] || checkBindPortAvailable(bindHost, req.Port) != nil {
			return EngineState{}, portInUseError(req.Port)
		}
	}

	serveDir, dirErr := safeServeDir(*w, slug)
	if dirErr != nil {
		return EngineState{}, dirErr
	}
	planPath := filepath.Join(serveDir, "serve-plan.json")
	reservedPort := 0
	defer func() {
		if reservedPort > 0 {
			s.mu.Lock()
			delete(s.reservedPorts, reservedPort)
			s.mu.Unlock()
		}
	}()

	var startup Startup
	reused := false
	if cached, _, cachedPort, ok := loadCachedStandalonePlan(planPath, modelEntry.path, *engEntry, req.ContextSize); ok {
		if (req.Port == 0 || req.Port == cachedPort) && s.tryReservePort(bindHost, cachedPort) {
			reservedPort = cachedPort
			startup = cached
			reused = true
		} else if cached.Family != "polystrata" {
			targetPort, portErr := s.reserveStandalonePort(bindHost, req.Port)
			if portErr != nil {
				return EngineState{}, portErr
			}
			reservedPort = targetPort
			if rewritten, ok := rewriteStartupPort(cached, targetPort); ok {
				startup = rewritten
				reused = true
			}
		}
	}
	if !reused {
		cliPath, cliErr := s.resolveServeCLI(ctx, u)
		if cliErr != nil {
			return EngineState{}, cliErr
		}
		if ctx.Err() != nil {
			return EngineState{}, Err("cancelled")
		}
		port := reservedPort
		if port == 0 {
			var portErr *ApiError
			port, portErr = s.reserveStandalonePort(bindHost, req.Port)
			if portErr != nil {
				return EngineState{}, portErr
			}
			reservedPort = port
		}
		args := []string{
			"serve", "prepare",
			"--model", modelEntry.path,
			"--context-size", strconv.FormatInt(req.ContextSize, 10),
			"--port", strconv.Itoa(port),
			"--out", serveDir,
			"--json",
		}
		if engEntry.installation != "" {
			args = append(args, "--installation", engEntry.installation)
		} else if runtime.GOOS == "windows" {
			if engEntry.receipt == "" {
				return EngineState{}, Err("runtime_receipt_missing")
			}
			args = append(args, "--runtime-receipt", engEntry.receipt)
		} else {
			args = append(args, "--engine", engEntry.path)
		}
		out, execErr := s.exec(ctx, cliPath, args...)
		if ctx.Err() != nil {
			return EngineState{}, Err("cancelled")
		}
		if execErr != nil || out.ExitCode != 0 {
			if parsed, parseErr := inferyard.Parse(out.Stdout); parseErr == nil && len(parsed.Limitations) > 0 {
				return EngineState{}, Err(parsed.Limitations[0])
			}
			return EngineState{}, Err("cli_error")
		}
		if !isRegularNonSymlinkFile(planPath) {
			return EngineState{}, Err("cli_error")
		}
		raw, readErr := os.ReadFile(planPath)
		if readErr != nil {
			return EngineState{}, Err("cli_error")
		}
		plan, parseErr := inferyard.ParseServePlan(raw)
		if parseErr != nil {
			return EngineState{}, Err("cli_error")
		}
		startup = servePlanToStartup(plan)
	}
	if ctx.Err() != nil {
		return EngineState{}, Err("cancelled")
	}

	startup = applyStandaloneArgs(startup, req)
	startup, lanErr := applyLANAccess(startup, serveDir, req.LANAccess)
	if lanErr != nil {
		return EngineState{}, lanErr
	}
	startup.ServiceID = serviceID
	startup.EstimatedMemoryGB = budget.EstimatedGB
	state, apiErr := s.startManagedServiceLocked(startup, slug, serviceID)
	if apiErr != nil {
		return state, apiErr
	}

	s.mu.Lock()
	s.clearExitNoticesForServiceLocked(serviceID, req.Model, req.Engine)
	s.mu.Unlock()

	_ = s.SaveServicePreset(ServicePreset{
		Model:         req.Model,
		Engine:        req.Engine,
		Port:          req.Port,
		LANAccess:     req.LANAccess,
		ContextSize:   req.ContextSize,
		GPULayers:     cloneIntPtr(req.GPULayers),
		Threads:       cloneIntPtr(req.Threads),
		ExtraArgs:     append([]string(nil), startup.ExtraArgs...),
		LastStartedAt: now(),
	})
	return state, nil
}
