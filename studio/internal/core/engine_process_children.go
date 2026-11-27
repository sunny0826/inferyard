package core

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var (
	engineProcessTableFunc    = engineProcessTable
	engineProcessParentReader = readEngineProcessParent
)

func readEngineProcessParent(pid int64) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.FormatInt(pid, 10), "-o", "ppid=")
	var output boundedProcessOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return 0, err
	}
	actual, err := strconv.ParseInt(strings.TrimSpace(output.String()), 10, 64)
	if err != nil || actual < 0 {
		return 0, os.ErrInvalid
	}
	return actual, nil
}

func (s *Service) confirmEngineParentMember(child, parent engineProcessIdentity) error {
	actualParent, err := engineProcessParentReader(child.PID)
	if err != nil {
		return err
	}
	if actualParent != parent.PID {
		return os.ErrInvalid
	}
	parentSame, err := s.sameEngineProcess(parent)
	if err != nil {
		return err
	}
	if !parentSame {
		return os.ErrInvalid
	}
	same, err := s.sameEngineProcess(child)
	if err != nil {
		return err
	}
	if !same {
		return os.ErrInvalid
	}
	return nil
}
