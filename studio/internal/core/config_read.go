package core

import (
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"strings"
)

func readConfig(path string) (string, string, string, error) {
	cfgPath, root, bundle, _, _, err := readConfigWithEndpoint(path)
	return cfgPath, root, bundle, err
}

func readConfigWithEndpoint(path string) (string, string, string, int64, int64, error) {
	if !filepath.IsAbs(path) {
		return "", "", "", 0, 0, os.ErrInvalid
	}
	if st, e := os.Stat(path); e == nil && st.IsDir() {
		path = filepath.Join(path, "config.toml")
	}
	path, e := filepath.EvalSymlinks(path)
	if e != nil {
		return "", "", "", 0, 0, e
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return "", "", "", 0, 0, e
	}
	fields := map[string]json.RawMessage{}
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
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := section + "." + strings.TrimSpace(k)
		if key == "output.root" || key == "bundle.path" || key == "endpoint.server_pid" || key == "endpoint.process_start_ticks" {
			if _, exists := fields[key]; exists {
				return "", "", "", 0, 0, os.ErrInvalid
			}
			fields[key] = json.RawMessage(strings.TrimSpace(v))
		}
	}
	var root, bundle string
	var pid, ticks int64
	if json.Unmarshal(fields["output.root"], &root) != nil || json.Unmarshal(fields["bundle.path"], &bundle) != nil || json.Unmarshal(fields["endpoint.server_pid"], &pid) != nil || json.Unmarshal(fields["endpoint.process_start_ticks"], &ticks) != nil || pid <= 0 || ticks <= 0 || root == "" || bundle == "" {
		return "", "", "", 0, 0, os.ErrInvalid
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(filepath.Dir(path), root)
	}
	if !filepath.IsAbs(bundle) {
		bundle = filepath.Join(filepath.Dir(path), bundle)
	}
	bundleRaw, err := os.ReadFile(bundle)
	if err != nil {
		return "", "", "", 0, 0, err
	}
	var frozenBundle struct {
		ID string `json:"bundle_id"`
	}
	if json.Unmarshal(bundleRaw, &frozenBundle) != nil || frozenBundle.ID == "" {
		return "", "", "", 0, 0, os.ErrInvalid
	}
	bundleName := strings.TrimSuffix(filepath.Base(bundle), ".json")
	if bundleName == "" || bundleName == filepath.Base(bundle) {
		bundleName = inferyard.CanonicalBundleName(frozenBundle.ID)
	}
	return path, root, bundleName, pid, ticks, nil
}
