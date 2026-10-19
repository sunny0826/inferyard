package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

type WizardRequest struct {
	PreflightPath string `json:"preflight_path,omitempty"`
	ModelName     string `json:"model_name,omitempty"`
	EngineName    string `json:"engine_name,omitempty"`
	Command       string `json:"command"`
	ReportVerify  bool   `json:"report_verify"`
}
type WizardResult struct {
	Limitations []string             `json:"limitations,omitempty"`
	Status      inferyard.ExitStatus `json:"status"`
	ExitCode    int                  `json:"exit_code"`
	Tasks       []RunTask            `json:"tasks"`
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func wizardArgs(command string) ([]string, map[string][]string, *ApiError) {
	args, err := inferyard.SplitCommand(command)
	if err != nil {
		return nil, nil, Err("invalid_request")
	}
	key := args[0]
	start := 1
	if key == "config" || key == "model" || key == "engine" {
		if len(args) < 2 {
			return nil, nil, Err("invalid_request")
		}
		key += " " + args[1]
		start = 2
	}
	allowed := map[string]string{
		"init": "--out", "model acquire": "--source --out --token-env --timeout-seconds", "engine acquire": "--source --out --timeout-seconds",
		"device-check": "--models-root --out --json", "config create": "--workspace --bundle --engine --runtime-receipt --preflight --results --out --port --model-source --model-repo --model-revision",
		"config bind": "--candidate --pid --endpoint --out", "run": "--config", "resume": "--from-run --endpoint-url --server-pid", "report": "--runs --out", "verify": "--path",
	}
	flags, ok := allowed[key]
	if !ok {
		return nil, nil, Err("invalid_request")
	}
	values := map[string][]string{}
	for i := start; i < len(args); {
		flag := args[i]
		if !strings.Contains(" "+flags+" ", " "+flag+" ") || values[flag] != nil {
			return nil, nil, Err("invalid_request")
		}
		i++
		values[flag] = []string{}
		if flag == "--json" {
			continue
		}
		for i < len(args) && !strings.HasPrefix(args[i], "--") {
			values[flag] = append(values[flag], args[i])
			i++
			if flag != "--runs" {
				break
			}
		}
		if len(values[flag]) == 0 {
			return nil, nil, Err("invalid_request")
		}
	}
	required := map[string][]string{
		"init": {"--out"}, "model acquire": {"--source", "--out"}, "engine acquire": {"--source", "--out"},
		"device-check": {"--out"}, "config create": {"--bundle", "--out"},
		"config bind": {"--candidate", "--pid", "--endpoint", "--out"}, "run": {"--config"},
		"resume": {"--from-run"}, "report": {"--runs", "--out"}, "verify": {"--path"},
	}
	for _, flag := range required[key] {
		if len(values[flag]) == 0 || values[flag][0] == "" {
			return nil, nil, Err("invalid_request")
		}
	}
	if key == "resume" && (value(values, "--endpoint-url") == "") != (value(values, "--server-pid") == "") {
		return nil, nil, Err("invalid_request")
	}
	for _, flag := range []string{"--source", "--endpoint", "--endpoint-url"} {
		if v := values[flag]; len(v) > 0 {
			u, e := url.Parse(v[0])
			if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host == "" {
				return nil, nil, Err("invalid_request")
			}
		}
	}
	if v := values["--token-env"]; len(v) > 0 && !envName.MatchString(v[0]) {
		return nil, nil, Err("invalid_request")
	}
	return args, values, nil
}
func value(flags map[string][]string, name string) string {
	if len(flags[name]) == 1 {
		return flags[name][0]
	}
	return ""
}
func (s *Service) WizardExecute(ctx context.Context, q WizardRequest) (WizardResult, *ApiError) {
	result := WizardResult{Tasks: []RunTask{}}
	s.mu.Lock()
	closing := s.closing
	s.mu.Unlock()
	if closing {
		return result, Err("service_closing")
	}
	args, flags, e := wizardArgs(q.Command)
	if e != nil {
		return result, e
	}
	if s.paths != nil && args[0] == "init" && value(flags, "--out") != s.paths.Workspace {
		return result, Conflict("paths_bound")
	}
	if args[0] == "device-check" && q.ModelName != "" {
		path, err := s.resourcePath(q.ModelName, false)
		if err != nil {
			return result, err
		}
		args = resourceArg(args, "--model", path)
		flags["--model"] = []string{path}
	}
	if len(args) > 1 && args[0] == "config" && args[1] == "create" && q.EngineName != "" {
		var err *ApiError
		args, err = s.candidateEngineArgs(args, q.EngineName, runtime.GOOS)
		if err != nil {
			return result, err
		}
	}
	if len(args) > 1 && args[0] == "config" && args[1] == "create" && q.PreflightPath != "" {
		args = resourceArg(args, "--preflight", q.PreflightPath)
	}

	if q.ReportVerify && args[0] != "report" && args[0] != "verify" {
		return result, Err("invalid_request")
	}
	if args[0] == "run" {
		tasks, e := s.StartRuns(StartRunsRequest{ConfigPaths: []string{value(flags, "--config")}, AllowSingleBundle: ptr(true)})
		result.Tasks = tasks
		result.Status = inferyard.Complete
		return result, e
	}
	if args[0] == "resume" {
		req := ResumeRunRequest{FromRun: value(flags, "--from-run")}
		if endpoint := value(flags, "--endpoint-url"); endpoint != "" {
			pid, err := strconv.ParseInt(value(flags, "--server-pid"), 10, 64)
			if err != nil {
				return result, Err("invalid_request")
			}
			req.EndpointUrl = &endpoint
			req.ServerPid = &pid
		}
		task, e := s.Resume(req)
		if e == nil {
			result.Tasks = append(result.Tasks, task)
		}
		result.Status = inferyard.Complete
		return result, e
	}
	u := s.userSettings()
	p := s.probe(u)
	if p.Status != "ok" {
		return result, Err("cli_unavailable")
	}
	if out := value(flags, "--out"); out != "" {
		if !filepath.IsAbs(out) {
			return result, Err("invalid_request")
		}
		if _, err := os.Lstat(out); !os.IsNotExist(err) {
			return result, Conflict("output_exists")
		}
		if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			return result, Err("internal_error")
		}
	}
	if args[0] == "device-check" {
		if value(flags, "--models-root") == "" {
			if u.ModelsRoot == nil {
				return result, Conflict("settings_incomplete")
			}
			args = append(args, "--models-root", *u.ModelsRoot)
		}
		if flags["--json"] == nil {
			args = append(args, "--json")
		}
	}
	out, err := s.exec(ctx, *p.Path, args...)
	if err != nil {
		return result, Err("internal_error")
	}
	result.Status = inferyard.MapExitCode(out.ExitCode)
	result.ExitCode = out.ExitCode
	// Never return stdout/stderr: they may contain credential-bearing diagnostics.
	if out.ExitCode != 0 {
		if parsed, err := inferyard.Parse(out.Stdout); err == nil {
			for _, reason := range parsed.Limitations {
				if fixedReason.MatchString(reason) {
					result.Limitations = append(result.Limitations, reason)
				}
			}
		}
		return result, nil
	}
	if args[0] == "init" && s.paths == nil {
		root := value(flags, "--out")
		if root == "" {
			return result, Err("invalid_request")
		}
		workspace, _ := json.Marshal(root)
		results, _ := json.Marshal(filepath.Join(root, "results"))
		if _, e := s.UpdateSettings(map[string]json.RawMessage{"workspace_root": workspace, "results_root": results}); e != nil {
			return result, e
		}
	}
	if args[0] == "report" {
		path := value(flags, "--out")
		if report, err := reportSummary(path); err == nil {
			s.Bus.Publish("report.created", map[string]any{"report": report})
		}
		if q.ReportVerify {
			verified, err := s.exec(ctx, *p.Path, "verify", "--path", path)
			if err != nil {
				return result, Err("internal_error")
			}
			result.Status = inferyard.MapExitCode(verified.ExitCode)
			result.ExitCode = verified.ExitCode
		}
	}
	s.Bus.Publish("sync.reset", map[string]string{"reason": "workspace_changed"})
	return result, nil
}

var fixedReason = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)

func resourceArg(args []string, flag, value string) []string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			args[i+1] = value
			return args
		}
	}
	return append(args, flag, value)
}
