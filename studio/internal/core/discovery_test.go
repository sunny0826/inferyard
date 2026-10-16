package core

import (
	"context"
	"encoding/json"
	"errors"
	"inferyard/studio/internal/inferyard"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const discoveryFixture = `{"command":"service discover","status":"discovered","completeness":"complete","run_id":null,"evidence_dir":null,"limitations":[],"details":{"model_requests_sent":0,"services":[{"pid":6124,"process_name":"inference-engine","process_start_ticks":12345,"endpoints":[{"url":"http://127.0.0.1:8080","health":"2xx","models":["/models/Qwen.gguf"],"reason":null,"ambiguous_listener":true}]}]}}`

func TestDiscoverUsesCLIAndPreservesAmbiguity(t *testing.T) {
	s := wizardService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if args[0] == "--version" {
			return inferyard.Execution{Stdout: []byte("inferyard fixture source=release")}, nil
		}
		if !reflect.DeepEqual(args, []string{"service", "discover"}) {
			t.Fatalf("unexpected invocation %v", args)
		}
		return inferyard.Execution{Stdout: []byte(discoveryFixture)}, nil
	})
	r, e := s.Discover(context.Background())
	if e != nil || len(r.Services) != 1 {
		t.Fatalf("%+v %v", r, e)
	}
	service := r.Services[0]
	if service.PID != 6124 || service.ProcessStartTicks != 12345 || service.ProcessName != "inference-engine" ||
		!service.Endpoints[0].AmbiguousListener || *service.Endpoints[0].Health != "2xx" || service.Endpoints[0].Models[0] != "/models/Qwen.gguf" {
		t.Fatal(service)
	}
}

func TestDiscoverFailuresAndPartialResults(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		exit      int
		code      string
	}{
		{"unknown command", `{"command":"unknown"}`, 2, "cli_outdated"},
		{"old parser", "invalid CLI arguments; use --help\n{\"command\":\"unknown\"}", 2, "cli_outdated"},
		{"invalid JSON", "secret diagnostic", 0, "service_discovery_invalid_response"},
		{"known command rejected", `{"command":"service discover"}`, 2, "service_discovery_invalid_response"},
		{"blocked", `{"command":"service discover","status":"blocked","completeness":"incomplete","run_id":null,"evidence_dir":null,"limitations":["invalid_input"],"details":null}`, 2, "cli_blocked"},
		{"cancelled", `{"command":"service discover","status":"interrupted","completeness":"incomplete","run_id":null,"evidence_dir":null,"limitations":["cancelled"],"details":null}`, 130, "cancelled"},
		{"invalid cancellation", `{"command":"service discover","status":"blocked","completeness":"incomplete","run_id":null,"evidence_dir":null,"limitations":["invalid_input"],"details":null}`, 130, "service_discovery_invalid_response"},
		{"partial", strings.NewReplacer(`"status":"discovered"`, `"status":"partial"`, `"completeness":"complete"`, `"completeness":"incomplete"`, `"limitations":[]`, `"limitations":["listener_ownership_incomplete"]`).Replace(discoveryFixture), 3, ""},
		{"tool failure", strings.NewReplacer(`"status":"discovered"`, `"status":"error"`, `"completeness":"complete"`, `"completeness":"incomplete"`, `"limitations":[]`, `"limitations":["listener_enumeration_failed"]`).Replace(discoveryFixture), 4, "service_discovery_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := progressService(t, tc.exit, tc.raw)
			r, e := s.Discover(context.Background())
			if tc.code == "" {
				if e != nil || len(r.Services) != 1 || !reflect.DeepEqual(r.Limitations, []string{"listener_ownership_incomplete"}) {
					t.Fatalf("%+v %v", r, e)
				}
			} else if e == nil || e.Code != tc.code {
				t.Fatalf("%+v", e)
			} else if tc.code == "cli_blocked" && !reflect.DeepEqual(e.Limitations, []string{"invalid_input"}) {
				t.Fatal(e)
			} else if tc.code == "cancelled" && (r.Status != "interrupted" || !reflect.DeepEqual(e.Limitations, []string{"cancelled"})) {
				t.Fatal(r, e)
			} else if tc.exit == 4 && !reflect.DeepEqual(e.Limitations, []string{"listener_enumeration_failed"}) {
				t.Fatal(e)
			}
		})
	}
}

func TestDiscoverMissingCLI(t *testing.T) {
	s := progressService(t, 0, discoveryFixture)
	s.settings.InferyardPath = ptr(filepath.Join(t.TempDir(), "missing-cli"))
	if _, e := s.Discover(context.Background()); e == nil || e.Code != "cli_outdated" {
		t.Fatal(e)
	}
	s = wizardService(t, func(context.Context, string, ...string) (inferyard.Execution, error) {
		return inferyard.Execution{}, errors.New("version unavailable")
	})
	if _, e := s.Discover(context.Background()); e == nil || e.Code != "cli_outdated" {
		t.Fatal(e)
	}
}

func TestDiscoverRejectsMalformedWireContract(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(discoveryFixture, `"pid":6124`, `"pid":true`, 1),
		strings.Replace(discoveryFixture, `"pid":6124`, `"pid":0`, 1),
		strings.Replace(discoveryFixture, `"pid":6124`, `"pid":6124,"pid":6125`, 1),
		strings.Replace(discoveryFixture, `"ambiguous_listener":true`, `"ambiguous_listener":null`, 1),
		strings.Replace(discoveryFixture, `,"ambiguous_listener":true`, ``, 1),
		strings.Replace(discoveryFixture, `"model_requests_sent":0`, `"model_requests_sent":1`, 1),
		strings.Replace(discoveryFixture, `"models":["/models/Qwen.gguf"]`, `"models":null`, 1),
		strings.Replace(discoveryFixture, `"models":["/models/Qwen.gguf"]`, `"models":[]`, 1),
		strings.Replace(discoveryFixture, `"reason":null`, `"reason":"unknown"`, 1),
		strings.Replace(discoveryFixture, `127.0.0.1`, `192.168.1.1`, 1),
		discoveryFixture + `{}`,
	} {
		s := progressService(t, 0, raw)
		if _, e := s.Discover(context.Background()); e == nil || e.Code != "service_discovery_invalid_response" {
			t.Fatalf("accepted %s: %v", raw, e)
		}
	}
	var value map[string]any
	_ = json.Unmarshal([]byte(discoveryFixture), &value)
	value["details"].(map[string]any)["services"] = []any{}
	raw, _ := json.Marshal(value)
	s := progressService(t, 0, string(raw))
	if r, e := s.Discover(context.Background()); e != nil || len(r.Services) != 0 {
		t.Fatalf("empty list: %+v %v", r, e)
	}
}

func TestBindPreservesFixedCLIFailureReasons(t *testing.T) {
	s := progressService(t, 2, `{"command":"config bind","status":"blocked","completeness":"incomplete","limitations":["startup_arguments_mismatch","service_process_unavailable","/secret/path"]}`)
	result, e := s.WizardExecute(context.Background(), WizardRequest{Command: "inferyard config bind --candidate /candidate --pid 6124 --endpoint http://127.0.0.1:8080 --out " + filepath.Join(t.TempDir(), "new")})
	if e != nil || result.ExitCode != 2 || !reflect.DeepEqual(result.Limitations, []string{"startup_arguments_mismatch", "service_process_unavailable"}) {
		t.Fatalf("%+v %v", result, e)
	}
}
