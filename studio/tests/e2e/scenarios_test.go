package e2e

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

func TestWizardCommandGuards(t *testing.T) {
	cli := ResolveCLI(t)
	env := NewEnv(t, cli)
	ctx := context.Background()
	service, err := core.New(ctx, env.Options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		_ = service.Shutdown(shutdownCtx)
	}()
	data, _ := json.Marshal(cli)
	if _, e := service.UpdateSettings(map[string]json.RawMessage{"inferyard_path": data}); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ name, command string }{
		{"missing inferyard prefix", "init --out " + env.Workspace},
		{"shell metacharacters", "inferyard init --out /tmp/x$(whoami)"},
		{"shell pipe", "inferyard init --out /tmp/x | tee"},
		{"unknown command", "inferyard frobnicate --out /tmp/x"},
		{"disallowed flag", "inferyard init --out /tmp/x --force"},
		{"relative output path", "inferyard init --out relative/dir"},
		{"report without runs", "inferyard report --out /tmp/x"},
		{"removed verify command", "inferyard verify --path /tmp/x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, apiErr := service.WizardExecute(ctx, core.WizardRequest{Command: tc.command})
			if apiErr == nil || apiErr.Code != "invalid_request" {
				t.Fatalf("invalid command %q: %+v", tc.command, apiErr)
			}
		})
	}
}
