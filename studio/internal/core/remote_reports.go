package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/remote"
	"path/filepath"
	"sort"
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
	Report          json.RawMessage `json:"report"`
	RemoteRunIDs    []string        `json:"remote_run_ids"`
	RemoteRunPaths  []string        `json:"remote_run_paths"`
	VerifiedFromRaw bool            `json:"verified_from_raw"`
}

func RemoteReportReference(nodeID, remoteReportID string) string {
	return remoteReportRefPrefix + nodeID + "/" + remoteReportID
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
