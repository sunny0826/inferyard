//go:build ui_debug && !release

package pages

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"os"
	"reflect"
	"testing"
	"time"
)

type speedParityBackend struct {
	task                   core.RunTask
	result                 inferyard.ContextSpeedResult
	failure                *core.ApiError
	starts, reads, cancels int
}

func (b *speedParityBackend) StartContextSpeed(r core.StartContextSpeedRequest) (core.RunTask, *core.ApiError) {
	b.starts++
	return b.task, nil
}
func (b *speedParityBackend) CancelRun(string) (core.RunTask, *core.ApiError) {
	b.cancels++
	b.task.State = "cancelled"
	return b.task, nil
}
func (b *speedParityBackend) ReadContextSpeed(context.Context, string) (inferyard.ContextSpeedResult, *core.ApiError) {
	b.reads++
	if b.failure != nil {
		return inferyard.ContextSpeedResult{}, b.failure
	}
	return b.result, nil
}

type speedParityFrame struct {
	stage                 speedStage
	rows, valid           int
	summary               bool
	prompt                int
	time, prefill, decode float64
	model, engine         string
}

func speedFrame(r *speedTask) speedParityFrame {
	f := speedParityFrame{stage: r.stage, rows: len(r.samples), valid: r.validCount(), summary: r.summary != nil, model: r.model, engine: r.engine}
	if r.summary != nil {
		f.prompt = *r.summary.prompt
		f.time = *r.summary.seconds
		f.prefill = *r.summary.prefill
		f.decode = *r.summary.decode
	}
	return f
}

// Production fixtures originate from the frozen contract, not the simulator.
func speedParityResult(t *testing.T, model, engine string, missing bool) inferyard.ContextSpeedResult {
	t.Helper()
	raw, e := os.ReadFile("../inferyard/testdata/context_speed_contract.json")
	if e != nil {
		t.Fatal(e)
	}
	var rows []struct {
		Name     string         `json:"name"`
		Response map[string]any `json:"response"`
	}
	if e = json.Unmarshal(raw, &rows); e != nil {
		t.Fatal(e)
	}
	response := rows[0].Response
	d := response["details"].(map[string]any)
	meta := d["metadata"].(map[string]any)
	meta["model"].(map[string]any)["display_name"] = model
	meta["engine"].(map[string]any)["release"] = engine
	times := []float64{25.9125, 26.1125, 26.3125, 26.5125, 26.7125}
	for i, v := range d["samples"].([]any) {
		sample := v.(map[string]any)
		c := sample["columns"].(map[string]any)
		for key, value := range map[string]float64{"prompt_length": 52625, "time": times[i], "prefill": 52625 / times[i], "decode": 42} {
			c[key].(map[string]any)["value"] = value
		}
		record := sample["proof"].(map[string]any)["final_timing"].(map[string]any)["record"].(map[string]any)
		record["prompt_n"], record["prompt_ms"], record["predicted_ms"] = 52625, times[i]*1000, 255.0/42*1000
		if missing && i == 4 {
			sample["proof"].(map[string]any)["final_timing"] = nil
			for _, key := range []string{"time", "prefill", "decode"} {
				value := c[key].(map[string]any)
				value["value"] = nil
				value["missing_reason"] = "final_timing_missing"
				value["evidence_refs"] = []any{}
			}
			m := sample["measurement"].(map[string]any)
			m["valid"] = false
			m["reasons"] = []string{"final_timing_missing", "metric_missing"}
		}
	}
	if missing {
		d["summary"] = nil
		m := d["measurement"].(map[string]any)
		m["status"] = "incomplete"
		m["valid_count"] = 4
		m["reasons"] = []string{"five_valid_samples_required"}
	} else {
		c := d["summary"].(map[string]any)["columns"].(map[string]any)
		c["prompt_length"].(map[string]any)["value"] = 52625
		c["time"] = map[string]float64{"median": 26.3125, "min": 25.9125, "max": 26.7125}
		c["prefill"] = map[string]float64{"median": 2000, "min": 52625 / times[4], "max": 52625 / times[0]}
		c["decode"] = map[string]float64{"median": 42, "min": 42, "max": 42}
	}
	raw, _ = json.Marshal(response)
	result, e := inferyard.ParseContextSpeed(raw, 0)
	if e != nil {
		t.Fatal(e)
	}
	return result
}

func TestPreviewContextSpeedParity(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "five_complete", true: "missing_timing"}[missing], func(t *testing.T) {
			p, tt := prepareSpeed(t, "64k")
			model, engine := p.state.model, p.state.engine
			p.speedMissingTiming = missing
			b := &speedParityBackend{task: core.RunTask{TaskId: "production", State: "starting", ContextSpeed: &core.ContextSpeedTask{Model: model, Engine: engine, Context: 65536, Stage: "preparing"}}, result: speedParityResult(t, model, engine, missing)}
			production, e := startSpeedFlow(b, core.StartContextSpeedRequest{Model: model, Engine: engine, Context: 65536})
			if e != nil {
				t.Fatal(e)
			}
			click(t, tt, "开始专项测评")
			for _, checkpoint := range []struct {
				elapsed time.Duration
				stage   string
				want    speedStage
			}{{0, "preparing", speedPreparing}, {400 * time.Millisecond, "binding", speedBinding}, {700 * time.Millisecond, "planning", speedPlanning}, {400 * time.Millisecond, "queued", speedQueued}, {400 * time.Millisecond, "measuring", speedMeasuring}} {
				p.advance(checkpoint.elapsed)
				b.task.ContextSpeed.Stage = checkpoint.stage
				production = speedFromTask(b.task)
				for label, r := range map[string]*speedTask{"production": production, "preview": p.state.speed.current} {
					if r.stage != checkpoint.want || !r.active() || r.model != model || r.engine != engine {
						t.Fatalf("%s checkpoint %+v", label, r)
					}
				}
			}
			p.advance(10 * time.Second)
			b.task.State, b.task.ContextSpeed.Stage = "done", "done"
			production = speedFromTask(b.task)
			production, e = readSpeedFlow(t.Context(), b, production)
			if e != nil {
				t.Fatal(e)
			}
			tt.Frame()
			click(t, tt, "查看结果")
			want := speedParityFrame{stage: speedDone, rows: 5, valid: 5, summary: true, prompt: 52625, time: 26.3125, prefill: 2000, decode: 42, model: model, engine: engine}
			if missing {
				want.valid = 4
				want.summary = false
				want.prompt = 0
				want.time = 0
				want.prefill = 0
				want.decode = 0
			}
			prod, sim := speedFrame(production), speedFrame(p.state.speed.opened)
			for label, got := range map[string]speedParityFrame{"production": prod, "preview": sim} {
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s got %+v want %+v", label, got, want)
				}
			}
			if prod != sim {
				t.Fatal("production/preview drift")
			}
			p.state.model, p.state.engine = "other model", "other engine"
			p.failNext = true
			p.state.openSpeed(p.state.speed.current.id)
			b.failure = core.Err("context_speed_invalid_response")
			failed, e := readSpeedFlow(t.Context(), b, production)
			if e == nil || failed.samples != nil || failed.result != nil || p.state.speed.opened.samples != nil || p.state.speed.opened.result != nil {
				t.Fatal("read failure kept stale values")
			}
			b.failure = nil
			_, e = readSpeedFlow(t.Context(), b, production)
			if e != nil {
				t.Fatal(e)
			}
			p.state.openSpeed(p.state.speed.current.id)
			if p.state.speed.opened.model != model || b.starts != 1 || b.reads != 3 || len(p.state.speed.history) != 1 || len(p.indexes) != 0 || p.state.service != nil {
				t.Fatal("read retry changed identity, reran, reported or reached IO")
			}
		})
	}
}

func TestPreviewContextSpeedFailureParity(t *testing.T) {
	for _, scenario := range []struct{ stage, reason string }{
		{"preparing", "human_corpus_review_required"}, {"preparing", "cli_outdated"},
		{"preparing", "device_preflight_blocked"}, {"binding", "engine_start_failed"},
		{"binding", "startup_arguments_mismatch"}, {"planning", "context_unsupported"},
	} {
		t.Run(scenario.reason, func(t *testing.T) {
			p, tt := prepareSpeed(t, "64k")
			p.speedFailureStage = scenario.stage
			p.speedFailure = &core.ApiError{Code: scenario.reason, Message: scenario.reason}
			model, engine := p.state.model, p.state.engine
			b := &speedParityBackend{task: core.RunTask{TaskId: "failed", State: "starting", ContextSpeed: &core.ContextSpeedTask{Model: model, Engine: engine, Context: 65536, Stage: "preparing"}}}
			if _, e := startSpeedFlow(b, core.StartContextSpeedRequest{Model: model, Engine: engine, Context: 65536}); e != nil {
				t.Fatal(e)
			}
			click(t, tt, "开始专项测评")
			p.advance(10 * time.Second)
			b.task.State = "failed"
			b.task.ContextSpeed.Stage = scenario.stage
			b.task.Error = &core.ApiError{Code: scenario.reason, Message: scenario.reason}
			production := speedFromTask(b.task)
			for name, r := range map[string]*speedTask{"production": production, "preview": p.state.speed.current} {
				if r.stage != speedFailed || r.reason != scenario.reason || r.samples != nil || r.summary != nil || r.active() {
					t.Fatalf("%s failure mismatch: %+v", name, r)
				}
			}
			p.advance(time.Minute)
			if len(p.state.speed.history) != 1 || len(p.jobs) != 0 {
				t.Fatal("failure retried without action")
			}
			// Retry is a new full task, never continuation of a failed batch.
			tt.Frame()
			click(t, tt, "用此组合新开任务")
			if engineLocked(p.state.engineState.State) {
				p.state.stopEngine()
				p.advance(time.Second)
			}
			tt.Frame()
			click(t, tt, "开始专项测评")
			p.advance(10 * time.Second)
			if p.state.speed.current.stage != speedDone || len(p.state.speed.history) != 2 || p.state.speed.history[0].reason != scenario.reason || len(p.indexes) != 0 {
				t.Fatal("manual retry changed old task or failed")
			}
		})
	}
}

func TestPreviewContextSpeedCancelParity(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		elapsed   time.Duration
		completed int
	}{
		{"waiting", 1600 * time.Millisecond, 0}, {"active", 3 * time.Second, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			p, tt := prepareSpeed(t, "64k")
			click(t, tt, "开始专项测评")
			p.advance(scenario.elapsed)
			tt.Frame()
			if scenario.name == "waiting" && p.state.speed.current.stage != speedQueued {
				t.Fatal("not queued")
			}
			click(t, tt, "取消专项测评")
			if p.state.speed.current.stage != speedCancelling || !p.state.busy {
				t.Fatal("skipped draining")
			}
			p.advance(time.Second)
			r := p.state.speed.current
			if r.stage != speedCancelled || r.completed != scenario.completed || len(p.state.speed.history) != 1 || len(p.indexes) != 0 {
				t.Fatalf("wrong cancelled state: %+v", r)
			}
			task := core.RunTask{TaskId: "cancelled", State: "cancelled", ContextSpeed: &core.ContextSpeedTask{Stage: "measuring", Model: r.model, Engine: r.engine, Context: 65536, Completed: scenario.completed, Batch: &core.SpeedBatchRef{}}}
			backend := &speedParityBackend{task: task}
			backend.task.State = "running"
			if e := cancelSpeedFlow(backend, task.TaskId); e != nil {
				t.Fatal(e)
			}
			if backend.cancels != 1 {
				t.Fatal("cancel did not reach Service boundary")
			}
			production := speedFromTask(backend.task)
			if production.stage != speedCancelled || production.completed != scenario.completed || production.active() {
				t.Fatal("production cancellation projection diverged")
			}
			p.advance(time.Minute)
			if r.completed != scenario.completed || len(p.jobs) != 0 {
				t.Fatal("cancel resumed trials")
			}
		})
	}
}

func TestPreviewContextSpeedTypedContract(t *testing.T) {
	for _, context := range []int{65536, 131072, 262144} {
		for _, completed := range []int{0, 1, 4, 5} {
			for _, missing := range []bool{false, true} {
				task := &speedTask{model: "simulated-model", engine: "simulated-engine", context: context}
				result := previewSpeedResult(task, completed, missing)
				code, status, completeness := 0, "finished", "complete"
				if completed != 5 {
					code, status, completeness = 3, "partial", "incomplete"
				}
				raw, _ := json.Marshal(map[string]any{"command": "speed-results", "status": status, "completeness": completeness, "run_id": nil, "evidence_dir": "/simulated/batch", "limitations": []string{}, "details": result})
				if _, e := inferyard.ParseContextSpeed(raw, code); e != nil {
					t.Fatalf("context=%d completed=%d missing=%v: %v", context, completed, missing, e)
				}
			}
		}
	}
}
