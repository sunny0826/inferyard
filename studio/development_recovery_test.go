//go:build studio_dev && !release

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

func TestDevelopmentRecoveryRecoversExitedChildWithoutReceipt(t *testing.T) {
	m := devFixture(t)
	if err := os.MkdirAll(filepath.Join(m.ProfileRoot, "workspace"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicDevJSON(filepath.Join(filepath.Dir(m.path), "child.json"), struct {
		PID int64 `json:"pid"`
	}{2147483647}); err != nil {
		t.Fatal(err)
	}
	m, err := loadDevManifest(m.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recoverDevelopment(context.Background(), m, m, nativeDevRecoveryReader()); err != nil {
		t.Fatalf("exited child without session receipt cannot recover: %v", err)
	}
	if _, err := os.Lstat(m.sessionPath()); !os.IsNotExist(err) {
		t.Fatal("recovery invented a session receipt", err)
	}
}

func TestDevelopmentRecoveryLockChild(t *testing.T) {
	workspace := os.Getenv("INFERYARD_RECOVERY_LOCK_CHILD")
	if workspace == "" {
		return
	}
	unlock, err := core.DevelopmentLockEngineOwner(context.Background(), workspace)
	if err != nil {
		os.Exit(2)
	}
	fmt.Println("ready")
	io.Copy(io.Discard, os.Stdin)
	if err := os.WriteFile(filepath.Join(workspace, "logs", "engine-owner.json"), []byte("fixture owner"), 0600); err != nil {
		os.Exit(3)
	}
	unlock()
	os.Exit(0)
}

func TestDevelopmentRecoveryWaitsForRealOwnerWriter(t *testing.T) {
	m := recoveryFixture(t)
	workspace := filepath.Join(m.ProfileRoot, "workspace")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-test.run=^TestDevelopmentRecoveryLockChild$")
	cmd.Env = append(os.Environ(), "INFERYARD_RECOVERY_LOCK_CHILD="+workspace)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		input.Close()
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatal(line, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := recoverDevelopment(ctx, m, m, absentRecoveryReader()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("recovery bypassed owner writer", err)
	}
	input.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverDevelopment(context.Background(), m, m, absentRecoveryReader()); err == nil {
		t.Fatal("owner published before unlock was ignored")
	}
}

func TestDevelopmentRecoveryHandoffCannotDowngradeAfterSessionDisappears(t *testing.T) {
	m := recoveryFixture(t)
	publishRecoverySession(t, m, "stopped", 2147483647)
	result, err := recoverDevelopment(context.Background(), m, m, absentRecoveryReader())
	if err != nil || result.Outcome != "session_present" {
		t.Fatal(result, err)
	}
	if err := os.Remove(m.sessionPath()); err != nil {
		t.Fatal(err)
	}
	if err := m.stopSession(context.Background(), nativeDevProcessReader()); err == nil {
		t.Fatal("original stop downgraded missing session to recovery")
	}
}

func recoveryFixture(t *testing.T) *devManifest {
	t.Helper()
	m := devFixture(t)
	if err := os.MkdirAll(filepath.Join(m.ProfileRoot, "workspace"), 0700); err != nil {
		t.Fatal(err)
	}
	writeDevFile(t, filepath.Join(filepath.Dir(m.path), "child.json"), `{"pid":2147483647}`)
	m, err := loadDevManifest(m.path)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func absentRecoveryReader() devRecoveryReader {
	reader := nativeDevRecoveryReader()
	reader.observe = func(int64) (core.DevelopmentProcessState, error) { return core.DevelopmentProcessAbsent, nil }
	return reader
}

func TestDevelopmentRecoveryRejectsUnsafeRecords(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"pid":null}`, `{"pid":true}`, `{"pid":1.0}`, `{"pid":0}`, `{"pid":-1}`, `{"pid":4294967296}`, `{"pid":1,"pid":2}`, `{"pid":1,"extra":0}`, `{"PID":1}`, `{"pid":1} {}`} {
		t.Run(body, func(t *testing.T) {
			m := recoveryFixture(t)
			writeDevFile(t, filepath.Join(filepath.Dir(m.path), "child.json"), body)
			if _, err := recoverDevelopment(context.Background(), m, m, absentRecoveryReader()); err == nil {
				t.Fatal("invalid child accepted")
			}
		})
	}
	for _, name := range []string{"session.json", "child.json", "stop.json"} {
		t.Run("symlink-"+name, func(t *testing.T) {
			m := recoveryFixture(t)
			path := filepath.Join(filepath.Dir(m.path), name)
			os.Remove(path)
			if err := os.Symlink(m.path, path); err != nil {
				t.Skip(err)
			}
			if _, err := recoverDevelopment(context.Background(), m, m, absentRecoveryReader()); err == nil {
				t.Fatal("symlink accepted")
			}
		})
	}
	for _, name := range []string{"session.json", "stop.json"} {
		for _, body := range []string{`null`, `{}`, `{"session_id":"wrong"}`} {
			t.Run(name+body, func(t *testing.T) {
				m := recoveryFixture(t)
				path := filepath.Join(filepath.Dir(m.path), name)
				writeDevFile(t, path, body)
				if _, err := recoverDevelopment(context.Background(), m, m, absentRecoveryReader()); err == nil {
					t.Fatal("bad record accepted")
				}
				if got, _ := os.ReadFile(path); string(got) != body {
					t.Fatal("bad record overwritten")
				}
			})
		}
	}
}

func TestDevelopmentRecoveryRejectsPresentUnknownAndChangedInputs(t *testing.T) {
	for _, state := range []core.DevelopmentProcessState{core.DevelopmentProcessPresent, core.DevelopmentProcessUnknown} {
		t.Run(fmt.Sprint(state), func(t *testing.T) {
			m := recoveryFixture(t)
			reader := absentRecoveryReader()
			reader.observe = func(int64) (core.DevelopmentProcessState, error) { return state, nil }
			if _, err := recoverDevelopment(context.Background(), m, m, reader); err == nil {
				t.Fatal("unproven absence accepted")
			}
		})
	}
	for _, change := range []string{"native-error", "second-present", "child", "manifest", "binary", "owner", "workspace", "lock-error"} {
		t.Run(change, func(t *testing.T) {
			m := recoveryFixture(t)
			reader := absentRecoveryReader()
			calls := 0
			reader.observe = func(int64) (core.DevelopmentProcessState, error) {
				calls++
				if change == "native-error" {
					return core.DevelopmentProcessUnknown, os.ErrPermission
				}
				if calls == 2 && change == "second-present" {
					return core.DevelopmentProcessPresent, nil
				}
				if calls == 1 {
					switch change {
					case "child":
						writeDevFile(t, filepath.Join(filepath.Dir(m.path), "child.json"), `{"pid":123}`)
					case "manifest":
						writeDevFile(t, m.path, `{}`)
					case "binary":
						writeDevFile(t, m.studioPath(), "changed")
					case "owner":
						os.MkdirAll(filepath.Join(m.ProfileRoot, "workspace", "logs"), 0700)
						writeDevFile(t, filepath.Join(m.ProfileRoot, "workspace", "logs", "engine-owner.json"), "broken owner")
					case "workspace":
						os.Remove(filepath.Join(m.ProfileRoot, "workspace"))
					}
				}
				return core.DevelopmentProcessAbsent, nil
			}
			if change == "lock-error" {
				reader.lock = func(context.Context, string) (func(), error) { return nil, os.ErrPermission }
			}
			if _, err := recoverDevelopment(context.Background(), m, m, reader); err == nil {
				t.Fatal("unsafe change accepted")
			}
		})
	}
}

func publishRecoverySession(t *testing.T, m *devManifest, state string, pid int64) {
	t.Helper()
	s := devSession{Kind: "studio_dev_session.v1", SessionID: m.sessionID(), PID: pid, StartTicks: 1, State: state, CLIPath: m.cliPath()}
	if err := atomicDevJSON(m.sessionPath(), s); err != nil {
		t.Fatal(err)
	}
}

func TestDevelopmentRecoveryLateSessions(t *testing.T) {
	for _, state := range []string{"starting", "ready", "stopped", "shutdown_failed", "wrong-pid"} {
		for _, barrier := range []int{0, 1, 2} {
			t.Run(fmt.Sprintf("%s-%d", state, barrier), func(t *testing.T) {
				m := recoveryFixture(t)
				reader := absentRecoveryReader()
				publish := func() {
					pid := int64(2147483647)
					next := state
					if state == "wrong-pid" {
						pid = 12
						next = "ready"
					}
					publishRecoverySession(t, m, next, pid)
				}
				calls := 0
				reader.observe = func(int64) (core.DevelopmentProcessState, error) {
					calls++
					if calls == barrier {
						publish()
					}
					return core.DevelopmentProcessAbsent, nil
				}
				if barrier == 0 {
					publish()
				}
				result, err := recoverDevelopment(context.Background(), m, m, reader)
				bad := state == "shutdown_failed" || state == "wrong-pid" && barrier > 0
				if bad {
					if err == nil {
						t.Fatal("failed/mismatched late session accepted")
					}
					return
				}
				if err != nil || result.Outcome != "session_present" {
					t.Fatal("late session was ignored", result, err)
				}
			})
		}
	}
}

func TestDevelopmentRecoveryPreservesInputsAndNeedsNoCLI(t *testing.T) {
	m := recoveryFixture(t)
	child := filepath.Join(filepath.Dir(m.path), "child.json")
	before := map[string]string{}
	for _, p := range []string{m.path, m.studioPath(), child} {
		before[p], _ = fileDigest(p)
	}
	if err := os.RemoveAll(filepath.Join(m.BuildRoot, "cli-env")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		result, err := recoverDevelopment(context.Background(), m, m, absentRecoveryReader())
		if err != nil || result.Outcome != "recovered_missing_session" || result.ChildSHA != before[child] {
			t.Fatal(result, err)
		}
	}
	for p, hash := range before {
		if err := checkDigest(p, hash); err != nil {
			t.Fatal("recovery changed original", err)
		}
	}
	if !m.stopRequested() {
		t.Fatal("Go did not create cancellation intent")
	}
	other := *m
	other.Checkout = filepath.Join(m.Checkout, "other")
	if _, err := recoverDevelopment(context.Background(), &other, m, absentRecoveryReader()); err == nil {
		t.Fatal("cross-checkout accepted")
	}
	writeDevFile(t, m.path, strings.Repeat(" ", 1<<20)+"{}")
	if _, err := loadDevManifest(m.path); err == nil {
		t.Fatal("oversized JSON accepted")
	}
}

func TestDevelopmentRecoveryOptions(t *testing.T) {
	pair := []string{"--dev-recovery-controller", "/a", "--dev-recovery-target", "/b"}
	if _, err := parseOptions(pair, os.Stderr); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		pair[:2], pair[2:], {"--dev-recovery-controller", "", "--dev-recovery-target", "/b"},
		append(append([]string{}, pair...), "--dev-stop", "/c"),
		append(append([]string{}, pair...), "--debug-ui"),
	} {
		if _, err := parseOptions(args, os.Stderr); err == nil {
			t.Fatal("invalid recovery options accepted", args)
		}
	}
}
