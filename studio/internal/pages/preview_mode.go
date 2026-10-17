package pages

import "inferyard/studio/internal/core"

type previewMode interface {
	showNotice()
	resetTask(model, engine string)
	report(reference string) (*core.ReportIndex, *core.ApiError)
	detail(id string) *core.RunTaskDetail
}

func (s *viewState) previewAction() bool {
	if s.preview == nil {
		return false
	}
	s.preview.showNotice()
	return true
}
