package core

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var shortCommitSuffix = regexp.MustCompile(`-[0-9a-f]{7,40}$`)

func SuggestDownloadSlug(kind, sourceURL string) string {
	fallback := "model"
	if kind == "engine" {
		fallback = "engine"
	}
	u, err := url.Parse(strings.TrimSpace(sourceURL))
	if err != nil || u.Path == "" {
		return fallback
	}
	base := path.Base(u.Path)
	if decoded, err := url.PathUnescape(base); err == nil && decoded != "" {
		base = decoded
	}
	if base == "" || base == "." || base == "/" {
		return fallback
	}
	lower := strings.ToLower(base)
	if kind == "model" {
		if strings.HasSuffix(lower, ".gguf") {
			base = base[:len(base)-5]
		}
	} else {
		for _, ext := range []string{".tar.gz", ".tgz", ".zip", ".exe"} {
			if strings.HasSuffix(lower, ext) {
				base = base[:len(base)-len(ext)]
				break
			}
		}
		if idx := strings.Index(strings.ToLower(base), "-bin-"); idx > 0 {
			base = base[:idx]
			base = shortCommitSuffix.ReplaceAllString(base, "")
		}
	}
	var b strings.Builder
	prevDash := false
	for _, r := range base {
		valid := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if valid {
			b.WriteRune(r)
			prevDash = r == '-'
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	slug := strings.Trim(b.String(), ".-")
	if len(slug) > 64 {
		slug = strings.TrimRight(slug[:64], ".-")
	}
	if !ValidDownloadName(slug) {
		return fallback
	}
	return slug
}

func ResolveDownloadDisplayName(kind, sourceURL string) string {
	if entry, ok := CatalogEntryByURL(sourceURL); ok {
		return entry.DisplayName()
	}
	if u, err := url.Parse(strings.TrimSpace(sourceURL)); err == nil && u.Path != "" {
		base := path.Base(u.Path)
		if decoded, err := url.PathUnescape(base); err == nil && decoded != "" {
			base = decoded
		}
		if base != "" && base != "." && base != "/" {
			return base
		}
	}
	if kind == "engine" {
		return "引擎"
	}
	return "模型"
}

func resolveAutoDownloadName(kind, configuredDefault, sourceURL string) string {
	if kind == "model" && configuredDefault == "model" {
		return SuggestDownloadSlug("model", sourceURL)
	}
	if kind == "engine" && configuredDefault == "engine" {
		return SuggestDownloadSlug("engine", sourceURL)
	}
	return configuredDefault
}

func ValidDownloadName(name string) bool {
	return outputName.MatchString(name) && strings.Trim(name, ".") != ""
}

func downloadNameDefaults(u UserSettings) UserSettings {
	if !ValidDownloadName(u.ModelDownloadName) {
		u.ModelDownloadName = "model"
	}
	if !ValidDownloadName(u.EngineDownloadName) {
		u.EngineDownloadName = "engine"
	}
	return u
}

func (s *Service) downloadOutputLocked(root, explicit, base string) (string, string, *ApiError) {
	auto := explicit == ""
	if !auto {
		base = explicit
	}
	if !ValidDownloadName(base) {
		return "", "", Err("invalid_request")
	}
	for n := 1; n <= 1000; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		path := filepath.Join(root, name)
		_, err := os.Lstat(path)
		occupied := err == nil
		if err != nil && !os.IsNotExist(err) {
			return "", "", Err("internal_error")
		}
		if !occupied {
			resolved, err := Resolve(root, name)
			if err != nil {
				return "", "", Err("invalid_request")
			}
			path = resolved
			for _, d := range s.downloads {
				if d.Task.OutDir == path {
					occupied = true
					break
				}
			}
		}
		if !occupied {
			return name, path, nil
		}
		if !auto {
			if _, err := Resolve(root, name); err != nil {
				return "", "", Err("invalid_request")
			}
			return "", "", Conflict("output_exists")
		}
	}
	return "", "", Err("output_name_exhausted")
}
