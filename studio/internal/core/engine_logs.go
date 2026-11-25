package core

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	maxEngineLogTailBytes     = 64 * 1024
	defaultEngineLogTailLines = 20
)

var openPathFunc = func(cmd string, args ...string) error {
	return exec.Command(cmd, args...).Start()
}

func (s *Service) configuredWorkspace() string {
	if s == nil {
		return ""
	}
	u := s.userSettings()
	if u.WorkspaceRoot != nil && *u.WorkspaceRoot != "" {
		return *u.WorkspaceRoot
	}
	if s.paths != nil && s.paths.Workspace != "" {
		return s.paths.Workspace
	}
	return ""
}

// ResolveEngineLogPath validates and resolves logPath within the service workspace logs directory.
func (s *Service) ResolveEngineLogPath(logPath string) (string, error) {
	if s == nil {
		return "", errors.New("service unavailable")
	}
	if strings.TrimSpace(logPath) == "" || strings.ContainsRune(logPath, 0) {
		return "", errors.New("invalid log path")
	}
	if workspace := s.configuredWorkspace(); workspace != "" {
		return Resolve(filepath.Join(workspace, "logs"), logPath)
	}
	if !filepath.IsAbs(logPath) {
		if w := workspaceRoot(s.userSettings()); w != nil && *w != "" {
			return Resolve(filepath.Join(*w, "logs"), logPath)
		}
		return "", errors.New("invalid log path")
	}
	return filepath.Clean(logPath), nil
}

// EngineLogTail reads the trailing lines of an engine log file safely within the configured workspace.
func (s *Service) EngineLogTail(logPath string, maxLines int) (string, error) {
	resolved, err := s.ResolveEngineLogPath(logPath)
	if err != nil {
		return "", err
	}
	return ReadLogTail(resolved, maxLines)
}

// readLogTailWindow reads at most the last 64 KiB of logPath as raw UTF-8 text.
func readLogTailWindow(logPath string) (string, error) {
	if strings.TrimSpace(logPath) == "" || strings.ContainsRune(logPath, 0) {
		return "", errors.New("invalid log path")
	}
	f, err := os.Open(logPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("log path is not a regular file")
	}
	size := info.Size()
	if size == 0 {
		return "", nil
	}

	truncated := false
	if size > maxEngineLogTailBytes {
		truncated = true
		if _, err := f.Seek(size-maxEngineLogTailBytes, io.SeekStart); err != nil {
			return "", err
		}
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxEngineLogTailBytes))
	if err != nil {
		return "", err
	}
	if truncated {
		if idx := bytes.IndexByte(raw, '\n'); idx >= 0 && len(bytes.TrimSpace(raw[idx+1:])) > 0 {
			raw = raw[idx+1:]
		} else {
			for len(raw) > 0 && raw[0]&0xC0 == 0x80 {
				raw = raw[1:]
			}
		}
	}
	return string(raw), nil
}

// ReadLogTail reads at most the last 64 KiB of logPath and returns up to maxLines trailing non-empty lines.
func ReadLogTail(logPath string, maxLines int) (string, error) {
	if maxLines <= 0 {
		maxLines = defaultEngineLogTailLines
	}
	window, err := readLogTailWindow(logPath)
	if err != nil || window == "" {
		return "", err
	}
	normalized := strings.ReplaceAll(window, "\r\n", "\n")
	rawLines := strings.Split(normalized, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n"), nil
}

func openOSCommand(goos string) (string, error) {
	switch goos {
	case "darwin":
		return "open", nil
	case "windows":
		return "explorer", nil
	case "linux":
		return "xdg-open", nil
	default:
		return "", fmt.Errorf("unsupported platform: %s", goos)
	}
}

// OpenPathInOS opens an existing path using the host operating system's default handler.
func OpenPathInOS(path string) error {
	if strings.TrimSpace(path) == "" || strings.ContainsRune(path, 0) {
		return errors.New("invalid path")
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	cmd, err := openOSCommand(runtime.GOOS)
	if err != nil {
		return err
	}
	return openPathFunc(cmd, path)
}
