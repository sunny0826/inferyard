package inferyard

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"
)

const ModelResolveDefinition = "model-resolve.v1"

var (
	resolveHex40    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	resolveRepoPart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

type ModelResolve struct {
	Definition        string `json:"definition"`
	Platform          string `json:"platform"`
	Repository        string `json:"repository"`
	Revision          string `json:"revision"`
	Path              string `json:"path"`
	PinnedURL         string `json:"pinned_url"`
	DownloadURL       string `json:"download_url"`
	Filename          string `json:"filename"`
	Bytes             *int64 `json:"bytes"`
	AlreadyPinned     bool   `json:"already_pinned"`
	ModelRequestsSent int    `json:"model_requests_sent"`
}

func validResolveRepo(repo string) bool {
	parts := strings.Split(repo, "/")
	return len(parts) == 2 && resolveRepoPart.MatchString(parts[0]) && resolveRepoPart.MatchString(parts[1])
}

func validResolvePath(filePath string) bool {
	if filePath == "" || strings.HasPrefix(filePath, "/") || strings.HasSuffix(filePath, "/") ||
		strings.Contains(filePath, "\\") || !strings.HasSuffix(filePath, ".gguf") {
		return false
	}
	parts := strings.Split(filePath, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return len(parts[len(parts)-1]) > len(".gguf")
}

func validResolveHost(platform, rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return false
	}
	host := strings.ToLower(u.Host)
	switch platform {
	case "huggingface":
		return host == "huggingface.co"
	case "modelscope":
		return host == "modelscope.cn"
	default:
		return false
	}
}

func ParseModelResolve(raw []byte) (ModelResolve, error) {
	invalid := errors.New("invalid model resolve result")
	data, err := unwrapStrictCommandDetails(raw, "model resolve", "resolved")
	if err != nil || !discoveryFields(
		data,
		"definition",
		"platform",
		"repository",
		"revision",
		"path",
		"pinned_url",
		"download_url",
		"filename",
		"bytes",
		"already_pinned",
		"model_requests_sent",
	) {
		return ModelResolve{}, invalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || !isStrictBoolJSON(fields["already_pinned"]) {
		return ModelResolve{}, invalid
	}
	if bytes.Equal(bytes.TrimSpace(fields["model_requests_sent"]), []byte("null")) {
		return ModelResolve{}, invalid
	}
	var wire struct {
		Definition        string `json:"definition"`
		Platform          string `json:"platform"`
		Repository        string `json:"repository"`
		Revision          string `json:"revision"`
		Path              string `json:"path"`
		PinnedURL         string `json:"pinned_url"`
		DownloadURL       string `json:"download_url"`
		Filename          string `json:"filename"`
		Bytes             *int64 `json:"bytes"`
		AlreadyPinned     bool   `json:"already_pinned"`
		ModelRequestsSent *int   `json:"model_requests_sent"`
	}
	if json.Unmarshal(data, &wire) != nil || wire.Definition != ModelResolveDefinition ||
		wire.ModelRequestsSent == nil || *wire.ModelRequestsSent != 0 {
		return ModelResolve{}, invalid
	}
	if !validResolveRepo(wire.Repository) || !resolveHex40.MatchString(wire.Revision) || !validResolvePath(wire.Path) {
		return ModelResolve{}, invalid
	}
	if wire.Filename != path.Base(wire.Path) || !validResolveHost(wire.Platform, wire.PinnedURL) || !validResolveHost(wire.Platform, wire.DownloadURL) {
		return ModelResolve{}, invalid
	}
	if !strings.Contains(wire.PinnedURL, "/"+wire.Repository+"/blob/"+wire.Revision+"/") || strings.Contains(wire.PinnedURL, "?") {
		return ModelResolve{}, invalid
	}
	if wire.Bytes != nil && *wire.Bytes <= 0 {
		return ModelResolve{}, invalid
	}
	return ModelResolve{
		Definition:        wire.Definition,
		Platform:          wire.Platform,
		Repository:        wire.Repository,
		Revision:          wire.Revision,
		Path:              wire.Path,
		PinnedURL:         wire.PinnedURL,
		DownloadURL:       wire.DownloadURL,
		Filename:          wire.Filename,
		Bytes:             wire.Bytes,
		AlreadyPinned:     wire.AlreadyPinned,
		ModelRequestsSent: *wire.ModelRequestsSent,
	}, nil
}
