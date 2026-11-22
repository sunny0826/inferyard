package pages

import (
	"context"
	"strings"
	"sync"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type modelResolveOutcome struct {
	reqID    uint64
	source   string
	tokenEnv string
	resolved *inferyard.ModelResolve
	errText  string
}

type modelResolveMailbox struct {
	mu      sync.Mutex
	latest  uint64
	pending *modelResolveOutcome
}

type modelResolveState struct {
	reqID     uint64
	source    string
	tokenEnv  string
	resolving bool
	resolved  *inferyard.ModelResolve
	errText   string
	cancel    context.CancelFunc
	mailbox   *modelResolveMailbox
}

func (st *modelResolveState) invalidate(source, tokenEnv string) {
	if st.cancel != nil {
		st.cancel()
		st.cancel = nil
	}
	st.reqID++
	st.source = source
	st.tokenEnv = tokenEnv
	st.resolving = false
	st.resolved = nil
	st.errText = ""
	if st.mailbox != nil {
		st.mailbox.mu.Lock()
		st.mailbox.latest = st.reqID
		st.mailbox.pending = nil
		st.mailbox.mu.Unlock()
	}
}

func customModelURLError(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if err := downloadURLError(trimmed); err != "" {
		return err
	}
	if !core.IsSupportedModelURL(trimmed) {
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
		if _, e := s.service.CreateOrResolveModelDownload(s.ctx, core.CreateDownloadRequest{SourceUrl: modelURL}, ""); e != nil {
			return "", e
		}
		return "已将推荐引擎与模型加入下载队列", nil
	})
}

func (s *viewState) triggerModelResolve(st *modelResolveState, source, tokenEnv string) {
	if st.mailbox == nil {
		st.mailbox = &modelResolveMailbox{}
	}
	st.invalidate(source, tokenEnv)
	if s.preview != nil {
		res := s.preview.resolveModelURL(source)
		st.resolved = &res
		return
	}
	if s.service == nil {
		st.errText = "后端服务尚未就绪，无法解析地址。"
		return
	}
	parent := s.ctx
	if parent == nil {
		parent = context.Background()
	}
	reqCtx, cancel := context.WithCancel(parent)
	st.cancel = cancel
	st.resolving = true
	reqID := st.reqID
	mailbox := st.mailbox
	update := s.updateWindow
	if update == nil && s.win != nil {
		update = s.win.Update
	}
	go func() {
		defer cancel()
		res, apiErr := s.service.ResolveModelURL(reqCtx, source, tokenEnv)
		outcome := &modelResolveOutcome{reqID: reqID, source: source, tokenEnv: tokenEnv}
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
		stale := mailbox.latest != reqID
		if !stale {
			mailbox.pending = outcome
		}
		mailbox.mu.Unlock()
		if !stale && update != nil {
			update(func() {})
		}
	}()
}
