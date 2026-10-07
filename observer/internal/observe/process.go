package observe

import (
	"errors"
	"strings"
)

var (
	ErrExited      = errors.New("process_exited")
	ErrPermission  = errors.New("permission_denied")
	ErrUnavailable = errors.New("process_unavailable")
)

func ErrorReason(err error) string {
	if errors.Is(err, ErrExited) {
		return "process_exited"
	}
	if errors.Is(err, ErrPermission) {
		return "permission_denied"
	}
	return "process_unavailable"
}

func IsCandidate(name string) bool {
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	return name == "llama-server" || name == "ollama" || name == "vllm" || name == "mlx_lm.server"
}

type baseline struct {
	cpu      uint64
	midpoint int64
}
type Tracker struct {
	previous map[int]baseline
	lost     map[int]string
}

func NewTracker() *Tracker { return &Tracker{map[int]baseline{}, map[int]string{}} }

// A lost target never binds a later process that happens to reuse its PID.
func (t *Tracker) Read(target Target, started, finished int64, raw RawProcess, err error) Process {
	p := Process{Target: target, State: "running", CPUNS: raw.CPUNS, RSS: raw.RSS,
		ReadStartedNS: started, ReadFinishedNS: finished,
		CPUPercent: Missing[float64]("first_observation", "counter_delta_over_monotonic_interval")}
	reason := t.lost[target.PID]
	if reason == "" && err != nil {
		reason = ErrorReason(err)
	}
	if reason == "" && raw.Target.StartTicks != target.StartTicks {
		reason = "process_identity_changed"
	}
	if reason != "" {
		t.lost[target.PID] = reason
		delete(t.previous, target.PID)
		p.State = "unavailable"
		if reason == "process_exited" {
			p.State = "exited"
		}
		if reason == "process_identity_changed" {
			p.State = "identity_changed"
		}
		p.CPUNS = Missing[uint64](reason, "native_process")
		p.RSS = Missing[uint64](reason, "native_process")
		p.CPUPercent = Missing[float64](reason, "counter_delta_over_monotonic_interval")
		return p
	}
	if raw.CPUNS.Value == nil {
		delete(t.previous, target.PID)
		p.CPUPercent = Missing[float64]("cpu_counter_unavailable", "counter_delta_over_monotonic_interval")
		return p
	}
	mid := started + (finished-started)/2
	if prev, ok := t.previous[target.PID]; ok {
		if mid <= prev.midpoint || *raw.CPUNS.Value < prev.cpu {
			p.CPUPercent = Missing[float64]("counter_or_clock_regressed", "counter_delta_over_monotonic_interval")
		} else {
			p.CPUPercent = Known(100*float64(*raw.CPUNS.Value-prev.cpu)/float64(mid-prev.midpoint),
				"counter_delta_over_monotonic_interval")
		}
	}
	t.previous[target.PID] = baseline{*raw.CPUNS.Value, mid}
	return p
}
