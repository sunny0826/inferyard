//go:build darwin && cgo

package observe

import (
	"math"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestDarwinMachCPUConvertedToNanoseconds(t *testing.T) {
	var before, after syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &before)
	p, err := NativeProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(40 * time.Millisecond)
	for time.Now().Before(deadline) {
	}
	next, err := NativeProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	syscall.Getrusage(syscall.RUSAGE_SELF, &after)
	actual := float64(*next.CPUNS.Value - *p.CPUNS.Value)
	reference := float64(after.Utime.Nano() + after.Stime.Nano() - before.Utime.Nano() - before.Stime.Nano())
	if reference <= 0 || math.Abs(actual-reference) > math.Max(3_000_000, reference*0.15) {
		t.Fatalf("CPU units: libproc_ns=%v rusage_ns=%v", actual, reference)
	}
}
