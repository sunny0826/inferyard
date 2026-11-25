package core

import (
	"os"
	"runtime"
	"testing"
)

func TestSampleProcessTreeRSSGB(t *testing.T) {
	if got := sampleProcessTreeRSSGB(nil); got != nil {
		t.Fatalf("expected nil for empty pids, got %v", *got)
	}
	if got := sampleProcessTreeRSSGB([]int{0, -5}); got != nil {
		t.Fatalf("expected nil for non-positive pids, got %v", *got)
	}
	if got := sampleProcessTreeRSSGB([]int{99999999}); got != nil {
		t.Fatalf("expected nil for nonexistent pid, got %v", *got)
	}
	if runtime.GOOS != "windows" {
		got := sampleProcessTreeRSSGB([]int{os.Getpid()})
		if got == nil || *got <= 0 {
			t.Fatalf("expected positive non-nil RSS GB for current process on %s, got %v", runtime.GOOS, got)
		}
	}
}

func TestParsePSRSSOutputGB(t *testing.T) {
	if got := parsePSRSSOutputGB([]byte("")); got != nil {
		t.Fatalf("expected nil for empty output, got %v", *got)
	}
	if got := parsePSRSSOutputGB([]byte("   \n  0 \n")); got != nil {
		t.Fatalf("expected nil for zero output, got %v", *got)
	}
	// 2 * 1024 * 1024 KB = 2.00 GB, plus 524288 KB = 0.50 GB -> 2.50 GB
	got := parsePSRSSOutputGB([]byte("  2097152 \n 524288\n"))
	if got == nil || *got != 2.5 {
		t.Fatalf("expected 2.5 GB, got %v", got)
	}
}

func TestServiceMemoryUsageHelper(t *testing.T) {
	measured := 12.34
	gb, isMeasured, known := ServiceMemoryUsage(EngineState{
		RuntimeMemoryGB:   &measured,
		EstimatedMemoryGB: 15.0,
	})
	if !known || !isMeasured || gb != 12.34 {
		t.Fatalf("expected live measured 12.34, got gb=%v measured=%v known=%v", gb, isMeasured, known)
	}

	gb, isMeasured, known = ServiceMemoryUsage(EngineState{
		EstimatedMemoryGB: 15.25,
	})
	if !known || isMeasured || gb != 15.25 {
		t.Fatalf("expected static estimate 15.25, got gb=%v measured=%v known=%v", gb, isMeasured, known)
	}

	gb, isMeasured, known = ServiceMemoryUsage(EngineState{})
	if known || isMeasured || gb != 0 {
		t.Fatalf("expected unknown memory when both absent, got gb=%v measured=%v known=%v", gb, isMeasured, known)
	}
}
