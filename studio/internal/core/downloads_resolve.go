package core

import (
	"context"
	"inferyard/studio/internal/inferyard"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var resolvableRepoPart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (s *Service) downloadProxy() string { return str(s.userSettings().DownloadProxy) }

func ValidateDownloadProxy(raw string) bool {
	if raw == "" {
		return true
	}
	for _, r := range raw {
		if r < 33 || r >= 127 {
			return false
		}
	}
	if strings.ContainsAny(raw, "?#") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	if u.User != nil || strings.Contains(u.Host, "@") || strings.HasSuffix(u.Host, ":") {
		return false
	}
	if u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return false
	}
	if port := u.Port(); port != "" {
		for _, r := range port {
			if r < '0' || r > '9' {
				return false
			}
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return true
}

func IsResolvableModelURL(raw string) bool {
	if raw == "" || strings.Contains(raw, "#") {
		return false
	}
	for _, r := range raw {
		if r < 33 || r >= 127 {
			return false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || strings.Contains(u.Host, "@") || u.Fragment != "" {
		return false
	}
	host := strings.ToLower(u.Host)
	if host != "huggingface.co" && host != "modelscope.cn" {
		return false
	}
	if strings.Contains(raw, "?") && u.RawQuery != "download=true" && u.RawQuery != "download=1" {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	if len(parts) < 4 {
		return false
	}
	for i, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err != nil {
			return false
		}
		parts[i] = decoded
		if decoded == "" || decoded == "." || decoded == ".." || strings.TrimSpace(decoded) != decoded {
			return false
		}
		for _, r := range decoded {
			if r < 32 || r >= 127 || strings.ContainsRune("/\\:", r) {
				return false
			}
		}
	}
	owner, repo, mode, ref, fileParts, ok := extractResolvableParts(host, parts)
	if !ok || !resolvableRepoPart.MatchString(owner) || !resolvableRepoPart.MatchString(repo) {
		return false
	}
	if ref == "" || strings.Contains(ref, "..") {
		return false
	}
	for _, r := range ref {
		if r < 33 || r >= 127 {
			return false
		}
	}
	if mode == "tree" {
		if len(fileParts) > 0 && strings.HasSuffix(strings.ToLower(fileParts[len(fileParts)-1]), ".gguf") {
			return false
		}
		return true
	}
	if len(fileParts) == 0 {
		return false
	}
	last := fileParts[len(fileParts)-1]
	return strings.HasSuffix(last, ".gguf") && len(last) > len(".gguf")
}

func extractResolvableParts(host string, parts []string) (owner, repo, mode, ref string, fileParts []string, ok bool) {
	if host == "modelscope.cn" && len(parts) > 0 && parts[0] == "models" {
		if len(parts) >= 5 && (parts[3] == "blob" || parts[3] == "resolve" || parts[3] == "tree") {
			return parts[1], parts[2], parts[3], parts[4], parts[5:], true
		}
		if len(parts) >= 7 && parts[3] == "file" && parts[4] == "view" {
			return parts[1], parts[2], "blob", parts[5], parts[6:], true
		}
		return "", "", "", "", nil, false
	}
	if len(parts) >= 4 && parts[2] == "tree" {
		return parts[0], parts[1], "tree", parts[3], parts[4:], true
	}
	if len(parts) < 5 || (parts[2] != "blob" && parts[2] != "resolve") {
		return "", "", "", "", nil, false
	}
	return parts[0], parts[1], parts[2], parts[3], parts[4:], true
}

func IsSupportedModelURL(raw string) bool {
	return validSource(raw) || validDirectorySource(raw) || IsResolvableModelURL(raw)
}

func (s *Service) ResolveModelURL(ctx context.Context, rawURL, tokenEnv string) (inferyard.ModelResolve, *ApiError) {
	if !IsResolvableModelURL(rawURL) || (tokenEnv != "" && !envName.MatchString(tokenEnv)) {
		return inferyard.ModelResolve{}, Err("invalid_request")
	}
	u := s.userSettings()
	p := s.probe(u)
	if p.Status != "ok" {
		return inferyard.ModelResolve{}, cliProbeError(p)
	}
	proxy := s.downloadProxy()
	if proxy != "" && !ValidateDownloadProxy(proxy) {
		return inferyard.ModelResolve{}, Err("invalid_request")
	}
	args := []string{"model", "resolve", "--source", rawURL}
	if strings.Contains(rawURL, "/tree/") {
		args = append(args, "--kind", "directory")
	}
	if tokenEnv != "" {
		args = append(args, "--token-env", tokenEnv)
	}
	if proxy != "" {
		args = append(args, "--proxy-origin", proxy)
	}
	out, execErr := s.exec(ctx, *p.Path, args...)
	if execErr != nil {
		return inferyard.ModelResolve{}, Err("internal_error")
	}
	if out.ExitCode != inferyard.ExitComplete {
		apiErr := exitError(out, nil)
		if apiErr == nil {
			apiErr = Err("cli_error")
		}
		return inferyard.ModelResolve{}, apiErr
	}
	resolved, parseErr := inferyard.ParseModelResolve(out.Stdout)
	if parseErr != nil {
		return inferyard.ModelResolve{}, Err("internal_error")
	}
	return resolved, nil
}

func (s *Service) CreateOrResolveModelDownload(ctx context.Context, req CreateDownloadRequest, tokenEnv string) (DownloadTask, *ApiError) {
	if validSource(req.SourceUrl) || validDirectorySource(req.SourceUrl) {
		return s.CreateModelDownload(req, tokenEnv)
	}
	if (req.OutName != "" && !ValidDownloadName(req.OutName)) || !IsResolvableModelURL(req.SourceUrl) || (tokenEnv != "" && !envName.MatchString(tokenEnv)) {
		return DownloadTask{}, Err("invalid_request")
	}
	u := s.userSettings()
	root, apiErr := s.downloadRoot("model", u)
	if apiErr != nil {
		return DownloadTask{}, apiErr
	}
	if req.OutName == "" {
		s.mu.Lock()
		existing, found := s.activeDownloadLocked("model", root, req.SourceUrl, "")
		s.mu.Unlock()
		if found {
			return existing, nil
		}
	}
	resolved, apiErr := s.ResolveModelURL(ctx, req.SourceUrl, tokenEnv)
	if apiErr != nil {
		return DownloadTask{}, apiErr
	}
	pinnedReq := req
	pinnedReq.SourceUrl = resolved.PinnedURL
	return s.createModelDownload(pinnedReq, req.SourceUrl, tokenEnv)
}
