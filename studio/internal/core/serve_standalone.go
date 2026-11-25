package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/inferyard"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type StandaloneStartRequest struct {
	Model       string `json:"model"`
	Engine      string `json:"engine"`
	ContextSize int64  `json:"context_size"`
}

func (s *Service) configuredCLIPath(u UserSettings) string {
	path := str(u.InferyardPath)
	if s.paths != nil {
		path = s.paths.CLI
	}
	if path == "" {
		path = "inferyard"
	}
	return path
}

func (s *Service) clearServeSupport() {
	s.probeMu.Lock()
	s.serveProbePath = ""
	s.serveProbeVersion = ""
	s.serveProbeSize = 0
	s.serveProbeModTime = 0
	s.serveProbeChecked = time.Time{}
	s.serveSupported = false
	s.probeMu.Unlock()
}

func (s *Service) ServeSupported(ctx context.Context) bool {
	u := s.userSettings()
	rawPath := s.configuredCLIPath(u)
	found, lookErr := exec.LookPath(rawPath)
	if lookErr != nil {
		s.clearServeSupport()
		return false
	}
	absPath, _ := filepath.Abs(found)
	st, statErr := os.Stat(absPath)
	if statErr == nil {
		s.probeMu.Lock()
		cached := !s.serveProbeChecked.IsZero() &&
			s.serveProbePath == absPath &&
			s.serveProbeVersion == Version &&
			s.serveProbeSize == st.Size() &&
			s.serveProbeModTime == st.ModTime().UnixNano() &&
			time.Since(s.serveProbeChecked) < probeTTL
		ok := s.serveSupported
		s.probeMu.Unlock()
		if cached {
			return ok
		}
	}
	p := s.probe(u)
	if p.Status != "ok" || p.Path == nil || *p.Path == "" {
		if statErr == nil && ctx.Err() == nil {
			s.probeMu.Lock()
			s.serveProbePath = absPath
			s.serveProbeVersion = Version
			s.serveProbeSize = st.Size()
			s.serveProbeModTime = st.ModTime().UnixNano()
			s.serveProbeChecked = time.Now()
			s.serveSupported = false
			s.probeMu.Unlock()
		} else {
			s.clearServeSupport()
		}
		return false
	}
	return s.checkServeSupported(ctx, *p.Path)
}

func (s *Service) checkServeSupported(ctx context.Context, cliPath string) bool {
	st, statErr := os.Stat(cliPath)
	if statErr == nil {
		s.probeMu.Lock()
		cached := !s.serveProbeChecked.IsZero() &&
			s.serveProbePath == cliPath &&
			s.serveProbeVersion == Version &&
			s.serveProbeSize == st.Size() &&
			s.serveProbeModTime == st.ModTime().UnixNano() &&
			time.Since(s.serveProbeChecked) < probeTTL
		ok := s.serveSupported
		s.probeMu.Unlock()
		if cached {
			return ok
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := s.exec(probeCtx, cliPath, "--schema", "serve_plan")
	supported := err == nil && out.ExitCode == 0 && inferyard.SupportsServePlanSchema(out.Stdout)
	if probeCtx.Err() == nil && statErr == nil {
		s.probeMu.Lock()
		s.serveProbePath = cliPath
		s.serveProbeVersion = Version
		s.serveProbeSize = st.Size()
		s.serveProbeModTime = st.ModTime().UnixNano()
		s.serveProbeChecked = time.Now()
		s.serveSupported = supported
		s.probeMu.Unlock()
	}
	return supported
}

// resolveServeCLI verifies the CLI supports standalone serve; when the binary
// fingerprint matches a previously confirmed positive probe, it skips spawning
// CLI subprocesses even after probeTTL expires.
func (s *Service) resolveServeCLI(ctx context.Context, u UserSettings) (string, *ApiError) {
	rawPath := s.configuredCLIPath(u)
	found, err := exec.LookPath(rawPath)
	if err != nil {
		s.clearServeSupport()
		return "", Err("cli_unavailable")
	}
	absPath, _ := filepath.Abs(found)
	if st, statErr := os.Stat(absPath); statErr == nil {
		s.probeMu.Lock()
		fingerprintOK := s.serveSupported &&
			s.serveProbePath == absPath &&
			s.serveProbeVersion == Version &&
			s.serveProbeSize == st.Size() &&
			s.serveProbeModTime == st.ModTime().UnixNano()
		s.probeMu.Unlock()
		if fingerprintOK {
			return absPath, nil
		}
	}
	p := s.probe(u)
	if p.Status != "ok" || p.Path == nil || *p.Path == "" {
		s.clearServeSupport()
		return "", cliProbeError(p)
	}
	if !s.checkServeSupported(ctx, *p.Path) {
		if ctx.Err() != nil {
			return "", Err("cancelled")
		}
		return "", cliOutdatedError(p.Version)
	}
	return *p.Path, nil
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

func reusableStandalonePlan(planPath string, modelPath string, eng registryEntry, contextSize int64) (Startup, bool) {
	if !isRegularNonSymlinkFile(planPath) {
		return Startup{}, false
	}
	raw, err := os.ReadFile(planPath)
	if err != nil {
		return Startup{}, false
	}
	plan, err := inferyard.ParseServePlan(raw)
	if err != nil {
		return Startup{}, false
	}
	wantLocal := eng.installation != ""
	if plan.Local != wantLocal || plan.ContextSize != contextSize {
		return Startup{}, false
	}
	if normalizedPath(plan.ModelPath) != normalizedPath(modelPath) {
		return Startup{}, false
	}
	if wantLocal && eng.family != "" && plan.Family != eng.family {
		return Startup{}, false
	}
	wantExe, ok := expectedEngineExecutable(eng)
	if !ok {
		return Startup{}, false
	}
	// Preserve lexical venv Python symlinks for local installations so two venvs sharing a base interpreter do not collide.
	if wantLocal {
		if filepath.Clean(plan.Argv[0]) != filepath.Clean(wantExe) {
			return Startup{}, false
		}
	} else if normalizedPath(plan.Argv[0]) != normalizedPath(wantExe) {
		return Startup{}, false
	}
	if info, err := os.Stat(plan.Argv[0]); err != nil || info.IsDir() {
		return Startup{}, false
	}
	if plan.Family == "polystrata" {
		cfgFound := false
		for i := 1; i+1 < len(plan.Argv); i++ {
			if plan.Argv[i] == "--config" {
				if !isRegularNonSymlinkFile(plan.Argv[i+1]) {
					return Startup{}, false
				}
				cfgFound = true
				break
			}
		}
		if !cfgFound {
			return Startup{}, false
		}
	}
	startup := servePlanToStartup(plan)
	host, port, apiErr := validateStartup(startup)
	if apiErr != nil {
		return Startup{}, false
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return Startup{}, false
	}
	_ = ln.Close()
	return startup, true
}

func (s *Service) StartStandalone(ctx context.Context, req StandaloneStartRequest) (EngineState, *ApiError) {
	req.Model = strings.TrimSpace(req.Model)
	req.Engine = strings.TrimSpace(req.Engine)
	if req.Model == "" || req.Engine == "" || req.ContextSize <= 0 {
		return EngineState{}, Err("invalid_input")
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

	// Check active running state first so same-combination idempotency never touches CLI.
	s.mu.Lock()
	if s.orphan != nil {
		state := clone(s.orphan.state)
		s.mu.Unlock()
		return state, Err("engine_orphaned")
	}
	if s.engine != nil && (s.engine.state.State == "starting" || s.engine.state.State == "running" || s.engine.state.State == "stopping") {
		state := clone(s.engine.state)
		s.mu.Unlock()
		if state.Origin == EngineOriginStandalone && state.Candidate == slug {
			return state, nil
		}
		return state, Err("engine_candidate_mismatch")
	}
	s.mu.Unlock()

	cliPath, cliErr := s.resolveServeCLI(ctx, u)
	if cliErr != nil {
		return EngineState{}, cliErr
	}
	if ctx.Err() != nil {
		return EngineState{}, Err("cancelled")
	}

	serveDir, dirErr := safeServeDir(*w, slug)
	if dirErr != nil {
		return EngineState{}, dirErr
	}
	planPath := filepath.Join(serveDir, "serve-plan.json")
	startup, reused := reusableStandalonePlan(planPath, modelEntry.path, *engEntry, req.ContextSize)
	if !reused {
		port, err := ChooseFreeLoopbackPort(DefaultBenchmarkPort)
		if err != nil {
			return EngineState{}, Err("engine_port_in_use")
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

	return s.startFromStartupLocked(startup, EngineOriginStandalone, slug)
}
