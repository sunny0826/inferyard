package core

import (
	"inferyard/studio/internal/inferyard"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func loadCachedStandalonePlan(planPath string, modelPath string, eng registryEntry, contextSize int64) (Startup, string, int, bool) {
	if !isRegularNonSymlinkFile(planPath) {
		return Startup{}, "", 0, false
	}
	raw, err := os.ReadFile(planPath)
	if err != nil {
		return Startup{}, "", 0, false
	}
	plan, err := inferyard.ParseServePlan(raw)
	if err != nil {
		return Startup{}, "", 0, false
	}
	wantLocal := eng.installation != ""
	if plan.Local != wantLocal || plan.ContextSize != contextSize {
		return Startup{}, "", 0, false
	}
	if normalizedPath(plan.ModelPath) != normalizedPath(modelPath) {
		return Startup{}, "", 0, false
	}
	if wantLocal && eng.family != "" && plan.Family != eng.family {
		return Startup{}, "", 0, false
	}
	wantExe, ok := expectedEngineExecutable(eng)
	if !ok {
		return Startup{}, "", 0, false
	}
	// Preserve lexical venv Python symlinks for local installations so two venvs sharing a base interpreter do not collide.
	if wantLocal {
		if filepath.Clean(plan.Argv[0]) != filepath.Clean(wantExe) {
			return Startup{}, "", 0, false
		}
	} else if normalizedPath(plan.Argv[0]) != normalizedPath(wantExe) {
		return Startup{}, "", 0, false
	}
	if info, err := os.Stat(plan.Argv[0]); err != nil || info.IsDir() {
		return Startup{}, "", 0, false
	}
	for _, arg := range plan.Argv[1:] {
		if strings.HasSuffix(strings.ReplaceAll(arg, "\\", "/"), "/data/local_engine/bootstrap.py") {
			return Startup{}, "", 0, false
		}
	}
	if plan.Family == "polystrata" {
		cfgPath, _, _ := findConfigArg(plan.Argv[1:])
		if cfgPath == "" || !isRegularNonSymlinkFile(cfgPath) {
			return Startup{}, "", 0, false
		}
	}
	startup := servePlanToStartup(plan)
	host, port, apiErr := validateStartup(startup)
	if apiErr != nil {
		return Startup{}, "", 0, false
	}
	return startup, host, port, true
}

func rewriteStartupPort(startup Startup, newPort int) (Startup, bool) {
	if startup.Family == "polystrata" || newPort <= 0 || newPort > 65535 {
		return Startup{}, false
	}
	host, _, apiErr := validateStartup(startup)
	if apiErr != nil {
		return Startup{}, false
	}
	rewritten := startup
	rewritten.Args = replaceOrAppendFlag(startup.Args, []string{"--port"}, "--port", strconv.Itoa(newPort))
	rewritten.Endpoint = "http://" + net.JoinHostPort(host, strconv.Itoa(newPort))
	if _, port, err := validateStartup(rewritten); err != nil || port != newPort {
		return Startup{}, false
	}
	return rewritten, true
}

func (s *Service) tryReservePort(bindHost string, port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.occupiedPortsLocked()[port] {
		return false
	}
	if checkBindPortAvailable(bindHost, port) != nil {
		return false
	}
	s.reservedPorts[port] = true
	return true
}

func (s *Service) reserveStandalonePort(bindHost string, requestedPort int) (int, *ApiError) {
	if requestedPort > 0 {
		if !s.tryReservePort(bindHost, requestedPort) {
			return 0, portInUseError(requestedPort)
		}
		return requestedPort, nil
	}
	exclude := map[int]bool{}
	for range 8 {
		s.mu.Lock()
		for p := range s.occupiedPortsLocked() {
			exclude[p] = true
		}
		s.mu.Unlock()
		port, err := ChooseFreePortExcluding(bindHost, DefaultBenchmarkPort, exclude)
		if err != nil {
			return 0, portInUseError(0)
		}
		s.mu.Lock()
		if !s.occupiedPortsLocked()[port] {
			s.reservedPorts[port] = true
			s.mu.Unlock()
			return port, nil
		}
		s.mu.Unlock()
		exclude[port] = true
	}
	return 0, portInUseError(0)
}
