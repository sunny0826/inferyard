package pages

import (
	"inferyard/studio/internal/core"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

func pathError(value string) string {
	if value != "" && (!filepath.IsAbs(value) || strings.ContainsRune(value, 0)) {
		return "请输入有效的绝对路径。"
	}
	return ""
}

func urlError(value string) string {
	if value == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "请输入完整的 http 或 https URL。"
	}
	return ""
}

func tokenNameError(value string) string {
	if value != "" && !environmentName.MatchString(value) {
		return "只接受环境变量名，请勿输入明文 token。"
	}
	return ""
}

func resumeErrors(endpoint, pid string) (string, string) {
	endpoint, pid = strings.TrimSpace(endpoint), strings.TrimSpace(pid)
	if (endpoint == "") != (pid == "") {
		return "endpoint 与 PID 必须同给同省。", "endpoint 与 PID 必须同给同省。"
	}
	pe := ""
	if pid != "" {
		n, err := strconv.ParseInt(pid, 10, 64)
		if err != nil || n <= 0 {
			pe = "PID 必须为正整数。"
		}
	}
	return urlError(endpoint), pe
}

func (s *viewState) validateSettings() bool {
	for _, value := range []string{s.cliPath, s.workspace, s.results, s.models} {
		if message := pathError(value); message != "" {
			s.submissionFailed(&core.ApiError{Code: "invalid_request", Message: message})
			return false
		}
	}
	for _, entry := range [][2]string{{s.workspace, emptyText(s.settings.Effective.WorkspaceRoot)}, {s.results, emptyText(s.settings.Settings.ResultsRoot)}, {s.models, emptyText(s.settings.Settings.ModelsRoot)}} {
		if entry[0] == "" && entry[1] != "" {
			s.submissionFailed(&core.ApiError{Code: "invalid_request", Message: "已配置的目录不能为空，请填写绝对路径。"})
			return false
		}
	}
	return true
}

func settingsPathError(value, previous string) string {
	if value == "" && previous != "" {
		return "已配置的目录不能为空。"
	}
	return pathError(value)
}
