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
	Kind              string `json:"kind,omitempty"`
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

func validResolveSubpath(filePath string) bool {
	if filePath == "" {
		return true
	}
	if strings.HasPrefix(filePath, "/") || strings.HasSuffix(filePath, "/") || strings.Contains(filePath, "\\") {
		return false
	}
	for _, part := range strings.Split(filePath, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validResolvePath(filePath string) bool {
	if filePath == "" || !validResolveSubpath(filePath) || !strings.HasSuffix(filePath, ".gguf") {
		return false
	}
	parts := strings.Split(filePath, "/")
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
	if err != nil {
		return ModelResolve{}, invalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return ModelResolve{}, invalid
	}
	required := []string{
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
	}
	for _, k := range required {
		if _, ok := fields[k]; !ok {
			return ModelResolve{}, invalid
		}
	}
	for k := range fields {
		switch k {
		case "definition", "kind", "platform", "repository", "revision", "path", "pinned_url", "download_url", "filename", "bytes", "already_pinned", "model_requests_sent":
		default:
			return ModelResolve{}, invalid
		}
	}
	if !isStrictBoolJSON(fields["already_pinned"]) || bytes.Equal(bytes.TrimSpace(fields["model_requests_sent"]), []byte("null")) {
		return ModelResolve{}, invalid
	}
	var wire struct {
		Definition        string `json:"definition"`
		Kind              string `json:"kind"`
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
	if kindRaw, hasKind := fields["kind"]; hasKind && (bytes.Equal(bytes.TrimSpace(kindRaw), []byte("null")) || (wire.Kind != "gguf" && wire.Kind != "directory")) {
		return ModelResolve{}, invalid
	}
	if !validResolveRepo(wire.Repository) || !resolveHex40.MatchString(wire.Revision) ||
		!validResolveHost(wire.Platform, wire.PinnedURL) || !validResolveHost(wire.Platform, wire.DownloadURL) ||
		strings.Contains(wire.PinnedURL, "?") {
		return ModelResolve{}, invalid
	}
	if wire.Kind == "directory" {
		if !validResolveSubpath(wire.Path) || strings.TrimSpace(wire.Filename) == "" ||
			!strings.Contains(wire.PinnedURL, "/"+wire.Repository+"/tree/"+wire.Revision) {
			return ModelResolve{}, invalid
		}
	} else {
		if !validResolvePath(wire.Path) || wire.Filename != path.Base(wire.Path) ||
			!strings.Contains(wire.PinnedURL, "/"+wire.Repository+"/blob/"+wire.Revision+"/") {
			return ModelResolve{}, invalid
		}
	}
	if wire.Bytes != nil && *wire.Bytes <= 0 {
		return ModelResolve{}, invalid
	}
	return ModelResolve{
		Definition:        wire.Definition,
		Kind:              wire.Kind,
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
