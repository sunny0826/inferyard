package core

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"inferyard/studio/internal/inferyard"
)

func (s *Service) StartContextSpeed(req StartContextSpeedRequest) (RunTask, *ApiError) {
	if req.Model == "" || req.Engine == "" || (req.Context != 65536 && req.Context != 131072 && req.Context != 262144) {
		return RunTask{}, Err("invalid_request")
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	u := s.userSettings()
	w := workspaceRoot(u)
	if w == nil || u.ResultsRoot == nil {
		return RunTask{}, Conflict("settings_incomplete")
	}
	p := s.probe(u)
	if p.Status != "ok" {
		return RunTask{}, Err("cli_unavailable")
	}
	id := uuid()
	ops, e := Resolve(*w, filepath.Join(*w, "context-speed-operations", id))
	if e != nil {
		return RunTask{}, Err("invalid_request")
	}
	batch, e := Resolve(*u.ResultsRoot, filepath.Join(*u.ResultsRoot, "context-speed-"+id))
	if e != nil {
		return RunTask{}, Conflict("output_root_outside_results")
	}
	recordRoot, e := Resolve(*w, filepath.Join(*w, "logs", "context-speed-tasks"))
	if e != nil {
		return RunTask{}, Err("invalid_request")
	}
	t := &runEntry{Task: RunTask{TaskId: id, Kind: "context_speed", State: "starting", CreatedAt: now(), ContextSpeed: &ContextSpeedTask{Model: req.Model, Engine: req.Engine, Context: req.Context, Stage: "preparing", Operations: ops}}, root: batch, binary: *p.Path, recordRoot: recordRoot}
	ctx, cancel := context.WithCancel(s.ctx)
	t.cancel = cancel
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.admitRunLocked(); e != nil {
		cancel()
		return RunTask{}, e
	}
	if s.runsActiveLocked() {
		cancel()
		return RunTask{}, Conflict("run_active")
	}
	if s.engine != nil && !terminalEngine(s.engine.state.State) {
		cancel()
		return RunTask{}, Conflict("engine_active")
	}
	if err := saveSpeedTask(recordRoot, t.Task); err != nil {
		cancel()
		return RunTask{}, Err("task_record_write_failed")
	}
	s.runs = append(s.runs, t)
	s.speedPreparing.Add(1)
	s.state(t)
	go func() {
		defer s.speedPreparing.Done()
		defer cancel()
		failure := s.prepareContextSpeed(ctx, t, req)
		s.mu.Lock()
		defer s.mu.Unlock()
		if failure != nil || ctx.Err() != nil || s.closing {
			t.Task.State = "failed"
			if ctx.Err() != nil {
				t.Task.State = "cancelled"
			}
			t.Task.Error = failure
			t.Task.FinishedAt = ptr(now())
		} else {
			t.Task.Queued = true
			t.Task.ContextSpeed.Stage = "queued"
			t.cancel = nil
			s.signal()
		}
		s.state(t)
	}()
	return clone(t.Task), nil
}

func terminalEngine(state string) bool { return state == "stopped" || state == "failed" }

func (s *Service) speedCommand(ctx context.Context, t *runEntry, args ...string) *ApiError {
	if ctx.Err() != nil {
		return Err("cancelled")
	}
	for i, arg := range args {
		if arg == "--out" && i+1 < len(args) {
			w := workspaceRoot(s.userSettings())
			if w == nil {
				return Err("settings_incomplete")
			}
			path, e := Resolve(*w, args[i+1])
			if e != nil || path != filepath.Clean(args[i+1]) {
				return Err("output_root_outside_workspace")
			}
		}
	}
	out, err := s.exec(ctx, t.binary, args...)
	if ctx.Err() != nil {
		return Err("cancelled")
	}
	if err != nil {
		return Err("cli_error")
	}
	if out.ExitCode != 0 {
		failure := exitError(out, nil)
		if failure == nil {
			failure = Err("cli_error")
		}
		for _, reason := range failure.Limitations {
			if message := speedFailureMessage(reason); message != "" {
				failure.Message = message
				break
			}
		}
		return failure
	}
	if _, err := inferyard.Parse(out.Stdout); err != nil {
		return Err("cli_error")
	}
	return nil
}

func (s *Service) speedStage(t *runEntry, stage string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.Task.ContextSpeed.Stage = stage
	s.state(t)
}

func (s *Service) prepareContextSpeed(ctx context.Context, t *runEntry, req StartContextSpeedRequest) *ApiError {
	out, err := s.exec(ctx, t.binary, "--schema", "context_speed_result")
	if ctx.Err() != nil {
		return Err("cancelled")
	}
	if err != nil || out.ExitCode != 0 || !inferyard.SupportsContextSpeedSchema(out.Stdout) {
		return &ApiError{Code: "cli_outdated", Message: "CLI 不支持长上下文速度结果，请升级 CLI；普通测评仍可使用。"}
	}
	model, e := s.resourcePath(req.Model, false)
	if e != nil {
		return e
	}
	ops := t.Task.ContextSpeed.Operations
	if _, err := os.Lstat(ops); !os.IsNotExist(err) {
		return Conflict("output_exists")
	}
	if err := os.MkdirAll(filepath.Dir(ops), 0700); err != nil {
		return Err("internal_error")
	}
	if e := s.speedCommand(ctx, t, "init", "--bundle", "zh-context-speed", "--out", ops); e != nil {
		return e
	}
	preflight := filepath.Join(ops, "preflight", "selected")
	if e := s.speedCommand(ctx, t, "device-check", "--json", "--model", model, "--out", preflight); e != nil {
		return e
	}
	candidate := filepath.Join(ops, "candidates", "selected")
	args := []string{"config", "create", "--workspace", ops, "--bundle", "zh-context-speed", "--profile", "context-speed", "--context-size", strconv.Itoa(req.Context), "--preflight", filepath.Join(preflight, "device-preflight.json"), "--results", t.root, "--out", candidate}
	args, e = s.candidateEngineArgs(args, req.Engine, runtime.GOOS)
	if e != nil {
		return e
	}
	if e := s.speedCommand(ctx, t, args...); e != nil {
		return e
	}
	s.speedStage(t, "binding")
	engine, e := s.StartEngine(candidate)
	if e != nil {
		return e
	}
	for engine.State == "starting" {
		select {
		case <-ctx.Done():
			return Err("cancelled")
		case <-time.After(100 * time.Millisecond):
		}
		engine = s.Engine()
	}
	if engine.State != "running" {
		if engine.Error != nil {
			return engine.Error
		}
		return Err("engine_start_failed")
	}
	bound := filepath.Join(ops, "bound", "selected")
	if e := s.speedCommand(ctx, t, "config", "bind", "--candidate", filepath.Join(candidate, "candidate.toml"), "--pid", strconv.FormatInt(engine.Pid, 10), "--endpoint", engine.Endpoint, "--out", bound); e != nil {
		return e
	}
	s.speedStage(t, "planning")
	planRoot := filepath.Join(ops, "plans", "selected")
	if _, err := Resolve(ops, planRoot); err != nil {
		return Err("invalid_request")
	}
	if err := os.MkdirAll(filepath.Dir(planRoot), 0700); err != nil {
		return Err("internal_error")
	}
	if e := s.speedCommand(ctx, t, "plan", "--preset", "context-speed", "--config", filepath.Join(bound, "config.toml"), "--out", planRoot); e != nil {
		return e
	}
	plan, e := readSpeedPlan(filepath.Join(planRoot, "plan.json"))
	if e != nil {
		return e
	}
	if _, err := os.Lstat(t.root); !os.IsNotExist(err) {
		return Conflict("output_exists")
	}
	s.mu.Lock()
	t.Task.ContextSpeed.Plan = &plan
	t.Task.ContextSpeed.Batch = &SpeedBatchRef{Path: t.root, Runs: []inferyard.SpeedRunRef{}}
	t.args = []string{"run", "--plan", plan.Path, "--output-root", t.root}
	s.mu.Unlock()
	return nil
}

func speedFailureMessage(reason string) string {
	switch reason {
	case "human_corpus_review_required":
		return "专项题库尚未完成人工审核，不能开始测评。"
	case "context_unsupported":
		return "模型不支持所选上下文档位，请选择支持的档位。"
	case "device_preflight_blocked", "insufficient_host_memory", "insufficient_device_memory":
		return "设备资源预算未通过检查，测评未启动。"
	}
	return ""
}
