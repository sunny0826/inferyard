package core

import (
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func startupAddress(args []string) (string, int) {
	host, port := "", 0
	for i, arg := range args {
		key, value, inline := strings.Cut(arg, "=")
		if !inline && i+1 < len(args) {
			value = args[i+1]
		}
		switch key {
		case "--host":
			host = value
		case "--port":
			port, _ = strconv.Atoi(value)
		}
	}
	return host, port
}

func validateStartup(startup Startup) (string, int, *ApiError) {
	if startup.Kind == "lmstudio" {
		return "", 0, Err("local_engine_external_binding_required")
	}
	for _, arg := range startup.Args {
		option, value, _ := strings.Cut(strings.ToLower(arg), "=")
		switch option {
		case "--api-key", "--api-key-file", "--password", "--token", "-hft", "--hf-token":
			return "", 0, Err("engine_credentials_forbidden")
		}
		if strings.HasPrefix(option, "sk-") || strings.HasPrefix(value, "sk-") {
			return "", 0, Err("engine_credentials_forbidden")
		}
	}
	host, port := startupAddress(startup.Args)
	if startup.Local {
		if startup.Kind != "direct_process" {
			return "", 0, Err("local_engine_external_binding_required")
		}
		u, err := url.Parse(startup.Endpoint)
		if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return "", 0, Err("engine_host_not_loopback")
		}
		host = u.Hostname()
		port, _ = strconv.Atoi(u.Port())
		if host != "localhost" && !net.ParseIP(host).IsLoopback() {
			return "", 0, Err("engine_host_not_loopback")
		}
		st, err := os.Stat(startup.WorkingDirectory)
		if !filepath.IsAbs(startup.WorkingDirectory) || err != nil || !st.IsDir() {
			return "", 0, Err("engine_working_directory_invalid")
		}
		for key, value := range startup.Environment {
			if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, 0) || (key == "HOME" && value != os.Getenv("HOME")) {
				return "", 0, Err("engine_environment_invalid")
			}
		}
	}
	if host != "127.0.0.1" && host != "::1" && host != "localhost" {
		return "", 0, Err("engine_host_not_loopback")
	}
	// Reject every host occurrence, including a conflicting earlier argument.
	for i, arg := range startup.Args {
		key, value, inline := strings.Cut(arg, "=")
		if key != "--host" {
			continue
		}
		if !inline && i+1 < len(startup.Args) {
			value = startup.Args[i+1]
		}
		if value != "127.0.0.1" && value != "::1" && value != "localhost" {
			return "", 0, Err("engine_host_not_loopback")
		}
	}
	if port <= 0 || port > 65535 {
		return "", 0, Err("invalid_request")
	}
	st, err := os.Stat(startup.Path)
	if err != nil || !st.Mode().IsRegular() || !filepath.IsAbs(startup.Path) {
		return "", 0, Err("engine_not_executable")
	}
	if _, err := exec.LookPath(startup.Path); err != nil {
		return "", 0, Err("engine_not_executable")
	}
	return host, port, nil
}
