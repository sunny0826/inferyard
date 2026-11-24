package core

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

func engineBinaryName() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}
	return "llama-server"
}

func validEngineSource(source string) bool {
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host == "" {
		return false
	}
	for _, c := range source {
		if c < 33 || c >= 127 {
			return false
		}
	}
	return !strings.Contains(u.Path, "..")
}

func isEngineArchive(base string) bool {
	return strings.HasSuffix(base, ".tar.gz") || strings.HasSuffix(base, ".tgz") || strings.HasSuffix(base, ".zip")
}

func EngineRoots(workspace string) []string {
	if workspace == "" {
		return []string{}
	}
	return []string{filepath.Join(workspace, "engines")}
}
