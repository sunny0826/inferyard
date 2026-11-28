package core

import (
	"context"
	"encoding/json"
	"errors"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/remote"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const remoteReportRefPrefix = "remote://"

type RemoteReportImportMeta struct {
	NodeID          string    `json:"node_id"`
	NodeName        string    `json:"node_name"`
	Platform        string    `json:"platform"`
	RemoteReportID  string    `json:"remote_report_id"`
	RemoteRunIDs    []string  `json:"remote_run_ids"`
	RemoteRunPaths  []string  `json:"remote_run_paths"`
	VerifiedFromRaw bool      `json:"verified_from_raw"`
	ImportedAt      time.Time `json:"imported_at"`
}

type UnifiedReportSummary struct {
	ReportSummary
	ReportRef       string   `json:"report_ref"`
	NodeID          string   `json:"node_id"`
	NodeName        string   `json:"node_name"`
	IsRemote        bool     `json:"is_remote"`
	RemoteReportID  string   `json:"remote_report_id,omitempty"`
	VerifiedFromRaw bool     `json:"verified_from_raw"`
	SourceOnline    bool     `json:"source_online"`
	CanResume       bool     `json:"can_resume"`
	CanRerun        bool     `json:"can_rerun"`
	RemoteRunPaths  []string `json:"remote_run_paths,omitempty"`
}

type remoteReportExport struct {
	StudioJSON      json.RawMessage `json:"studio_json"`
	Studio          json.RawMessage `json:"studio"`
	Report          json.RawMessage `json:"report"`
	RemoteRunIDs    []string        `json:"remote_run_ids"`
	RemoteRunPaths  []string        `json:"remote_run_paths"`
	VerifiedFromRaw bool            `json:"verified_from_raw"`
}

func RemoteReportReference(nodeID, remoteReportID string) string {
	return remoteReportRefPrefix + nodeID + "/" + remoteReportID
}

func (s *Service) remoteReportsRoot() (string, *ApiError) {
	u := s.userSettings()
	workspace := workspaceRoot(u)
	if s.paths != nil && strings.TrimSpace(s.paths.Workspace) != "" {
		workspace = &s.paths.Workspace
	}
	if workspace == nil || strings.TrimSpace(*workspace) == "" {
		return "", Err("settings_incomplete")
	}
	return filepath.Join(*workspace, "remote_reports"), nil
}

func ensureCacheDirectory(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return os.ErrInvalid
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Mkdir(path, 0o700)
}

func (s *Service) remoteReportDir(nodeID, reportID string, create bool) (string, *ApiError) {
	if !validNodeID(nodeID) || !validRemoteCacheID(reportID) {
		return "", Err("invalid_request")
	}
	root, apiErr := s.remoteReportsRoot()
	if apiErr != nil {
		return "", apiErr
	}
	nodeDir := filepath.Join(root, nodeID)
	reportDir := filepath.Join(nodeDir, reportID)
	if create {
		workspace := filepath.Dir(root)
		if err := os.MkdirAll(workspace, 0o700); err != nil {
			return "", Err("internal_error")
		}
		for _, dir := range []string{root, nodeDir, reportDir} {
			if err := ensureCacheDirectory(dir); err != nil {
				return "", Err("internal_error")
			}
		}
		return reportDir, nil
	}
	for _, dir := range []string{root, nodeDir, reportDir} {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", Err("not_found")
		}
	}
	return reportDir, nil
}

func writeRemoteCacheFile(dir, name string, raw []byte) error {
	file, err := os.CreateTemp(dir, ".import-*.tmp")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}

func decodeRemoteReportExport(raw json.RawMessage) ([]byte, remoteReportExport, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, remoteReportExport{}, err
	}
	var envelope remoteReportExport
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, envelope, err
	}
	studioRaw := envelope.StudioJSON
	if len(studioRaw) == 0 {
		studioRaw = envelope.Studio
	}
	if len(studioRaw) == 0 {
		studioRaw = envelope.Report
	}
	if len(studioRaw) == 0 && document["schema_version"] != nil {
		studioRaw = raw
	}
	var quoted string
	if len(studioRaw) > 0 && json.Unmarshal(studioRaw, &quoted) == nil {
		studioRaw = []byte(quoted)
	}
	if len(studioRaw) == 0 {
		return nil, envelope, os.ErrInvalid
	}
	return studioRaw, envelope, nil
}

func (s *Service) SyncNodeReport(ctx context.Context, nodeID, remoteReportID string) (UnifiedReportSummary, *ApiError) {
	if s.nodes == nil || !validRemoteCacheID(remoteReportID) {
		return UnifiedReportSummary{}, Err("invalid_request")
	}
	params := map[string]any{"remote_report_id": remoteReportID, "report_id": remoteReportID}
	var raw json.RawMessage
	if err := s.nodes.call(ctx, nodeID, remote.MethodReportExport, params, &raw); err != nil {
		return UnifiedReportSummary{}, err
	}
	studioRaw, envelope, err := decodeRemoteReportExport(raw)
	if err != nil {
		return UnifiedReportSummary{}, Err("report_unreadable")
	}
	report, err := inferyard.ParseStudioReport(studioRaw)
	if err != nil {
		return UnifiedReportSummary{}, Err("report_unreadable")
	}
	s.nodes.mu.RLock()
	record := s.nodes.records[nodeID]
	s.nodes.mu.RUnlock()
	meta := RemoteReportImportMeta{
		NodeID: nodeID, NodeName: record.DisplayName, Platform: record.Platform,
		RemoteReportID: remoteReportID, RemoteRunIDs: envelope.RemoteRunIDs,
		RemoteRunPaths: envelope.RemoteRunPaths, VerifiedFromRaw: envelope.VerifiedFromRaw,
		ImportedAt: time.Now().UTC(),
	}
	if len(meta.RemoteRunIDs) == 0 {
		for _, run := range report.Runs {
			meta.RemoteRunIDs = append(meta.RemoteRunIDs, run.Source.RunID)
		}
	}
	if len(meta.RemoteRunPaths) == 0 {
		for _, run := range report.Runs {
			meta.RemoteRunPaths = append(meta.RemoteRunPaths, run.Source.Path)
		}
	}
	dir, apiErr := s.remoteReportDir(nodeID, remoteReportID, true)
	if apiErr != nil {
		return UnifiedReportSummary{}, apiErr
	}
	metaRaw, _ := json.MarshalIndent(meta, "", "  ")
	if err := writeRemoteCacheFile(dir, "studio.json", studioRaw); err != nil {
		return UnifiedReportSummary{}, Err("internal_error")
	}
	if err := writeRemoteCacheFile(dir, "import_meta.json", append(metaRaw, '\n')); err != nil {
		return UnifiedReportSummary{}, Err("internal_error")
	}
	summary := s.remoteSummary(meta, report, filepath.Join(dir, "studio.json"), meta.ImportedAt)
	s.Bus.Publish("remote_report.imported", map[string]any{"node_id": nodeID, "report": summary})
	return summary, nil
}

func (s *Service) RemoteReportMetadata(nodeID, remoteReportID string) (RemoteReportImportMeta, *ApiError) {
	dir, apiErr := s.remoteReportDir(nodeID, remoteReportID, false)
	if apiErr != nil {
		return RemoteReportImportMeta{}, apiErr
	}
	raw, err := os.ReadFile(filepath.Join(dir, "import_meta.json"))
	if err != nil {
		return RemoteReportImportMeta{}, Err("not_found")
	}
	var meta RemoteReportImportMeta
	if json.Unmarshal(raw, &meta) != nil || meta.NodeID != nodeID || meta.RemoteReportID != remoteReportID {
		return RemoteReportImportMeta{}, Err("report_unreadable")
	}
	return meta, nil
}

func (s *Service) remoteSummary(meta RemoteReportImportMeta, report ReportIndex, studioPath string, modified time.Time) UnifiedReportSummary {
	summary := ReportSummary{
		ReportId: meta.RemoteReportID, Path: filepath.Dir(studioPath),
		ModifiedAt: ptr(modified.UTC().Format(time.RFC3339Nano)),
		RunIds:     append([]string(nil), meta.RemoteRunIDs...), RunPaths: make([]*string, len(report.Runs)),
	}
	for _, run := range report.Runs {
		summary.RunSummaries = append(summary.RunSummaries, ReportListRun{
			RunID: run.Source.RunID, Model: run.Model, BundleLabel: run.Facets.BundleLabel,
			Title: run.Profile.Title, Quality: run.Dashboard.Quality,
		})
	}
	online := false
	if s.nodes != nil {
		s.nodes.mu.RLock()
		runtime := s.nodes.runtimes[meta.NodeID]
		s.nodes.mu.RUnlock()
		online = runtime != nil && snapshotRemoteRuntime(runtime).Online
	}
	canRun := online && validRemoteRunPaths(meta.RemoteRunPaths)
	return UnifiedReportSummary{
		ReportSummary: summary, ReportRef: RemoteReportReference(meta.NodeID, meta.RemoteReportID),
		NodeID: meta.NodeID, NodeName: meta.NodeName, IsRemote: true,
		RemoteReportID: meta.RemoteReportID, VerifiedFromRaw: meta.VerifiedFromRaw,
		SourceOnline: online, CanResume: canRun, CanRerun: canRun,
		RemoteRunPaths: append([]string(nil), meta.RemoteRunPaths...),
	}
}

func (s *Service) RemoteReportIndex(nodeID, remoteReportID string) (*ReportIndex, *ApiError) {
	dir, apiErr := s.remoteReportDir(nodeID, remoteReportID, false)
	if apiErr != nil {
		return nil, apiErr
	}
	raw, err := os.ReadFile(filepath.Join(dir, "studio.json"))
	if err != nil {
		return nil, Err("not_found")
	}
	report, err := inferyard.ParseStudioReport(raw)
	if err != nil {
		return nil, reportUnreadable()
	}
	return &report, nil
}

func (s *Service) remoteReportSummaries(nodeID string) ([]UnifiedReportSummary, *ApiError) {
	root, apiErr := s.remoteReportsRoot()
	if apiErr != nil {
		return nil, apiErr
	}
	if nodeID != "" && !validNodeID(nodeID) {
		return nil, Err("invalid_request")
	}
	nodeEntries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []UnifiedReportSummary{}, nil
	}
	if err != nil {
		return nil, Err("internal_error")
	}
	out := []UnifiedReportSummary{}
	for _, nodeEntry := range nodeEntries {
		id := nodeEntry.Name()
		if nodeID != "" && id != nodeID || !validNodeID(id) || !nodeEntry.IsDir() || nodeEntry.Type()&os.ModeSymlink != 0 {
			continue
		}
		reportEntries, err := os.ReadDir(filepath.Join(root, id))
		if err != nil {
			continue
		}
		for _, reportEntry := range reportEntries {
			reportID := reportEntry.Name()
			if !reportEntry.IsDir() || reportEntry.Type()&os.ModeSymlink != 0 || !validRemoteCacheID(reportID) {
				continue
			}
			meta, metaErr := s.RemoteReportMetadata(id, reportID)
			report, reportErr := s.RemoteReportIndex(id, reportID)
			if metaErr != nil || reportErr != nil {
				continue
			}
			info, err := os.Stat(filepath.Join(root, id, reportID, "studio.json"))
			if err != nil {
				continue
			}
			studioPath := filepath.Join(root, id, reportID, "studio.json")
			out = append(out, s.remoteSummary(meta, *report, studioPath, info.ModTime()))
		}
	}
	return out, nil
}

func (s *Service) AllReports() ([]UnifiedReportSummary, *ApiError) {
	_, local, apiErr := s.Reports()
	if apiErr != nil {
		return nil, apiErr
	}
	out := make([]UnifiedReportSummary, 0, len(local))
	for _, summary := range local {
		canResume := false
		for _, path := range summary.RunPaths {
			canResume = canResume || path != nil
		}
		out = append(out, UnifiedReportSummary{
			ReportSummary: summary, ReportRef: summary.ReportId, NodeID: LocalNodeID,
			NodeName: LocalNodeID, SourceOnline: true, CanResume: canResume, CanRerun: len(summary.RunIds) > 0,
		})
	}
	remoteSummaries, remoteErr := s.remoteReportSummaries("")
	if remoteErr != nil && remoteErr.Code != "settings_incomplete" {
		return nil, remoteErr
	}
	out = append(out, remoteSummaries...)
	sort.SliceStable(out, func(i, j int) bool {
		if str(out[i].ModifiedAt) != str(out[j].ModifiedAt) {
			return str(out[i].ModifiedAt) > str(out[j].ModifiedAt)
		}
		return out[i].ReportRef < out[j].ReportRef
	})
	return out, nil
}

func validRemoteRunPaths(paths []string) bool {
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if !validOpaqueRemotePath(path) {
			return false
		}
	}
	return true
}

func (s *Service) resumeOrRerunRemoteReport(ctx context.Context, nodeID, remoteReportID, action string) ([]RunTask, *ApiError) {
	meta, apiErr := s.RemoteReportMetadata(nodeID, remoteReportID)
	if apiErr != nil {
		return nil, apiErr
	}
	if s.nodes == nil || !validRemoteRunPaths(meta.RemoteRunPaths) {
		return nil, Err("invalid_request")
	}
	if _, apiErr := s.nodes.peer(nodeID); apiErr != nil {
		return nil, apiErr
	}
	params := map[string]any{
		"remote_report_id": remoteReportID, "remote_run_ids": meta.RemoteRunIDs,
		"remote_run_paths": meta.RemoteRunPaths,
	}
	return s.startNodeBenchmark(ctx, nodeID, action, params)
}

func (s *Service) ResumeRemoteReport(ctx context.Context, nodeID, remoteReportID string) ([]RunTask, *ApiError) {
	return s.resumeOrRerunRemoteReport(ctx, nodeID, remoteReportID, "resume")
}

func (s *Service) RerunRemoteReport(ctx context.Context, nodeID, remoteReportID string) ([]RunTask, *ApiError) {
	return s.resumeOrRerunRemoteReport(ctx, nodeID, remoteReportID, "rerun")
}
