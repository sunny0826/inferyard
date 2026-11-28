package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

type nodeStoreFile struct {
	Version int          `json:"version"`
	Nodes   []NodeRecord `json:"nodes"`
}

func loadNodeStore(path string) (map[string]NodeRecord, error) {
	out := map[string]NodeRecord{}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var stored nodeStoreFile
	if json.Unmarshal(raw, &stored) != nil {
		var records []NodeRecord
		if json.Unmarshal(raw, &records) != nil {
			return out, nil
		}
		stored.Nodes = records
	}
	for _, record := range stored.Nodes {
		if !validNodeID(record.NodeID) || record.Transport != NodeTransportMTLS && record.Transport != NodeTransportSSH {
			continue
		}
		if record.DisplayName == "" {
			record.DisplayName = record.Hostname
		}
		out[record.NodeID] = record
	}
	return out, nil
}

func (m *NodeManager) persistLocked() error {
	records := make([]NodeRecord, 0, len(m.records))
	for _, record := range m.records {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].NodeID < records[j].NodeID })
	raw, err := json.MarshalIndent(nodeStoreFile{Version: 1, Nodes: records}, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(m.storePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "nodes-*.json")
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
	return os.Rename(tmp, m.storePath)
}
