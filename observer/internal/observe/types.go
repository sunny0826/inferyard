package observe

import "runtime"

const Definition = "lab_observer.v2"

type Metric[T any] struct {
	Value  *T      `json:"value"`
	Reason *string `json:"reason"`
	Source string  `json:"source"`
}

func Known[T any](v T, source string) Metric[T] { return Metric[T]{Value: &v, Source: source} }
func Missing[T any](reason, source string) Metric[T] {
	return Metric[T]{Reason: &reason, Source: source}
}

type Envelope struct {
	SchemaVersion int    `json:"schema_version"`
	Definition    string `json:"definition"`
	Kind          string `json:"kind"`
	SessionID     string `json:"session_id"`
}

func Base(kind, session string) Envelope { return Envelope{3, Definition, kind, session} }

type Inventory struct {
	OS          string           `json:"os"`
	Arch        string           `json:"arch"`
	OSVersion   Metric[string]   `json:"os_version"`
	CPUModel    Metric[string]   `json:"cpu_model"`
	LogicalCPUs int              `json:"logical_cpus"`
	MemoryTotal Metric[uint64]   `json:"memory_total_bytes"`
	Graphics    Metric[[]string] `json:"graphics_devices"`
	Limitations []string         `json:"limitations"`
}

func InventoryBase() Inventory {
	return Inventory{OS: runtime.GOOS, Arch: runtime.GOARCH, LogicalCPUs: runtime.NumCPU(),
		Limitations: []string{"diagnostic_only", "graphics_inventory_not_backend_verification",
			"gpu_utilization_temperature_energy_not_collected", "no_model_load_or_generation_proof",
			"logical_cpus_are_visible_to_runtime_not_a_quota_measurement"}}
}

type HostMetrics struct {
	MemoryAvailable Metric[uint64] `json:"memory_available_bytes"`
	DiskAvailable   Metric[uint64] `json:"disk_available_bytes"`
}

type Target struct {
	PID        int    `json:"pid"`
	StartTicks uint64 `json:"process_start_ticks"`
	Name       string `json:"name"`
}

type Process struct {
	Target
	ReadStartedNS  int64           `json:"read_started_ns"`
	ReadFinishedNS int64           `json:"read_finished_ns"`
	State          string          `json:"state"`
	CPUNS          Metric[uint64]  `json:"cpu_total_ns"`
	RSS            Metric[uint64]  `json:"rss_bytes"`
	CPUPercent     Metric[float64] `json:"cpu_percent_one_core"`
}

type RawProcess struct {
	Target Target
	CPUNS  Metric[uint64]
	RSS    Metric[uint64]
}

type Binding struct {
	RunID        string `json:"run_id"`
	RunSHA256    string `json:"run_file_sha256"`
	ConfigSHA256 string `json:"config_file_sha256"`
	ModelLabel   string `json:"model_label_declared"`
	Backend      string `json:"backend_declared"`
}

type Snapshot struct {
	Envelope
	System          Inventory   `json:"system"`
	Host            HostMetrics `json:"host"`
	Candidates      []Target    `json:"candidates"`
	DiscoveryReason *string     `json:"discovery_reason"`
}

type Header struct {
	Envelope
	Version    string         `json:"tool_version"`
	SourceHash Metric[string] `json:"tool_source_sha256"`
	BinaryHash Metric[string] `json:"binary_sha256"`
	UTC        string         `json:"started_utc"`
	System     Inventory      `json:"system"`
	IntervalNS int64          `json:"interval_ns"`
	DurationNS int64          `json:"duration_ns"`
	Targets    []Target       `json:"targets"`
	Observer   Target         `json:"observer_target"`
	Binding    *Binding       `json:"benchmark_binding"`
	Clock      string         `json:"clock"`
	DiskScope  string         `json:"disk_scope"`
}

type Sample struct {
	Envelope
	Seq            int         `json:"seq"`
	ReadStartedNS  int64       `json:"read_started_ns"`
	ReadFinishedNS int64       `json:"read_finished_ns"`
	Host           HostMetrics `json:"host"`
	Processes      []Process   `json:"processes"`
	Observer       Process     `json:"observer"`
	Health         Health      `json:"endpoint_health"`
}

type End struct {
	Envelope
	Samples              int    `json:"samples"`
	ElapsedNS            int64  `json:"elapsed_ns"`
	Skipped              int64  `json:"skipped_intervals"`
	Reason               string `json:"stop_reason"`
	PerformanceQualified bool   `json:"performance_comparison_qualified"`
}
