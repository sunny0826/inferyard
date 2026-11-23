package inferyard

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
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

const (
	ExitComplete   = 0
	ExitBlocked    = 2
	ExitIncomplete = 3
	ExitToolError  = 4
	ExitCancelled  = 130
)

func MapExitCode(code int) ExitStatus {
	switch code {
	case ExitComplete:
		return Complete
	case ExitBlocked:
		return Blocked
	case ExitIncomplete:
		return Incomplete
	case ExitToolError:
		return Error
	case ExitCancelled:
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
	Path        string
	Dir         string
	GracePeriod time.Duration
}
type Execution struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Status   ExitStatus
}

func (c Client) Exec(ctx context.Context, args ...string) (Execution, error) {
	return c.run(ctx, nil, args...)
}

func (c Client) ExecStream(ctx context.Context, onStderrLine func(string), args ...string) (Execution, error) {
	return c.run(ctx, onStderrLine, args...)
}

func (c Client) run(ctx context.Context, onStderrLine func(string), args ...string) (Execution, error) {
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
	if extraPaths := PrependedPaths(ctx); len(extraPaths) > 0 {
		cmd.Env = MergePrependedPath(os.Environ(), extraPaths)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	var pw *io.PipeWriter
	var scanDone chan struct{}
	if onStderrLine != nil {
		var pr *io.PipeReader
		pr, pw = io.Pipe()
		cmd.Stderr = pw
		scanDone = make(chan struct{})
		go func() {
			defer close(scanDone)
			tee := io.TeeReader(pr, &stderr)
			scanner := bufio.NewScanner(tee)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				onStderrLine(scanner.Text())
			}
			_, _ = io.Copy(io.Discard, tee)
			_ = pr.Close()
		}()
	} else {
		cmd.Stderr = &stderr
	}
	if err := cmd.Start(); err != nil {
		if pw != nil {
			_ = pw.Close()
			<-scanDone
		}
		return out, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr, cancelErr error
	select {
	case waitErr = <-done:
	case <-ctx.Done():
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
	if pw != nil {
		_ = pw.Close()
		<-scanDone
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
