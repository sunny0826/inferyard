package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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

// downloadOutputLocked reserves names through the task list, including failed
// and cancelled tasks whose downloader has not created a directory yet.
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
