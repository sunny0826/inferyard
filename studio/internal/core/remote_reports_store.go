package core

import (
	"encoding/json"
	"errors"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"strings"
)

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
	var envelope remoteReportExport
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, envelope, err
	}
	studioRaw := envelope.Report
	if len(studioRaw) == 0 {
		var document map[string]json.RawMessage
		if json.Unmarshal(raw, &document) == nil && document["schema_version"] != nil {
			studioRaw = raw
		}
	}
	if len(studioRaw) == 0 {
		return nil, envelope, os.ErrInvalid
	}
	return studioRaw, envelope, nil
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
			studioPath := filepath.Join(root, id, reportID, "studio.json")
			info, err := os.Stat(studioPath)
			if err != nil {
				continue
			}
			out = append(out, s.remoteSummary(meta, *report, studioPath, info.ModTime()))
		}
	}
	return out, nil
}
