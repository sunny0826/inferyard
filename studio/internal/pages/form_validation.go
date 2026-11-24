package pages

import (
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"inferyard/studio/internal/l10n"
)

func pathError(value string) string {
	if value != "" && (!filepath.IsAbs(value) || strings.ContainsRune(value, 0)) {
		return l10n.TR("common.validation.abs_path")
	}
	return ""
}

func urlError(value string) string {
	if value == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return l10n.TR("common.validation.http_url")
	}
	return ""
}

func tokenNameError(value string) string {
	if value != "" && !environmentName.MatchString(value) {
		return l10n.TR("common.validation.env_var_name")
	}
	return ""
}

func resumeErrors(endpoint, pid string) (string, string) {
	endpoint, pid = strings.TrimSpace(endpoint), strings.TrimSpace(pid)
	if (endpoint == "") != (pid == "") {
		msg := l10n.TR("common.validation.endpoint_pid_pair")
		return msg, msg
	}
	pe := ""
	if pid != "" {
		n, err := strconv.ParseInt(pid, 10, 64)
		if err != nil || n <= 0 {
			pe = l10n.TR("common.validation.pid_positive_int")
		}
	}
	return urlError(endpoint), pe
}

func (s *viewState) validateSettings() bool {
	for _, value := range []string{s.cliPath, s.workspace, s.results, s.models} {
		if pathError(value) != "" {
			return false
		}
	}
	for _, entry := range [][2]string{{s.workspace, emptyText(s.settings.Effective.WorkspaceRoot)}, {s.results, emptyText(s.settings.Settings.ResultsRoot)}, {s.models, emptyText(s.settings.Settings.ModelsRoot)}} {
		if entry[0] == "" && entry[1] != "" {
			return false
		}
	}
	return true
}

func settingsPathError(value, previous string) string {
	if value == "" && previous != "" {
		return l10n.TR("common.validation.configured_dir_required")
	}
	return pathError(value)
}
