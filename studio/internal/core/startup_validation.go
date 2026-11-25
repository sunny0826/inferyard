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

func explicitPortArg(args []string) (port int, explicit bool, valid bool) {
	for i, arg := range args {
		key, value, inline := strings.Cut(arg, "=")
		if key != "--port" {
			continue
		}
		explicit = true
		if !inline {
			if i+1 >= len(args) {
				return 0, true, false
			}
			value = args[i+1]
		}
		p, err := strconv.Atoi(value)
		if err != nil || p <= 0 || p > 65535 {
			return 0, true, false
		}
		port = p
		valid = true
	}
	return port, explicit, valid
}

func validateEngineArgsCredentials(args []string) *ApiError {
	for _, arg := range args {
		option, value, _ := strings.Cut(strings.ToLower(strings.TrimSpace(arg)), "=")
		switch option {
		case "--api-key", "--api-key-file", "--password", "--token", "-hft", "--hf-token":
			return Err("engine_credentials_forbidden")
		}
		if strings.HasPrefix(option, "sk-") || strings.HasPrefix(value, "sk-") {
			return Err("engine_credentials_forbidden")
		}
	}
	return nil
}

func isAllowedBindHost(host string, lanAccess bool) bool {
	if host == "127.0.0.1" || host == "::1" || host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	if lanAccess && (host == "0.0.0.0" || host == "::") {
		return true
	}
	return false
}

func validateStartup(startup Startup) (string, int, *ApiError) {
	if startup.Kind == "lmstudio" {
		return "", 0, Err("local_engine_external_binding_required")
	}
	if apiErr := validateEngineArgsCredentials(startup.Args); apiErr != nil {
		return "", 0, apiErr
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
		if !isAllowedBindHost(host, startup.LANAccess) {
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
	if !isAllowedBindHost(host, startup.LANAccess) {
		return "", 0, Err("engine_host_not_loopback")
	}
	for i, arg := range startup.Args {
		key, value, inline := strings.Cut(arg, "=")
		if key != "--host" {
			continue
		}
		if !inline && i+1 < len(startup.Args) {
			value = startup.Args[i+1]
		}
		if !isAllowedBindHost(value, startup.LANAccess) {
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
