package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
	if workspace == nil || u.ResultsRoot == nil || len(req.Bundles) < 1 || len(req.Bundles) > 2 {
		return nil, Err("invalid_request")
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
		return nil, Err("cli_unavailable")
	}
	key := installationKey(selected.installation)
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
	family := *check.Family
	port := "48857"
	if family == "lmstudio" {
		if req.ServicePID <= 0 {
			return nil, Err("local_engine_lmstudio_background_pid_required")
		}
		var failure *ApiError
		port, failure = localEndpointPort(req.Endpoint)
		if failure != nil {
			return nil, failure
		}
	} else if req.ServicePID != 0 {
		return nil, Err("invalid_request")
	}
	seen := map[string]bool{}
	candidates := []CandidateRun{}
	for _, bundle := range req.Bundles {
		if ctx.Err() != nil {
			return nil, Err("cancelled")
		}
		if bundle != "zh-core" && bundle != "zh-svg-pelican" || seen[bundle] {
			return nil, Err("invalid_request")
		}
		seen[bundle] = true
		bundlePath, err := Resolve(*workspace, filepath.Join("bundles", bundle+".json"))
		if err != nil {
			return nil, Err("invalid_request")
		}
		installation := selected.installation
		if family == "sglang" {
			installation, failure = scopedSGLangInstallation(*workspace, installation, key)
			if failure != nil {
				return nil, failure
			}
		}
		output, failure := nextLocalOutput(*workspace, "candidates", bundle)
		if failure != nil {
			return nil, failure
		}
		args := []string{"config", "create", "--installation", installation, "--model", model,
			"--bundle", bundlePath, "--results", *u.ResultsRoot, "--port", port, "--out", output}
		if family == "lmstudio" {
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
		if description.Family != family || description.Assets.Model.Path != model || description.Assets.Installation == nil {
			return nil, Err("candidate_mismatch")
		}
		if family != "sglang" && description.Assets.Installation.DeclarationSHA256 != *check.DeclarationSHA256 {
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

func nextLocalOutput(workspace, category, prefix string) (string, *ApiError) {
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

// Each SGLang service receives a new metrics directory, outside its immutable runtime inventory.
func scopedSGLangInstallation(workspace, path, key string) (string, *ApiError) {
	raw, err := installationBytes(path)
	if err != nil || "installation|"+fmt.Sprintf("%x", sha256.Sum256(raw)) != key {
		return "", Err("local_engine_installation_changed")
	}
	var declaration map[string]json.RawMessage
	if json.Unmarshal(raw, &declaration) != nil {
		return "", Err("local_engine_installation_unreadable")
	}
	stateRoot, failure := nextLocalOutput(workspace, "service-state", "sglang")
	if failure != nil {
		return "", failure
	}
	if err := os.MkdirAll(filepath.Dir(stateRoot), 0700); err != nil {
		return "", Err("internal_error")
	}
	if err := os.Mkdir(stateRoot, 0700); err != nil {
		return "", Err("internal_error")
	}
	metrics := filepath.Join(stateRoot, "metrics")
	if err := os.Mkdir(metrics, 0700); err != nil {
		return "", Err("internal_error")
	}
	declaration["working_directory"], _ = json.Marshal(stateRoot)
	for _, field := range []string{"environment", "worker_environment"} {
		var values map[string]string
		if field == "worker_environment" && declaration[field] == nil {
			continue
		}
		if json.Unmarshal(declaration[field], &values) != nil || values == nil {
			return "", Err("local_engine_installation_unreadable")
		}
		values["SGLANG_RUN_ID"] = "inferyard-" + filepath.Base(stateRoot)
		values["PROMETHEUS_MULTIPROC_DIR"] = metrics
		declaration[field], _ = json.Marshal(values)
	}
	derived := filepath.Join(stateRoot, "installation.json")
	data, err := json.MarshalIndent(declaration, "", "  ")
	if err != nil {
		return "", Err("internal_error")
	}
	if err := os.WriteFile(derived, append(data, '\n'), 0600); err != nil {
		return "", Err("internal_error")
	}
	return derived, nil
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
