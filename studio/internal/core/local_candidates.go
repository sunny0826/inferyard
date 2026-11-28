package core

import (
	"context"
	"inferyard/studio/internal/inferyard"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type LocalCandidateRequest struct {
	ModelName, EngineName string
	Bundles               []string
	ServicePID            int64
	Endpoint              string
	ReasoningEffort       string
}

type CandidateRun struct {
	CandidatePath string
	Bundle        string
	Family        string
}

type localCandidateOrigin struct {
	installationPath, installationKey, declarationHash string
}

func (s *Service) PrepareLocalCandidates(ctx context.Context, req LocalCandidateRequest) ([]CandidateRun, *ApiError) {
	u := s.userSettings()
	workspace := workspaceRoot(u)
	results := s.resultsRoot(u)
	if workspace == nil || results == nil || len(req.Bundles) < 1 || len(req.Bundles) > 2 {
		return nil, Err("invalid_request")
	}
	rawEffort := strings.TrimSpace(req.ReasoningEffort)
	if rawEffort == "" {
		rawEffort = strings.TrimSpace(u.ReasoningEffort)
	}
	var normalizedEffort string
	if rawEffort != "" {
		var ok bool
		normalizedEffort, ok = ParseReasoningEffort(rawEffort)
		if !ok {
			return nil, Err("invalid_request")
		}
	}
	var selected *registryEntry
	for _, entry := range s.registry().engines {
		if entry.engine.Name == req.EngineName && entry.installation != "" {
			selected = &entry
			break
		}
	}
	if selected == nil {
		return nil, Err("not_found")
	}
	model, failure := s.resourcePath(req.ModelName, false)
	if failure != nil {
		return nil, failure
	}
	probe := s.probe(u)
	if probe.Status != "ok" {
		return nil, cliProbeError(probe)
	}
	key := installationKey(selected.installation)
	cliKey, generation := s.captureCheckGeneration(probe)
	var family, declSHA256 string
	s.checkMu.Lock()
	if cached, ok := s.checks[selected.path]; ok && cached.verified && cached.key == key &&
		cached.result.State == EngineCheckEligible && cached.result.Family != "" && cached.result.DeclarationSHA256 != nil {
		family = cached.result.Family
		declSHA256 = *cached.result.DeclarationSHA256
	}
	s.checkMu.Unlock()
	if family == "" || declSHA256 == "" {
		checked, err := s.exec(ctx, *probe.Path, "engine", "check", "--installation", selected.installation)
		if failure := exitError(checked, err); failure != nil {
			return nil, failure
		}
		check, err := inferyard.ParseEngineCheck(checked.Stdout, checked.ExitCode)
		if err != nil || check.Family == nil || check.DeclarationSHA256 == nil {
			return nil, Err("engine_check_invalid_response")
		}
		if !check.Eligible {
			return nil, &ApiError{Code: "local_engine_installation_rejected", Limitations: []string{str(check.Reason)}}
		}
		if key != installationKey(selected.installation) {
			return nil, Err("local_engine_installation_changed")
		}
		s.cacheInstallationCheck(cliKey, generation, selected.path, key, check)
		family = *check.Family
		declSHA256 = *check.DeclarationSHA256
	}
	port := strconv.Itoa(DefaultBenchmarkPort)
	if family == "lmstudio" {
		if req.ServicePID <= 0 {
			return nil, Err("local_engine_lmstudio_background_pid_required")
		}
		var failure *ApiError
		port, failure = localEndpointPort(req.Endpoint)
		if failure != nil {
			return nil, failure
		}
	} else if req.ServicePID > 0 {
		var failure *ApiError
		port, failure = localEndpointPort(req.Endpoint)
		if failure != nil {
			return nil, failure
		}
	} else if req.ServicePID < 0 {
		return nil, Err("invalid_request")
	} else {
		allocated, err := ChooseFreeLoopbackPort(DefaultBenchmarkPort)
		if err != nil || allocated <= 0 {
			return nil, Err("internal_error")
		}
		port = strconv.Itoa(allocated)
	}
	seen := map[string]bool{}
	candidates := []CandidateRun{}
	var sharedInstallation string
	for _, bundle := range req.Bundles {
		if ctx.Err() != nil {
			return nil, Err("cancelled")
		}
		if bundle == "" || strings.ContainsAny(bundle, `/\`) || bundle == "." || bundle == ".." || seen[bundle] {
			return nil, Err("invalid_request")
		}
		seen[bundle] = true
		bundlePath, err := Resolve(*workspace, filepath.Join("bundles", bundle+".json"))
		if err != nil {
			return nil, Err("invalid_request")
		}
		if info, statErr := os.Stat(bundlePath); statErr != nil || info.IsDir() {
			return nil, Err("invalid_request")
		}
		if sharedInstallation == "" {
			sharedInstallation = selected.installation
		}
		installation := sharedInstallation
		output, failure := NextOutput(*workspace, "candidates", bundle)
		if failure != nil {
			return nil, failure
		}
		args := []string{"config", "create", "--installation", installation, "--model", model,
			"--bundle", bundlePath, "--results", *results, "--port", port, "--out", output}
		if family == "polystrata" && normalizedEffort != "" && normalizedEffort != ReasoningEffortNone {
			args = append(args, "--reasoning-effort", normalizedEffort)
		}
		if req.ServicePID > 0 {
			args = append(args, "--service-pid", strconv.FormatInt(req.ServicePID, 10))
		}
		out, err := s.exec(ctx, *probe.Path, args...)
		if ctx.Err() != nil {
			return nil, Err("cancelled")
		}
		if failure := exitError(out, err); failure != nil {
			return nil, failure
		}
		if out.ExitCode != 0 {
			return nil, Err("cli_error")
		}
		candidate := filepath.Join(output, "candidate.toml")
		description, failure := s.DescribeCandidate(ctx, candidate)
		if failure != nil {
			return nil, failure
		}
		modelMatches := description.Assets.Model.Path == model ||
			(family == "polystrata" && strings.HasSuffix(strings.ToLower(model), ".json") && filepath.IsAbs(description.Assets.Model.Path))
		if description.Family != family || !modelMatches || description.Assets.Installation == nil {
			return nil, Err("candidate_mismatch")
		}
		if description.Assets.Installation.DeclarationSHA256 != declSHA256 {
			return nil, Err("candidate_mismatch")
		}
		s.mu.Lock()
		if s.candidateOrigins == nil {
			s.candidateOrigins = map[string]localCandidateOrigin{}
		}
		s.candidateOrigins[candidate] = localCandidateOrigin{selected.installation, key, description.Assets.Installation.DeclarationSHA256}
		s.mu.Unlock()
		candidates = append(candidates, CandidateRun{CandidatePath: candidate, Bundle: bundle, Family: family})
	}
	return candidates, nil
}

func localEndpointPort(endpoint string) (string, *ApiError) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return "", Err("invalid_request")
	}
	if parsed.Hostname() != "127.0.0.1" {
		return "", Err("engine_host_not_loopback")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", Err("invalid_request")
	}
	return strconv.Itoa(port), nil
}

func NextOutput(workspace, category, prefix string) (string, *ApiError) {
	if workspace == "" || !ValidDownloadName(prefix) {
		return "", Err("invalid_request")
	}
	for i := 1; i <= 1000; i++ {
		path, err := Resolve(workspace, filepath.Join(category, prefix+"-"+strconv.Itoa(i)))
		if err != nil {
			return "", Err("invalid_request")
		}
		if _, err = os.Lstat(path); os.IsNotExist(err) {
			return path, nil
		} else if err != nil {
			return "", Err("internal_error")
		}
	}
	return "", Err("output_name_exhausted")
}

func validBindingEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return false
	}
	host := u.Hostname()
	if host != "localhost" && !net.ParseIP(host).IsLoopback() {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	return err == nil && port > 0 && port <= 65535 && !strings.ContainsRune(endpoint, 0)
}
