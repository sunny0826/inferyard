package e2e

import (
	"inferyard/studio/internal/inferyard"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestContextSpeedPublicCLI(t *testing.T) {
	uv, err := exec.LookPath("uv")
	if err != nil {
		t.Skip("mise uv required for public CLI fixture")
	}
	root := t.TempDir()
	if keep := os.Getenv("STUDIO_SPEED_E2E_ROOT"); keep != "" {
		root = keep
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "output")
	project := filepath.Join(repoRoot(t), "cli")
	helper := filepath.Join(repoRoot(t), "studio", "tests", "e2e", "context_speed_bridge.py")
	cmd := exec.CommandContext(t.Context(), uv, "run", "--project", project, "--frozen", "pytest", "-p", "no:cacheprovider", helper, "--basetemp", filepath.Join(root, "fixture"), "-q")
	cmd.Dir = project
	cmd.Env = append(os.Environ(), "PYTHONPATH="+project, "PYTHONDONTWRITEBYTECODE=1", "STUDIO_BRIDGE_OUTPUT="+output)
	log, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI fixture failed: %v\n%s", err, log)
	}
	raw, err := os.ReadFile(filepath.Join(output, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := inferyard.ParseContextSpeed(raw, 0)
	if err != nil {
		t.Fatalf("Studio rejected real CLI: %v", err)
	}
	if len(result.Samples) != 5 || result.Summary == nil || result.Measurement.ValidCount != 5 || result.Summary.Columns.Time.Median != 0.4 || result.Summary.Columns.Prefill.Median != 250 || result.Summary.Columns.Decode.Median != 510 {
		t.Fatalf("wrong production projection: %+v", result)
	}
	t.Logf("Synthetic CLI result and original hash evidence: %s", output)
}
