package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	exclusivityPollInterval = 3 * time.Second
	exclusivitySidecarName  = "benchmark-exclusivity.json"
)

type exclusivityStore struct {
	Runs    map[string]string `json:"runs,omitempty"`
	Reports map[string]string `json:"reports,omitempty"`
}

func validExclusivityStatus(status string) bool {
	return status == ExclusivityExclusive || status == ExclusivityExternal || status == ExclusivityUnknown
}

func normalizeInitialExclusivity(raw string) string {
	s := strings.TrimSpace(raw)
	if s == ExclusivityExternal {
		return ExclusivityExternal
	}
	return ""
}

func mergeExclusivityStatus(current, next string) string {
	if current == ExclusivityExternal || next == ExclusivityExternal {
		return ExclusivityExternal
	}
	if current == "" {
		if validExclusivityStatus(next) {
			return next
		}
		return ExclusivityUnknown
	}
	if current == ExclusivityUnknown || next == ExclusivityUnknown {
		return ExclusivityUnknown
	}
	if current == ExclusivityExclusive && next == ExclusivityExclusive {
		return ExclusivityExclusive
	}
	return ExclusivityUnknown
}

func (s *Service) exclusivityFilePath() string {
	w := workspaceRoot(s.userSettings())
	if w == nil || strings.TrimSpace(*w) == "" {
		return ""
	}
	return filepath.Join(*w, exclusivitySidecarName)
}

func readExclusivityStore(path string) exclusivityStore {
	store := exclusivityStore{
		Runs:    map[string]string{},
		Reports: map[string]string{},
	}
	if path == "" {
		return store
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return store
	}
	var parsed exclusivityStore
	if json.Unmarshal(raw, &parsed) != nil {
		return store
	}
	if parsed.Runs != nil {
		store.Runs = parsed.Runs
	}
	if parsed.Reports != nil {
		store.Reports = parsed.Reports
	}
	return store
}

func writeExclusivityStore(path string, store exclusivityStore) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "benchmark-exclusivity-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (s *Service) recordRunExclusivity(runID, status string) {
	id := strings.TrimSpace(runID)
	if id == "" || !validExclusivityStatus(status) {
		return
	}
	path := s.exclusivityFilePath()
	if path == "" {
		return
	}
	s.exclusivityMu.Lock()
	defer s.exclusivityMu.Unlock()
	store := readExclusivityStore(path)
	store.Runs[id] = status
	_ = writeExclusivityStore(path, store)
}

func (s *Service) recordReportExclusivity(reportID string, runIDs []string) {
	id := strings.TrimSpace(reportID)
	if id == "" || len(runIDs) == 0 {
		return
	}
	path := s.exclusivityFilePath()
	if path == "" {
		return
	}
	s.exclusivityMu.Lock()
	defer s.exclusivityMu.Unlock()
	store := readExclusivityStore(path)
	status := aggregateRunExclusivity(store.Runs, runIDs)
	if status == ExclusivityUnknown {
		return
	}
	store.Reports[id] = status
	_ = writeExclusivityStore(path, store)
}

func aggregateRunExclusivity(runs map[string]string, runIDs []string) string {
	if len(runIDs) == 0 {
		return ExclusivityUnknown
	}
	status := ""
	for _, id := range runIDs {
		st, ok := runs[strings.TrimSpace(id)]
		if !ok || !validExclusivityStatus(st) {
			st = ExclusivityUnknown
		}
		status = mergeExclusivityStatus(status, st)
	}
	if status == "" {
		return ExclusivityUnknown
	}
	return status
}

// ReportExclusivity returns the recorded external-connection exclusivity status for a
// report reference or run ID ("exclusive", "external", or "unknown" when unrecorded).
func (s *Service) ReportExclusivity(reportRef string) string {
	ref := strings.TrimSpace(reportRef)
	if ref == "" {
		return ExclusivityUnknown
	}
	path := s.exclusivityFilePath()
	s.exclusivityMu.Lock()
	store := readExclusivityStore(path)
	s.exclusivityMu.Unlock()

	if st, ok := store.Reports[ref]; ok && validExclusivityStatus(st) {
		return st
	}
	if base := filepath.Base(ref); base != "" && base != "." && base != "/" {
		if st, ok := store.Reports[base]; ok && validExclusivityStatus(st) {
			return st
		}
	}
	if st, ok := store.Runs[ref]; ok && validExclusivityStatus(st) {
		return st
	}
	if report, err := s.ReportIndex(ref); err == nil && report != nil && len(report.Runs) > 0 {
		ids := make([]string, 0, len(report.Runs))
		for _, r := range report.Runs {
			if r.Source.RunID != "" {
				ids = append(ids, r.Source.RunID)
			}
		}
		return aggregateRunExclusivity(store.Runs, ids)
	}
	return ExclusivityUnknown
}

// ReportExclusivities builds a batch map of reportID -> exclusivity status using a single
// sidecar read and ReportSummary.RunIds (no per-report studio.json reads).
func (s *Service) ReportExclusivities(reports []ReportSummary) map[string]string {
	out := make(map[string]string, len(reports))
	if len(reports) == 0 {
		return out
	}
	path := s.exclusivityFilePath()
	s.exclusivityMu.Lock()
	store := readExclusivityStore(path)
	s.exclusivityMu.Unlock()
	for _, rep := range reports {
		id := strings.TrimSpace(rep.ReportId)
		if id == "" {
			continue
		}
		if st, ok := store.Reports[id]; ok && validExclusivityStatus(st) {
			out[id] = st
			continue
		}
		runIDs := rep.RunIds
		if len(runIDs) == 0 && len(rep.RunSummaries) > 0 {
			runIDs = make([]string, 0, len(rep.RunSummaries))
			for _, rs := range rep.RunSummaries {
				if rs.RunID != "" {
					runIDs = append(runIDs, rs.RunID)
				}
			}
		}
		out[id] = aggregateRunExclusivity(store.Runs, runIDs)
	}
	return out
}

func (s *Service) runExclusivityFromStore(runID, reportRef string) string {
	path := s.exclusivityFilePath()
	s.exclusivityMu.Lock()
	store := readExclusivityStore(path)
	s.exclusivityMu.Unlock()
	if runID != "" {
		if st, ok := store.Runs[runID]; ok && validExclusivityStatus(st) {
			return st
		}
	}
	if reportRef != "" {
		if st, ok := store.Reports[reportRef]; ok && validExclusivityStatus(st) {
			return st
		}
	}
	return ExclusivityUnknown
}

type runExclusivityTracker struct {
	service     *Service
	port        int
	excludePIDs []int
	status      string
	lastProbe   time.Time
}

func (s *Service) startRunExclusivity(ctx context.Context, t *runEntry) *runExclusivityTracker {
	if t != nil && t.initialExclusivity == ExclusivityExternal {
		tr := &runExclusivityTracker{service: s, status: ExclusivityExternal}
		s.mu.Lock()
		t.Task.Exclusivity = ExclusivityExternal
		s.mu.Unlock()
		return tr
	}
	port, excludePIDs := s.resolveRunProbeTarget(t)
	tr := &runExclusivityTracker{
		service:     s,
		port:        port,
		excludePIDs: excludePIDs,
	}
	if port > 0 {
		tr.probeNow(ctx, t)
	} else {
		tr.status = ExclusivityUnknown
		s.mu.Lock()
		t.Task.Exclusivity = ExclusivityUnknown
		s.mu.Unlock()
	}
	return tr
}

func (tr *runExclusivityTracker) poll(ctx context.Context, t *runEntry) {
	if tr == nil || tr.port <= 0 || tr.status == ExclusivityExternal {
		return
	}
	if !tr.lastProbe.IsZero() && time.Since(tr.lastProbe) < exclusivityPollInterval {
		return
	}
	tr.probeNow(ctx, t)
}

func (tr *runExclusivityTracker) probeNow(ctx context.Context, t *runEntry) {
	res := ProbePortExternalConnections(ctx, tr.port, tr.excludePIDs)
	tr.lastProbe = time.Now()
	if tr.status == "" {
		tr.status = res.Status
	} else {
		tr.status = mergeExclusivityStatus(tr.status, res.Status)
	}
	tr.service.mu.Lock()
	if t.Task.Exclusivity != tr.status {
		t.Task.Exclusivity = tr.status
	}
	tr.service.mu.Unlock()
}

func (tr *runExclusivityTracker) finish(t *runEntry) {
	if tr == nil {
		return
	}
	status := tr.status
	if !validExclusivityStatus(status) {
		status = ExclusivityUnknown
	}
	tr.service.mu.Lock()
	t.Task.Exclusivity = status
	runID := str(t.Task.RunId)
	var batchRunIDs []string
	if t.Task.ContextSpeed != nil && t.Task.ContextSpeed.Batch != nil {
		for _, r := range t.Task.ContextSpeed.Batch.Runs {
			if r.RunID != "" {
				batchRunIDs = append(batchRunIDs, r.RunID)
			}
		}
	}
	tr.service.mu.Unlock()
	if runID != "" {
		tr.service.recordRunExclusivity(runID, status)
	}
	for _, id := range batchRunIDs {
		tr.service.recordRunExclusivity(id, status)
	}
}

func (s *Service) resolveRunProbeTarget(t *runEntry) (int, []int) {
	var port int
	var pids []int
	if t.probeEndpoint != "" {
		port = portFromEndpoint(t.probeEndpoint)
	}
	if t.probePID > 0 {
		pids = append(pids, t.probePID)
	}
	if port <= 0 && t.local != nil && t.local.bound != nil {
		port = portFromEndpoint(t.local.bound.Endpoint)
		if t.local.bound.Pid > 0 {
			pids = append(pids, int(t.local.bound.Pid))
		}
	}
	if port <= 0 && t.Task.ConfigPath != nil {
		cfgPort, cfgPID := readConfigEndpointTarget(*t.Task.ConfigPath)
		if cfgPort > 0 {
			port = cfgPort
		}
		if cfgPID > 0 {
			pids = append(pids, cfgPID)
		}
	}
	if port <= 0 && t.Task.ResumeFrom != nil && *t.Task.ResumeFrom != "" {
		cfgPort, cfgPID := readInputsEndpointTarget(*t.Task.ResumeFrom)
		if cfgPort > 0 {
			port = cfgPort
		}
		if cfgPID > 0 {
			pids = append(pids, cfgPID)
		}
	}
	if port <= 0 && t.Task.ContextSpeed != nil {
		eng := s.Engine()
		if p := portFromEndpoint(eng.Endpoint); p > 0 {
			port = p
		}
		if eng.Pid > 0 {
			pids = append(pids, int(eng.Pid))
		}
	}
	if port > 0 {
		pids = append(pids, s.managedPIDsOnPort(port)...)
	}
	return port, pids
}

func readInputsEndpointTarget(runDir string) (int, int) {
	if strings.TrimSpace(runDir) == "" {
		return 0, 0
	}
	raw, err := os.ReadFile(filepath.Join(runDir, "inputs.json"))
	if err != nil {
		return 0, 0
	}
	ep := obj(payload(raw, "config")["endpoint"])
	if ep == nil {
		return 0, 0
	}
	var serverPID int
	if p := integer(ep["server_pid"]); p != nil && *p > 0 {
		serverPID = int(*p)
	}
	return portFromEndpoint(str(text(ep["url"]))), serverPID
}

func readConfigEndpointTarget(configPath string) (int, int) {
	if configPath == "" {
		return 0, 0
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return 0, 0
	}
	var endpointURL string
	var serverPID int
	section := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[]")
			continue
		}
		if section != "endpoint" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "url":
			_ = json.Unmarshal([]byte(strings.TrimSpace(v)), &endpointURL)
		case "server_pid":
			var p int64
			if json.Unmarshal([]byte(strings.TrimSpace(v)), &p) == nil && p > 0 {
				serverPID = int(p)
			}
		}
	}
	return portFromEndpoint(endpointURL), serverPID
}

func descendantPIDsOf(rootPID int) []int {
	if runtime.GOOS == "windows" || rootPID <= 0 {
		return nil
	}
	rows, err := engineProcessTable()
	if err != nil {
		return nil
	}
	children := map[int64][]int64{}
	for _, r := range rows {
		children[r.parent] = append(children[r.parent], r.pid)
	}
	var out []int
	queue := []int64{int64(rootPID)}
	seen := map[int64]bool{int64(rootPID): true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, ch := range children[cur] {
			if seen[ch] || ch <= 0 {
				continue
			}
			seen[ch] = true
			out = append(out, int(ch))
			queue = append(queue, ch)
		}
	}
	return out
}
