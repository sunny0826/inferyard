package pages

import (
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type previewMode interface {
	showNotice()
	dispatch(previewIntent)
	resetTask(model, engine string)
	report(reference string) (*core.ReportIndex, *core.ApiError)
	detail(id string) *core.RunTaskDetail
	resolveModelURL(raw string) inferyard.ModelResolve
	detectedEngines() []inferyard.DetectedEngine
	downloadLogTail(taskID string) string
}

func (s *viewState) simulate(intent previewIntent) bool {
	if s.preview == nil {
		return false
	}
	s.preview.dispatch(intent)
	return true
}

func (s *viewState) previewAction() bool {
	if s.preview == nil {
		return false
	}
	s.preview.showNotice()
	return true
}
