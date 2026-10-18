package pages

import (
	"os"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func TestContextSpeedRealCLIProjection(t *testing.T) {
	path := os.Getenv("STUDIO_SPEED_RESULT")
	if path == "" {
		t.Skip("set STUDIO_SPEED_RESULT to a newly generated synthetic CLI result")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := inferyard.ParseContextSpeed(raw, 0)
	if err != nil {
		t.Fatal(err)
	}
	task := &speedTask{model: "unrelated current model", engine: "unrelated current engine"}
	applySpeedResult(task, result)
	if len(task.samples) != 5 || task.summary == nil || task.context != 65536 || task.validCount() != 5 {
		t.Fatalf("wrong projection: %+v", task)
	}
	summary := task.summary
	if summary.prompt == nil || *summary.prompt != 100 || summary.seconds == nil || *summary.seconds != 0.4 || summary.prefill == nil || *summary.prefill != 250 || summary.decode == nil || *summary.decode != 510 {
		t.Fatalf("wrong values: %+v", summary)
	}
	if task.model != result.Metadata.Model.DisplayName || task.engine != result.Metadata.Engine.Release {
		t.Fatal("current selection replaced frozen identity")
	}
}
