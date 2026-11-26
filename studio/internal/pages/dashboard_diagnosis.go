package pages

import (
	"fmt"
	"time"

	"inferyard/studio/internal/core"
)

func (s *viewState) engineFailureKey() string {
	if s.engineState.State != "failed" {
		return ""
	}
	code := ""
	if s.engineState.Error != nil {
		code = s.engineState.Error.Code
	}
	return fmt.Sprintf("%s|%d|%s|%s", s.engineState.State, s.engineState.Pid, s.engineState.Endpoint, code)
}

func (s *viewState) engineFailureBlocks() bool {
	key := s.engineFailureKey()
	return key != "" && key != s.engineFailureCleared
}

func (s *viewState) stepDiagnosis() (cause *core.ApiError, failed bool) {
	step := s.step()
	if step < 2 || step > 4 {
		return nil, false
	}
	if s.awaitingLocalService() {
		return nil, false
	}
	if s.bindFailure != nil {
		return s.bindFailure, true
	}
	if s.task.failure != nil {
		return s.task.failure, true
	}
	if step < 3 {
		return nil, false
	}
	if s.engineFailureBlocks() {
		return s.engineState.Error, true
	}
	return nil, false
}

func (s *viewState) awaitingLocalService() bool {
	for _, task := range s.tasks {
		if task.AwaitingService && !terminal(task.State) {
			return true
		}
	}
	return false
}

func (s *viewState) stepFailed() bool {
	_, failed := s.stepDiagnosis()
	return failed
}

func (s *viewState) activeTaskFailure() *core.ApiError {
	cause, _ := s.stepDiagnosis()
	return cause
}

func (s *viewState) stepOwnsError(e *core.ApiError) bool {
	if e == nil || canonicalPageID(s.selected) != "benchmarks" || s.detailID != "" {
		return false
	}
	if s.awaitingLocalService() {
		return s.handoffFailure == e
	}
	cause, _ := s.stepDiagnosis()
	if cause == nil || cause.Code != e.Code {
		return false
	}
	return cause.Message == e.Message || cause.Message == "" || e.Message == ""
}

func (s *viewState) handoffOwnsError(task core.RunTask) bool {
	return task.Error != nil && s.handoffFailure != nil && task.Error.Code == s.handoffFailure.Code
}

func (s *viewState) stepOwnsTaskError(task core.RunTask) bool {
	if task.Error == nil || canonicalPageID(s.selected) != "benchmarks" || s.detailID != "" {
		return false
	}
	if s.handoffOwnsError(task) {
		return true
	}
	cause, failed := s.stepDiagnosis()
	if !failed || cause == nil || cause.Code != task.Error.Code {
		return false
	}
	return terminal(task.State)
}

func (s *viewState) dashboardTaskNotices() []core.RunTask {
	active := s.running() != nil || s.isPreparingBenchmark()
	var notices []core.RunTask
	for _, task := range s.tasks {
		if task.Kind == "context_speed" || emptyText(task.RunPath) != "" || (task.Queued && task.Bundle != nil && task.Error == nil) {
			continue
		}
		if task.State == "failed" || (terminal(task.State) && task.Error != nil) {
			if active || s.supersededTask(task) {
				continue
			}
		}
		if !s.stepOwnsTaskError(task) {
			notices = append(notices, task)
		}
	}
	return notices
}

func (s *viewState) supersededTask(task core.RunTask) bool {
	bundle := emptyText(task.Bundle)
	if !s.busy && s.task.stage != "" && bundle != "" {
		if current := s.currentRoundTask(bundle); current != nil && current.TaskId != task.TaskId {
			return true
		}
	}
	created, err := time.Parse(time.RFC3339Nano, task.CreatedAt)
	if bundle == "" || err != nil {
		return false
	}
	for _, other := range s.tasks {
		if other.Kind != "context_speed" && emptyText(other.Bundle) == bundle {
			stamp, err := time.Parse(time.RFC3339Nano, other.CreatedAt)
			if err == nil && stamp.After(created) {
				return true
			}
		}
	}
	return false
}
