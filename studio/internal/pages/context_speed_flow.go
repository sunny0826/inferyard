package pages

import (
	"context"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type speedBackend interface {
	StartContextSpeed(core.StartContextSpeedRequest) (core.RunTask, *core.ApiError)
	CancelRun(string) (core.RunTask, *core.ApiError)
	ReadContextSpeed(context.Context, string) (inferyard.ContextSpeedResult, *core.ApiError)
}

func startSpeedFlow(backend speedBackend, request core.StartContextSpeedRequest) (*speedTask, *core.ApiError) {
	task, e := backend.StartContextSpeed(request)
	if e != nil {
		return nil, e
	}
	return speedFromTask(task), nil
}

func readSpeedFlow(ctx context.Context, backend speedBackend, task *speedTask) (*speedTask, *core.ApiError) {
	next := *task
	next.samples, next.summary, next.result = nil, nil, nil
	result, e := backend.ReadContextSpeed(ctx, task.id)
	if e == nil {
		applySpeedResult(&next, result)
	}
	return &next, e
}

func cancelSpeedFlow(backend speedBackend, id string) *core.ApiError {
	_, e := backend.CancelRun(id)
	return e
}
