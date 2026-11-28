package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func isFailedQuality(quality *string) bool {
	q := emptyText(quality)
	return q == "fail" || q == "failed"
}

func detailCaseItem(c *ui.Context, item core.CaseProgress) caseDetailItem {
	qText := caseQuality(item)
	qKind := stateNeutral
	hasQBadge := false
	switch emptyText(item.QualityState) {
	case "pass", "passed":
		qKind, hasQBadge = stateSuccess, true
	case "fail", "failed":
		qKind, hasQBadge = stateDanger, true
	case "unscorable":
		qKind, hasQBadge = stateWarning, true
	}
	errCat := ""
	if item.ErrorCategory != nil && *item.ErrorCategory != "" {
		errCat = *item.ErrorCategory
	}
	cat := ""
	if item.Category != nil && *item.Category != "" {
		cat = *item.Category
	}
	dur := ""
	if item.DurationMs != nil {
		dur = formatDurationMs(item.DurationMs)
	}
	return caseDetailItem{
		Key:             "detail-" + item.CaseId,
		CaseID:          item.CaseId,
		Category:        cat,
		StateLabel:      stateLabel(item.State),
		StateKind:       caseStateKind(item.State),
		QualityText:     qText,
		QualityKind:     qKind,
		HasQualityBadge: hasQBadge,
		DurationText:    dur,
		ErrorCategory:   errCat,
		Prompt:          emptyText(item.Prompt),
		Response:        emptyText(item.Response),
		ExpectedAnswer:  emptyText(item.ReferenceAnswer),
		ShowExpected:    isFailedQuality(item.QualityState),
	}
}

func reportCaseItem(c *ui.Context, runID string, idx int, req core.ReportRequest) caseDetailItem {
	qText := reportQualityFor(c, req.Quality)
	qKind, hasQBadge := reportQualityBadgeKind(req.Quality)
	dur := ""
	if req.LatencyMS != nil {
		dur = reportScaled(req.LatencyMS, 1000, "s")
	}
	return caseDetailItem{
		Key:             fmt.Sprintf("report-%s-%d-%s", runID, idx, req.CaseID),
		CaseID:          reportText(req.CaseID),
		Category:        reportText(req.CategoryLabel),
		StateLabel:      reportText(stateLabel(req.State)),
		StateKind:       caseStateKind(req.State),
		QualityText:     qText,
		QualityKind:     qKind,
		HasQualityBadge: hasQBadge,
		DurationText:    dur,
		Prompt:          emptyText(req.Prompt),
		Response:        emptyText(req.Response),
		ExpectedAnswer:  emptyText(req.ReferenceAnswer),
		ShowExpected:    isFailedQuality(req.Quality),
		IsReport:        true,
	}
}

func (s *viewState) detailCaseDrawer(c *ui.Context, d *core.RunTaskDetail) {
	open := s.detailCaseID != "" && d != nil && d.Progress != nil
	var item caseDetailItem
	if open {
		cases := d.Progress.Cases
		idx := -1
		for i := range cases {
			if cases[i].CaseId == s.detailCaseID {
				idx = i
				break
			}
		}
		if idx < 0 {
			open = false
			s.detailCaseID = ""
		} else {
			item = detailCaseItem(c, cases[idx])
			item.Index = idx
			item.Total = len(cases)
			const pageSize = 50
			if idx > 0 {
				prevIdx := idx - 1
				item.OnPrev = func() {
					s.detailCaseID = cases[prevIdx].CaseId
					s.casesTablePage = prevIdx / pageSize
				}
			}
			if idx+1 < len(cases) {
				nextIdx := idx + 1
				item.OnNext = func() {
					s.detailCaseID = cases[nextIdx].CaseId
					s.casesTablePage = nextIdx / pageSize
				}
			}
		}
	}
	s.caseDetailDrawer(c, "detail-case-drawer", &open, item)
	if !open {
		s.detailCaseID = ""
	}
}

func (s *viewState) reportCaseDrawer(c *ui.Context, report *reportViewState, run core.ReportRun) {
	open := report != nil && report.detailCaseOpen && report.detailCaseIndex >= 0 && report.detailCaseIndex < len(run.Requests)
	var item caseDetailItem
	if open {
		idx := report.detailCaseIndex
		item = reportCaseItem(c, run.Source.RunID, idx, run.Requests[idx])
		item.Index = idx
		item.Total = len(run.Requests)
		if idx > 0 {
			prevIdx := idx - 1
			item.OnPrev = func() {
				report.detailCaseIndex = prevIdx
				report.requestsPage = prevIdx / reportRequestPageSize
			}
		}
		if idx+1 < len(run.Requests) {
			nextIdx := idx + 1
			item.OnNext = func() {
				report.detailCaseIndex = nextIdx
				report.requestsPage = nextIdx / reportRequestPageSize
			}
		}
	}
	s.caseDetailDrawer(c, "report-case-drawer", &open, item)
	if !open && report != nil {
		report.detailCaseOpen = false
		report.detailCaseIndex = -1
	}
}
