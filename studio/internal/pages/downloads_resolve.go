package pages

import (
	"context"
	"net/url"
	"path"
	"strings"
	"sync"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type modelResolveOutcome struct {
	source   string
	resolved *inferyard.ModelResolve
	errText  string
}

type modelResolveMailbox struct {
	mu      sync.Mutex
	pending *modelResolveOutcome
}

type modelResolveState struct {
	source    string
	resolving bool
	resolved  *inferyard.ModelResolve
	errText   string
	mailbox   *modelResolveMailbox
}

func customModelURLError(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if err := downloadURLError(trimmed); err != "" {
		return err
	}
	if !core.IsResolvableModelURL(trimmed) {
		return "请输入 HuggingFace 或 ModelScope 的 .gguf 或 /tree 目录地址（支持 /blob、/resolve、/tree）。"
	}
	return ""
}

func (s *viewState) acquireStarterBundle(engineURL, modelURL string) {
	if s.preview != nil {
		s.simulate(previewDownload{kind: "engine", source: engineURL})
		s.simulate(previewDownload{kind: "model", source: modelURL})
		return
	}
	s.work(func() (string, *core.ApiError) {
		if _, e := s.service.CreateEngineDownload(engineURL, ""); e != nil {
			return "", e
		}
		if _, e := s.service.CreateOrResolveModelDownload(s.ctx, core.CreateDownloadRequest{SourceUrl: modelURL}, s.tokenEnv); e != nil {
			return "", e
		}
		return "已将推荐引擎与模型加入下载队列", nil
	})
}

func (s *viewState) triggerModelResolve(st *modelResolveState, source string) {
	if s.preview != nil {
		res := previewResolveModelURL(source)
		st.resolving = false
		st.resolved = &res
		st.errText = ""
		return
	}
	if s.service == nil {
		st.errText = "后端服务尚未就绪，无法解析地址。"
		return
	}
	st.resolving = true
	st.resolved = nil
	st.errText = ""
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	tokenEnv := s.tokenEnv
	mailbox := st.mailbox
	go func() {
		res, apiErr := s.service.ResolveModelURL(ctx, source, tokenEnv)
		outcome := &modelResolveOutcome{source: source}
		if apiErr != nil {
			msg := apiErr.Message
			if msg == "" {
				msg = human(apiErr.Code)
			}
			if cause, ok := apiErr.Details["failure_cause"].(string); ok && cause != "" {
				msg += "：" + human(cause)
			}
			outcome.errText = msg
		} else {
			outcome.resolved = &res
		}
		mailbox.mu.Lock()
		mailbox.pending = outcome
		mailbox.mu.Unlock()
		if s.win != nil {
			s.win.Update(func() {})
		}
	}()
}

func previewResolveModelURL(raw string) inferyard.ModelResolve {
	u, _ := url.Parse(raw)
	platform := "huggingface"
	host := "huggingface.co"
	if u != nil && strings.ToLower(u.Host) == "modelscope.cn" {
		platform = "modelscope"
		host = "modelscope.cn"
	}
	filename := "model.gguf"
	if u != nil && u.Path != "" {
		if base := path.Base(u.Path); base != "" && base != "." && base != "/" {
			filename = base
		}
	}
	rev := "0123456789abcdef0123456789abcdef01234567"
	var size *int64
	alreadyPinned := false
	repo := "preview/model"
	if entry, ok := core.CatalogEntryByURL(raw); ok {
		if entry.Revision != "" {
			rev = entry.Revision
		}
		if entry.Bytes > 0 {
			b := entry.Bytes
			size = &b
		}
		alreadyPinned = raw == entry.URL
	}
	if u != nil {
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if host == "modelscope.cn" && len(parts) >= 4 && parts[0] == "models" {
			repo = parts[1] + "/" + parts[2]
		} else if len(parts) >= 2 {
			repo = parts[0] + "/" + parts[1]
		}
	}
	if strings.Contains(raw, "/tree/") {
		repoName := path.Base(strings.TrimSuffix(repo, "/"))
		pinned := "https://" + host + "/" + repo + "/tree/" + rev
		return inferyard.ModelResolve{
			Definition:    inferyard.ModelResolveDefinition,
			Kind:          "directory",
			Platform:      platform,
			Repository:    repo,
			Revision:      rev,
			PinnedURL:     pinned,
			DownloadURL:   pinned,
			Filename:      repoName,
			Bytes:         size,
			AlreadyPinned: alreadyPinned,
		}
	}
	pinned := "https://" + host + "/" + repo + "/blob/" + rev + "/" + filename
	download := "https://" + host + "/" + repo + "/resolve/" + rev + "/" + filename
	if platform == "modelscope" {
		download = "https://modelscope.cn/api/v1/models/" + repo + "/repo?Revision=" + rev + "&FilePath=" + url.QueryEscape(filename)
	}
	return inferyard.ModelResolve{
		Definition:    inferyard.ModelResolveDefinition,
		Platform:      platform,
		Repository:    repo,
		Revision:      rev,
		Path:          filename,
		PinnedURL:     pinned,
		DownloadURL:   download,
		Filename:      filename,
		Bytes:         size,
		AlreadyPinned: alreadyPinned,
	}
}
