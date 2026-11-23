package inferyard

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseProgressVersionClassification(t *testing.T) {
	v1Raw, err := os.ReadFile(filepath.Join("testdata", "progress.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseProgress(v1Raw); err == nil {
		t.Fatal("expected error for progress_version 1")
	} else {
		var verErr ProgressVersionError
		if !errors.As(err, &verErr) || verErr.Version != 1 || err.Error() != "unsupported progress version" {
			t.Fatalf("unexpected error for v1: %T %+v (%v)", err, verErr, err)
		}
	}

	v2Raw, err := os.ReadFile(filepath.Join("testdata", "progress.v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseProgress(v2Raw)
	if err != nil {
		t.Fatalf("ParseProgress(v2) failed: %v", err)
	}
	if p.ProgressVersion != 2 || p.Workspace != "/work/ws" {
		t.Fatalf("unexpected v2 progress: %+v", p)
	}

	missingVersion := []byte(`{"kind":"progress","workspace":"/work/ws","device_preflight":{"status":"ok","path":"/w/p"},"bundles":[],"report":{"status":"missing","path":null,"runs":[]},"next_action":"done","next_command":null,"default_scope_complete":true}`)
	if _, err := ParseProgress(missingVersion); err == nil {
		t.Fatal("expected error when progress_version is missing")
	} else {
		var verErr ProgressVersionError
		if !errors.As(err, &verErr) || verErr.Version != 0 {
			t.Fatalf("unexpected error for missing version: %T %+v (%v)", err, verErr, err)
		}
	}

	futureVersion := []byte(`{"kind":"progress","progress_version":3,"workspace":"/work/ws","device_preflight":{"status":"ok","path":"/w/p"},"bundles":[],"report":{"status":"missing","path":null,"runs":[]},"next_action":"done","next_command":null,"default_scope_complete":true}`)
	if _, err := ParseProgress(futureVersion); err == nil {
		t.Fatal("expected error for progress_version 3")
	} else {
		var verErr ProgressVersionError
		if !errors.As(err, &verErr) || verErr.Version != 3 {
			t.Fatalf("unexpected error for v3: %T %+v (%v)", err, verErr, err)
		}
	}
}
