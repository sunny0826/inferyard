package core

import (
	"fmt"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func reportSummary(path string) (ReportSummary, error) {
	r := ReportSummary{ReportId: filepath.Base(path), Path: path, RunIds: []string{}, RunPaths: []*string{}, HtmlUrl: "/reports/" + filepath.Base(path) + "/report.html"}
	info, e := os.Stat(path)
	if e != nil {
		return r, e
	}
	r.CreatedAt = ptr(info.ModTime().UTC().Format(time.RFC3339Nano))
	raw, e := os.ReadFile(filepath.Join(path, "index.json"))
	if os.IsNotExist(e) {
		return r, nil
	}
	if e != nil {
		return r, e
	}
	rows, _ := document(raw)["runs"].([]any)
	for _, row := range rows {
		m := obj(row)
		id := text(m["run_id"])
		if id == nil {
			id = text(obj(m["source"])["run_id"])
		}
		r.RunIds = append(r.RunIds, str(id))
		source := text(obj(m["source"])["path"])
		var p *string
		if source != nil {
			v := *source
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
		if _, e := os.Stat(filepath.Join(path, "report.html")); e != nil {
			continue
		}
		r, e := reportSummary(path)
		if e != nil {
			return root, nil, Err("internal_error")
		}
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
	if _, e := os.Stat(filepath.Join(path, "report.html")); e != nil {
		return ReportSummary{}, Err("internal_error")
	}
	r, err := reportSummary(path)
	if err != nil {
		return r, Err("internal_error")
	}
	s.Bus.Publish("report.created", map[string]any{"report": r})
	return r, nil
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
