package pages

import (
	"fmt"

	"inferyard/studio/internal/core"
)

// engineFailureKey identifies one failed engine state, so recovering from it
// does not also hide a later, different failure.
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

// engineFailureBlocks reports whether the current engine failure still explains
// the step. A successful connection during this task clears exactly that state.
func (s *viewState) engineFailureBlocks() bool {
	key := s.engineFailureKey()
	return key != "" && key != s.engineFailureCleared
}

// stepDiagnosis is the failure the active step owns. The most recent connection
// rejection wins, because it is the cause the user just triggered; then the task
// chain's failure; then a failed supervised engine. Only the configuration, bind
// and run steps own diagnostics, and the configuration step owns only this
// task's failure, so a previous round's engine error never reappears on a fresh
// task. The cause may be nil even when failed, for example a failed engine state
// without an error code.
func (s *viewState) stepDiagnosis() (cause *core.ApiError, failed bool) {
	step := s.step()
	if step < 2 || step > 4 {
		return nil, false
	}
	// The handoff card owns the connection error while it is on screen.
	if s.awaitingLocalService() {
		return nil, false
	}
	// A rejected manual, discovered or handoff connection is owned by the step
	// even though the page-level banner may carry the same error; unrelated page
	// operations keep their banner, so they are never mislabeled as a binding
	// failure.
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

// awaitingLocalService reports an LM Studio handoff card on screen.
func (s *viewState) awaitingLocalService() bool {
	for _, task := range s.tasks {
		if task.AwaitingService && !terminal(task.State) {
			return true
		}
	}
	return false
}

// stepFailed reports a blocked step even when no cause code was recorded, so the
// failure is never silently hidden.
func (s *viewState) stepFailed() bool {
	_, failed := s.stepDiagnosis()
	return failed
}

// activeTaskFailure is the cause the step should show, if one was recorded.
func (s *viewState) activeTaskFailure() *core.ApiError {
	cause, _ := s.stepDiagnosis()
	return cause
}

// stepOwnsError reports whether the visible step already renders e, so the page
// banner does not repeat the same diagnostic. Only an error the step actually
// displays counts: the handoff card renders its own message, everything else
// stays in the banner.
func (s *viewState) stepOwnsError(e *core.ApiError) bool {
	if e == nil || s.selected != "dashboard" || s.detailID != "" {
		return false
	}
	if s.awaitingLocalService() {
		return s.handoffFailure == e
	}
	cause, _ := s.stepDiagnosis()
	if cause == nil || cause.Code != e.Code {
		return false
	}
	// The task chain and the page may carry separate copies whose empty message
	// was normalized to a human string; the code alone already identifies the
	// same diagnostic.
	return cause.Message == e.Message || cause.Message == "" || e.Message == ""
}

// handoffOwnsError reports whether the handoff card already renders this task's
// rejection. The card is the place a handoff failure belongs; the task list must
// not repeat it. A task error with no recorded handoff failure stays in the list,
// because then the list is the only place that shows it.
func (s *viewState) handoffOwnsError(task core.RunTask) bool {
	return task.Error != nil && s.handoffFailure != nil && task.Error.Code == s.handoffFailure.Code
}

// stepOwnsTaskError reports whether the visible step already renders this task's
// error. An asynchronous CLI rejection arrives after the handoff card cleared its
// own copy, so the step diagnosis is the one that shows it; the task list must not
// add a second copy of the same diagnostic.
func (s *viewState) stepOwnsTaskError(task core.RunTask) bool {
	if task.Error == nil || s.selected != "dashboard" || s.detailID != "" {
		return false
	}
	if s.handoffOwnsError(task) {
		return true
	}
	cause, failed := s.stepDiagnosis()
	if !failed || cause == nil || cause.Code != task.Error.Code {
		return false
	}
	// A live run task keeps its own status line; only a terminal rejection is
	// already covered by the step.
	return terminal(task.State)
}
