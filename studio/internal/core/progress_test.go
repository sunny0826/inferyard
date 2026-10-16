package core

import (
	"context"
	"inferyard/studio/internal/inferyard"
	"testing"
)

func progressService(t *testing.T, exitCode int, stdout string) *Service {
	t.Helper()
	return wizardService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if args[0] == "--version" {
			return inferyard.Execution{Stdout: []byte("inferyard fixture source=release")}, nil
		}
		return inferyard.Execution{ExitCode: exitCode, Stdout: []byte(stdout)}, nil
	})
}

func TestProgressDetectsOutdatedCLI(t *testing.T) {
	// Released 0.0.2 predates the progress command and rejects the arguments
	// with command "unknown"; it must surface as cli_outdated, not silence.
	s := progressService(t, 2, "invalid CLI arguments; use --help\n{\"command\": \"unknown\", \"status\": \"blocked\"}")
	_, e := s.Progress(context.Background())
	if e == nil || e.Code != "cli_outdated" {
		t.Fatalf("err = %+v", e)
	}
}

func TestProgressAcceptsCapableCLIBlocked(t *testing.T) {
	s := progressService(t, 2, "{\"command\": \"progress\", \"status\": \"blocked\"}")
	_, e := s.Progress(context.Background())
	if e != nil {
		t.Fatalf("err = %+v", e)
	}
}

func TestProgressCLIError(t *testing.T) {
	s := progressService(t, 4, "{\"command\": \"progress\", \"status\": \"error\"}")
	_, e := s.Progress(context.Background())
	if e == nil || e.Code != "cli_error" {
		t.Fatalf("err = %+v", e)
	}
}
