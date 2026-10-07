package observe

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestNativeOwnProcessAndExitedChild(t *testing.T) {
	if os.Getenv("LAB_OBSERVER_TEST_CHILD") == "1" {
		time.Sleep(time.Minute)
		return
	}
	p, err := NativeProcess(os.Getpid())
	if err != nil || p.Target.StartTicks == 0 || p.CPUNS.Value == nil || p.RSS.Value == nil || *p.RSS.Value == 0 {
		t.Fatal(p, err)
	}
	next, err := NativeProcess(os.Getpid())
	if err != nil || next.Target.StartTicks != p.Target.StartTicks || *next.CPUNS.Value < *p.CPUNS.Value {
		t.Fatal(next, err)
	}
	i, host := NativeInventory(), NativeHost(t.TempDir())
	if i.MemoryTotal.Value == nil || *i.MemoryTotal.Value == 0 || host.DiskAvailable.Value == nil {
		t.Fatal(i, host)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestNativeOwnProcessAndExitedChild$")
	child.Env = append(os.Environ(), "LAB_OBSERVER_TEST_CHILD=1")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { child.Process.Kill(); child.Wait() })
	if _, err := NativeProcess(child.Process.Pid); err != nil {
		t.Fatal(err)
	}
	child.Process.Kill()
	child.Wait()
	if _, err := NativeProcess(child.Process.Pid); err != ErrExited {
		t.Fatalf("dead child must be exited, got %v", err)
	}
}
