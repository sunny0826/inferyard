package observe

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const bindingFileLimit = 1 << 20
const inputsFileLimit = 64 << 20

func boundedFile(path string) ([]byte, error) {
	return boundedFileLimit(path, bindingFileLimit)
}

func boundedFileLimit(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("binding_file_unreadable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("binding_not_regular_file")
	}
	if info.Size() > limit {
		return nil, errors.New("binding_file_too_large")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("binding_file_too_large")
	}
	return data, nil
}

func digest(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }

func ReadBinding(root string) (Binding, Target, error) {
	if _, err := os.Lstat(filepath.Join(root, "inputs.json")); err == nil {
		return readInputsBinding(root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Binding{}, Target{}, errors.New("binding_file_unreadable")
	}
	var b Binding
	var target Target
	rawRun, err := boundedFile(filepath.Join(root, "run.json"))
	if err != nil {
		return b, target, err
	}
	rawConfig, err := boundedFile(filepath.Join(root, "config.frozen.json"))
	if err != nil {
		return b, target, err
	}
	var run struct {
		Schema int    `json:"schema_version"`
		RunID  string `json:"run_id"`
	}
	var config struct {
		Schema   int `json:"schema_version"`
		Endpoint struct {
			PID   int    `json:"server_pid"`
			Start uint64 `json:"process_start_ticks"`
		} `json:"endpoint"`
		Model struct {
			Name string `json:"display_name"`
		} `json:"model"`
		Engine struct {
			Backend string `json:"backend"`
		} `json:"engine"`
	}
	if StrictJSON(rawRun, &run) != nil || StrictJSON(rawConfig, &config) != nil || run.Schema != 3 ||
		config.Schema != 3 || run.RunID == "" || config.Endpoint.PID <= 0 ||
		config.Endpoint.PID > 2_147_483_647 || config.Endpoint.Start == 0 {
		return b, target, errors.New("benchmark_binding_invalid")
	}
	b = Binding{RunID: run.RunID, RunSHA256: digest(rawRun), ConfigSHA256: digest(rawConfig),
		ModelLabel: config.Model.Name, Backend: config.Engine.Backend}
	target = Target{PID: config.Endpoint.PID, StartTicks: config.Endpoint.Start}
	return b, target, nil
}
