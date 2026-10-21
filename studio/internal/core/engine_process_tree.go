package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type engineProcessIdentity struct {
	PID        int64 `json:"pid"`
	StartTicks int64 `json:"start_ticks"`
}

type engineProcessRow struct{ pid, parent, group int64 }

func configureEngineProcess(cmd *exec.Cmd, startup Startup) error {
	if !startup.Local {
		return nil
	}
	cmd.Dir = startup.WorkingDirectory
	environment := map[string]string{}
	for _, key := range []string{"HOME", "PATH", "TMPDIR", "LANG", "LC_ALL", "USER", "LOGNAME"} {
		if value, ok := os.LookupEnv(key); ok {
			environment[key] = value
		}
	}
	for key, value := range startup.Environment {
		environment[key] = value
	}
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	cmd.Env = make([]string, 0, len(keys))
	for _, key := range keys {
		cmd.Env = append(cmd.Env, key+"="+environment[key])
	}
	return isolateEngineProcess(cmd)
}

type boundedProcessOutput struct{ bytes.Buffer }

func (b *boundedProcessOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 4<<20 {
		return 0, os.ErrInvalid
	}
	return b.Buffer.Write(p)
}

// Read numerical process metadata only, with bounded time, bytes and tree size.
func engineProcessTable() ([]engineProcessRow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,ppid=,pgid=")
	var output boundedProcessOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	var rows []engineProcessRow
	for _, line := range strings.Split(output.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 3 {
			return nil, os.ErrInvalid
		}
		values := [3]int64{}
		for i, field := range fields {
			n, err := strconv.ParseInt(field, 10, 64)
			if err != nil || n < 0 {
				return nil, os.ErrInvalid
			}
			values[i] = n
		}
		rows = append(rows, engineProcessRow{values[0], values[1], values[2]})
	}
	return rows, nil
}

// Sandwich the group observation between identity reads so a PID recycled
// after the process-table snapshot cannot be adopted as an owned worker.
func (s *Service) confirmEngineGroupMember(p engineProcessIdentity, group int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.FormatInt(p.PID, 10), "-o", "pgid=")
	var output boundedProcessOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return err
	}
	actual, err := strconv.ParseInt(strings.TrimSpace(output.String()), 10, 64)
	if err != nil || actual != group {
		return os.ErrInvalid
	}
	same, err := s.sameEngineProcess(p)
	if err != nil {
		return err
	}
	if !same {
		return os.ErrInvalid
	}
	return nil
}

func (s *Service) sameEngineProcess(p engineProcessIdentity) (bool, error) {
	ticks, err := s.processTicks(p.PID)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || ticks <= 0 {
		if err == nil {
			err = os.ErrInvalid
		}
		return false, err
	}
	return ticks == p.StartTicks, nil
}

func ownerProcesses(owner engineOwner) []engineProcessIdentity {
	return append([]engineProcessIdentity{{owner.PID, owner.StartTicks}}, owner.Workers...)
}

// Adopt new group members only while the root or a recorded member still
// anchors its identity. An unanchored group may have been reused: keep the
// record and refuse signals rather than infer ownership from a group number.
func (s *Service) refreshEngineTree(owner *engineOwner, path string) error {
	if !owner.Local {
		return nil
	}
	if owner.StartTicks <= 0 {
		return os.ErrInvalid
	}
	rows, err := engineProcessTable()
	if err != nil {
		return err
	}
	rootTicks, rootErr := s.processTicks(owner.PID)
	if rootErr != nil && !errors.Is(rootErr, os.ErrNotExist) {
		return rootErr
	}
	known := map[int64]int64{}
	for _, p := range ownerProcesses(*owner) {
		known[p.PID] = p.StartTicks
	}
	anchored := rootErr == nil && rootTicks == owner.StartTicks
	if !anchored {
		for _, row := range rows {
			expected, knownMember := known[row.pid]
			if row.group != owner.ProcessGroup || !knownMember {
				continue
			}
			ticks, err := s.processTicks(row.pid)
			if err == nil && ticks == expected {
				anchored = true
				break
			}
		}
	}
	next := *owner
	next.Workers = append([]engineProcessIdentity(nil), owner.Workers...)
	for _, row := range rows {
		if row.group != owner.ProcessGroup || row.pid == owner.PID {
			continue
		}
		ticks, err := s.processTicks(row.pid)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || ticks <= 0 {
			return os.ErrPermission
		}
		if expected, exists := known[row.pid]; exists {
			if expected != ticks {
				return os.ErrInvalid
			}
			continue
		}
		if !anchored || rootErr == nil && rootTicks != owner.StartTicks || ticks < owner.StartTicks {
			return os.ErrInvalid
		}
		if len(next.Workers) >= 128 {
			return os.ErrInvalid
		}
		identity := engineProcessIdentity{row.pid, ticks}
		if err := s.confirmEngineGroupMember(identity, owner.ProcessGroup); err != nil {
			return err
		}
		next.Workers = append(next.Workers, identity)
	}
	if reflect.DeepEqual(next, *owner) {
		return nil
	}
	if path != "" {
		if err := replaceEngineOwner(path, *owner, next); err != nil {
			return err
		}
	}
	*owner = next
	return nil
}

func replaceEngineOwner(path string, expected, next engineOwner) error {
	unlock, err := lockEngineOwner(path)
	if err != nil {
		return err
	}
	defer unlock()
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var current engineOwner
	if json.Unmarshal(raw, &current) != nil || !reflect.DeepEqual(current, expected) {
		return os.ErrInvalid
	}
	raw, err = json.Marshal(next)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "engine-owner-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (s *Service) ownedTreeAlive(owner *engineOwner, path string) (bool, error) {
	if err := s.refreshEngineTree(owner, path); err != nil {
		return false, err
	}
	alive := false
	for _, p := range ownerProcesses(*owner) {
		same, err := s.sameEngineProcess(p)
		if err != nil {
			return false, err
		}
		alive = alive || same
	}
	return alive, nil
}

func (s *Service) signalEngineTree(owner *engineOwner, path string, sig os.Signal) error {
	if err := s.refreshEngineTree(owner, path); err != nil {
		return err
	}
	// Validate every known member before sending the first signal.
	for _, p := range ownerProcesses(*owner) {
		if _, err := s.sameEngineProcess(p); err != nil {
			return err
		}
	}
	for _, p := range ownerProcesses(*owner) {
		same, err := s.sameEngineProcess(p)
		if err != nil {
			return err
		}
		if !same {
			continue
		}
		if err = s.signalOwnedPID(p.PID, sig); err != nil {
			same, checkErr := s.sameEngineProcess(p)
			if checkErr != nil || same {
				return err
			}
		}
	}
	return nil
}

func (s *Service) terminateOwnedTree(owner *engineOwner, path string) *ApiError {
	timeout := 10 * time.Second
	if s.orphanStopTimeout > 0 {
		timeout = s.orphanStopTimeout
	}
	for _, sig := range []os.Signal{syscall.SIGTERM, os.Kill} {
		if err := s.signalEngineTree(owner, path, sig); err != nil {
			return orphanIdentityError()
		}
		deadline := time.Now().Add(timeout)
		for {
			alive, err := s.ownedTreeAlive(owner, path)
			if err != nil {
				return orphanIdentityError()
			}
			if !alive {
				return nil
			}
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	return &ApiError{Code: "engine_stop_failed", Message: "引擎进程树尚未完全退出，所有权记录已保留。"}
}

func (s *Service) finishEngineTree(e *engineEntry) *ApiError {
	if !e.state.Local {
		return nil
	}
	return s.terminateOwnedTree(&e.owner, e.ownerPath)
}
