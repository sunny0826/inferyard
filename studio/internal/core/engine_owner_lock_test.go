package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestOwnerLockHelper(t *testing.T) {
	path := os.Getenv("INFERYARD_OWNER_LOCK_TEST")
	if path == "" {
		return
	}
	if err := os.WriteFile(path+".ready", []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeEngineOwner(path, engineOwner{PID: 42, StartTicks: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerRemovalSerializesAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "engine-owner.json")
	old := engineOwner{PID: 42, StartTicks: 1}
	if err := writeEngineOwner(path, old); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockEngineOwner(path)
	if err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()
	cmd := exec.Command(os.Args[0], "-test.run=^TestOwnerLockHelper$")
	cmd.Env = append(os.Environ(), "INFERYARD_OWNER_LOCK_TEST="+path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() { _ = cmd.Process.Kill() }()
	wait(t, func() bool { _, err := os.Stat(path + ".ready"); return err == nil })
	select {
	case err := <-done:
		t.Fatalf("child bypassed lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	// Replace while holding the same mutation lock, as another owner operation
	// would. The child must compare only after this new record is visible.
	fresh := engineOwner{PID: 42, StartTicks: 2}
	raw, _ := json.Marshal(fresh)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	unlock()
	locked = false
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child remained locked")
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal("new record removed", err)
	}
	var saved engineOwner
	if json.Unmarshal(raw, &saved) != nil || !reflect.DeepEqual(saved, fresh) {
		t.Fatalf("%s", raw)
	}
}
