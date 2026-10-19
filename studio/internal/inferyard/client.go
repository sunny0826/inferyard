// Package inferyard implements the CLI boundary without interpreting benchmark results.
package inferyard

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

type ExitStatus string

const (
	Complete   ExitStatus = "complete"
	Blocked    ExitStatus = "blocked"
	Incomplete ExitStatus = "incomplete"
	Error      ExitStatus = "error"
	Cancelled  ExitStatus = "cancelled"
	Unknown    ExitStatus = "unknown"
)

func MapExitCode(code int) ExitStatus {
	switch code {
	case 0:
		return Complete
	case 2:
		return Blocked
	case 3:
		return Incomplete
	case 4:
		return Error
	case 130:
		return Cancelled
	default:
		return Unknown
	}
}

func ParseVersion(data []byte) (string, string, error) {
	fields := strings.Fields(string(data))
	if len(fields) < 2 || len(fields) > 3 || fields[0] != "inferyard" {
		return "", "", errors.New("invalid inferyard version output")
	}
	if len(fields) == 2 {
		return fields[1], "", nil
	}
	if !strings.HasPrefix(fields[2], "source=") {
		return "", "", errors.New("invalid inferyard version output")
	}
	source := strings.TrimPrefix(fields[2], "source=")
	if source != "checkout" && source != "release" {
		return "", "", errors.New("invalid inferyard version output")
	}
	return fields[1], source, nil
}

type Client struct {
	Path string
	Dir  string
	// GracePeriod defaults to two seconds. No shell is used to construct commands.
	GracePeriod time.Duration
}
type Execution struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int // Actual process exit code; -1 for a signal or failure to start.
	Status   ExitStatus
}

// Exec returns nonzero CLI exits as data. Errors indicate launch, I/O, or context failures.
// Captured output remains available even when cancellation prevents a final JSON result.
func (c Client) Exec(ctx context.Context, args ...string) (Execution, error) {
	out := Execution{ExitCode: -1, Status: Unknown}
	if err := ctx.Err(); err != nil {
		out.Status = Cancelled
		return out, err
	}
	path := c.Path
	if path == "" {
		path = "inferyard"
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = c.Dir
	prepareProcess(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return out, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr, cancelErr error
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		// Prefer an already completed process over a concurrent cancellation.
		select {
		case waitErr = <-done:
		default:
			cancelErr = ctx.Err()
			interruptProcess(cmd)
			grace := c.GracePeriod
			if requested, ok := ctx.Value(drainGraceKey{}).(time.Duration); ok && requested > grace {
				grace = requested
			}
			if grace <= 0 {
				grace = 2 * time.Second
			}
			timer := time.NewTimer(grace)
			select {
			case waitErr = <-done:
			case <-timer.C:
				killProcess(cmd)
				waitErr = <-done
			}
			timer.Stop()
		}
	}
	out.Stdout, out.Stderr = stdout.Bytes(), stderr.Bytes()
	out.ExitCode = cmd.ProcessState.ExitCode()
	out.Status = MapExitCode(out.ExitCode)
	if cancelErr != nil {
		out.Status = Cancelled
		return out, cancelErr
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return out, nil
	}
	return out, waitErr
}
