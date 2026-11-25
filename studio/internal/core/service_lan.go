package core

import (
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// localHealthEndpoint converts wildcard bind addresses (0.0.0.0 / ::) to 127.0.0.1
// so local health checks, unload calls, and endpoint tests always connect via loopback.
func localHealthEndpoint(endpoint string) string {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	host := u.Hostname()
	if host != "0.0.0.0" && host != "::" {
		return trimmed
	}
	port := u.Port()
	if port != "" {
		u.Host = net.JoinHostPort("127.0.0.1", port)
	} else {
		u.Host = "127.0.0.1"
	}
	return u.String()
}

// applyLANAccess updates startup listen arguments, endpoint, and (for PolyStrata)
// writes a non-destructive config copy when LANAccess is enabled.
func applyLANAccess(startup Startup, serveDir string, lanAccess bool) (Startup, *ApiError) {
	startup.LANAccess = lanAccess
	targetHost := "127.0.0.1"
	if lanAccess {
		targetHost = "0.0.0.0"
	}

	port := portFromEndpoint(startup.Endpoint)
	if port <= 0 {
		_, argPort := startupAddress(startup.Args)
		port = argPort
	}
	if port > 0 {
		startup.Endpoint = "http://" + net.JoinHostPort(targetHost, strconv.Itoa(port))
	}

	if !lanAccess {
		return startup, nil
	}

	if startup.Family == "polystrata" {
		if cfgPath, idx, inline := findConfigArg(startup.Args); cfgPath != "" {
			lanCfgPath, err := writePolystrataLANConfig(cfgPath, serveDir, targetHost)
			if err != nil {
				return Startup{}, Err("internal_error")
			}
			args := append([]string(nil), startup.Args...)
			if inline {
				args[idx] = "--config=" + lanCfgPath
			} else if idx+1 < len(args) {
				args[idx+1] = lanCfgPath
			}
			startup.Args = args
		}
		if hasHostArg(startup.Args) {
			startup.Args = replaceOrAppendFlag(startup.Args, []string{"--host"}, "--host", targetHost)
		}
		return startup, nil
	}

	startup.Args = replaceOrAppendFlag(startup.Args, []string{"--host"}, "--host", targetHost)
	return startup, nil
}

func hasHostArg(args []string) bool {
	for _, arg := range args {
		key, _, _ := strings.Cut(arg, "=")
		if key == "--host" {
			return true
		}
	}
	return false
}

func findConfigArg(args []string) (path string, idx int, inline bool) {
	for i := 0; i < len(args); i++ {
		key, value, isInline := strings.Cut(args[i], "=")
		if key != "--config" {
			continue
		}
		if isInline {
			return strings.TrimSpace(value), i, true
		}
		if i+1 < len(args) {
			return strings.TrimSpace(args[i+1]), i, false
		}
	}
	return "", -1, false
}

func writePolystrataLANConfig(srcPath, serveDir, targetHost string) (string, error) {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return "", err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", err
	}
	doc["host"] = targetHost
	if server, ok := doc["server"].(map[string]any); ok {
		server["host"] = targetHost
	}
	outDir := serveDir
	if outDir == "" {
		outDir = filepath.Dir(srcPath)
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return "", err
	}
	dstPath := filepath.Join(outDir, "polystrata-lan.json")
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(outDir, "polystrata-lan-*.json")
	if err != nil {
		return "", err
	}
	tmpName := f.Name()
	defer os.Remove(tmpName)
	if _, err := f.Write(encoded); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, dstPath); err != nil {
		return "", err
	}
	return dstPath, nil
}
