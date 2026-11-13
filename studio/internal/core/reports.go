package core

import (
	"fmt"
	"inferyard/studio/internal/evidence"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ReportIndex aliases the typed studio.json projection. The CLI contract lives
// in inferyard so Studio never rescans evidence or recomputes presentation.
type (
	ReportIndex     = inferyard.StudioReport
	ReportRun       = inferyard.StudioRun
	ReportSource    = inferyard.StudioSource
	ReportFacets    = inferyard.StudioFacets
	ReportProfile   = inferyard.StudioProfile
	ReportEngine    = inferyard.StudioEngine
	ReportMachine   = inferyard.StudioMachine
	ReportCounts    = inferyard.StudioCounts
	ReportQuality   = inferyard.StudioQuality
	ReportCategory  = inferyard.StudioCategory
	ReportLatency   = inferyard.StudioLatency
	ReportDashboard = inferyard.StudioDashboard
	ReportRequest   = inferyard.StudioRequest
)

func reportSummary(path string) (ReportSummary, error) {
	r := ReportSummary{ReportId: filepath.Base(path), Path: path, RunIds: []string{}, RunPaths: []*string{}}
	info, e := os.Stat(path)
	if e != nil {
		return r, e
	}
	r.CreatedAt = ptr(info.ModTime().UTC().Format(time.RFC3339Nano))
	raw, e := os.ReadFile(filepath.Join(path, "studio.json"))
	if e != nil {
		return r, e
	}
	report, e := inferyard.ParseStudioReport(raw)
	if e != nil {
		return r, e
	}
	for _, run := range report.Runs {
		r.RunIds = append(r.RunIds, run.Source.RunID)
		var p *string
		if run.Source.Path != "" {
			v := run.Source.Path
			if !filepath.IsAbs(v) {
				v = filepath.Join(path, v)
			}
			if st, e := os.Stat(filepath.Join(v, "inputs.json")); e == nil && st.Mode().IsRegular() {
				v, _ = filepath.Abs(v)
				p = &v
			}
		}
		r.RunPaths = append(r.RunPaths, p)
	}
	return r, nil
}
func (s *Service) Reports() (*string, []ReportSummary, *ApiError) {
	root := reportsRoot(s.userSettings())
	out := []ReportSummary{}
	if root == nil {
		return nil, out, nil
	}
	entries, e := os.ReadDir(*root)
	if os.IsNotExist(e) {
		return root, out, nil
	}
	if e != nil {
		return root, nil, Err("internal_error")
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(*root, entry.Name())
		if _, e := os.Stat(filepath.Join(path, "studio.json")); e != nil {
			continue
		}
		r, _ := reportSummary(path)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return str(out[i].CreatedAt) > str(out[j].CreatedAt) })
	return root, out, nil
}
func (s *Service) CreateReport(req CreateReportRequest) (ReportSummary, *ApiError) {
	if len(req.RunPaths) < 1 || len(req.RunPaths) > 100 {
		return ReportSummary{}, Err("invalid_request")
	}
	u := s.userSettings()
	if u.ResultsRoot == nil {
		return ReportSummary{}, Conflict("settings_incomplete")
	}
	paths := []string{}
	seen := map[string]bool{}
	for _, p := range req.RunPaths {
		if !filepath.IsAbs(p) {
			return ReportSummary{}, Err("invalid_request")
		}
		p, e := Resolve(*u.ResultsRoot, p)
		if e != nil || seen[p] {
			return ReportSummary{}, Err("invalid_request")
		}
		raw, readErr := os.ReadFile(filepath.Join(p, "inputs.json"))
		if readErr == nil && evidence.IsContextSpeedRun(raw) {
			return ReportSummary{}, Err("context_speed_report_forbidden")
		}
		seen[p] = true
		paths = append(paths, p)
	}
	h, e := s.scan(u)
	if e != nil {
		return ReportSummary{}, e
	}
	known := map[string]bool{}
	for _, r := range h.StandaloneRuns {
		known[r.Path] = true
	}
	for _, b := range h.Batches {
		for _, r := range b.Runs {
			known[r.Path] = true
		}
	}
	for _, p := range paths {
		if !known[p] {
			return ReportSummary{}, Err("not_found")
		}
	}
	p := s.probe(u)
	if p.Status != "ok" {
		return ReportSummary{}, Err("cli_unavailable")
	}
	s.reportMu.Lock()
	defer s.reportMu.Unlock()
	root := *reportsRoot(u)
	if e := os.MkdirAll(root, 0700); e != nil {
		return ReportSummary{}, Err("internal_error")
	}
	base := "report-" + time.Now().UTC().Format("20060102T150405Z")
	path := filepath.Join(root, base)
	for n := 2; ; n++ {
		_, e := os.Lstat(path)
		if os.IsNotExist(e) {
			break
		}
		if e != nil {
			return ReportSummary{}, Err("internal_error")
		}
		path = filepath.Join(root, fmt.Sprintf("%s-%d", base, n))
	}
	args := append([]string{"report", "--runs"}, paths...)
	args = append(args, "--out", path)
	out, err := s.exec(s.ctx, *p.Path, args...)
	if e := exitError(out, err); e != nil {
		return ReportSummary{}, e
	}
	if out.ExitCode != 0 {
		return ReportSummary{}, Err("cli_error")
	}
	if _, e := inferyard.Parse(out.Stdout); e != nil {
		return ReportSummary{}, Err("internal_error")
	}
	if !validStudioReport(path) {
		return ReportSummary{}, Err("internal_error")
	}
	r, err := reportSummary(path)
	if err != nil {
		return r, Err("internal_error")
	}
	s.Bus.Publish("report.created", map[string]any{"report": r})
	return r, nil
}

// validStudioReport checks that a created directory holds the single typed
// document Studio consumes. Old index.json/report.html output is not accepted.
func validStudioReport(path string) bool {
	raw, e := os.ReadFile(filepath.Join(path, "studio.json"))
	if e != nil {
		return false
	}
	_, e = inferyard.ParseStudioReport(raw)
	return e == nil
}

func (s *Service) ReportFile(relative string) (string, *ApiError) {
	root := reportsRoot(s.userSettings())
	if root == nil {
		return "", Err("not_found")
	}
	if relative == "" || strings.Contains(relative, "\\") || strings.ContainsRune(relative, 0) {
		return "", Err("invalid_request")
	}
	parts := strings.Split(relative, "/")
	if len(parts) < 2 {
		return "", Err("invalid_request")
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return "", Err("invalid_request")
		}
	}
	path, e := Resolve(filepath.Join(*root, parts[0]), filepath.Join(*root, relative))
	if e != nil {
		return "", Err("invalid_request")
	}
	if _, e = Resolve(*root, path); e != nil {
		return "", Err("invalid_request")
	}
	st, e := os.Stat(path)
	if e != nil || !st.Mode().IsRegular() {
		return "", Err("not_found")
	}
	return path, nil
}

func (s *Service) ReportIndex(reference string) (*ReportIndex, *ApiError) {
	if filepath.IsAbs(reference) {
		root := reportsRoot(s.userSettings())
		if root == nil {
			return nil, Err("not_found")
		}
		resolvedRoot, err := canonical(*root)
		if err != nil {
			return nil, Err("invalid_request")
		}
		resolved, err := Resolve(resolvedRoot, reference)
		if err != nil {
			return nil, Err("invalid_request")
		}
		relative, err := filepath.Rel(resolvedRoot, resolved)
		if err != nil {
			return nil, Err("invalid_request")
		}
		reference = filepath.ToSlash(relative)
	}
	path, e := s.ReportFile(reference + "/studio.json")
	if e != nil {
		if e.Code == "not_found" && s.legacyReport(reference) {
			return nil, &ApiError{Code: "unsupported_report_format", Message: "旧格式报告不受支持，请用当前 CLI 重新生成报告。"}
		}
		return nil, e
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, reportUnreadable()
	}
	report, err := inferyard.ParseStudioReport(raw)
	if err != nil {
		return nil, reportUnreadable()
	}
	return &report, nil
}

// legacyReport reports whether a directory still holds the retired HTML/JSON
// presentation. Such directories stay hidden from batch discovery but an
// explicit open explains that the format is unsupported.
func (s *Service) legacyReport(reference string) bool {
	for _, name := range []string{"index.json", "report.html"} {
		if _, e := s.ReportFile(reference + "/" + name); e == nil {
			return true
		}
	}
	return false
}

func reportUnreadable() *ApiError {
	return &ApiError{Code: "report_unreadable", Message: "无法读取"}
}
