package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"inferyard-studio/internal/core"
	"inferyard-studio/internal/inferyard"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func setup(t *testing.T, exec core.Executor) (*Server, *httptest.Server) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c, e := core.New(ctx, core.Options{SettingsPath: filepath.Join(t.TempDir(), "settings.json"), Execute: exec, HostStatePath: filepath.Join(t.TempDir(), "host.json")})
	if e != nil {
		t.Fatal(e)
	}
	s, e := NewWithCore(ctx, fstest.MapFS{"index.html": {Data: []byte("embedded frontend")}}, c)
	if e != nil {
		t.Fatal(e)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}
func TestAuthenticationAndLoopback(t *testing.T) {
	s, ts := setup(t, nil)
	for _, tc := range []struct {
		path, token string
		code        int
	}{{"/", "", 401}, {"/?token=wrong", "", 401}, {"/?token=" + s.Token(), "", 200}, {"/api/status?token=" + s.Token(), "", 401}, {"/api/status", s.Token(), 401}, {"/api/status", "Bearer wrong", 401}, {"/api/status", "Bearer " + s.Token(), 200}, {"/api/events", "", 401}} {
		req, _ := http.NewRequest("GET", ts.URL+tc.path, nil)
		req.Header.Set("Authorization", tc.token)
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != tc.code {
			t.Fatalf("got %d expected %d", res.StatusCode, tc.code)
		}
		if res.Header.Get("Referrer-Policy") != "no-referrer" {
			t.Fatal("policy")
		}
		if tc.code == 401 && !strings.Contains(string(body), `"code":"unauthorized"`) {
			t.Fatal(string(body))
		}
	}
	for _, addr := range []string{"0.0.0.0:0", "192.168.1.1:0", "[::]:0", "localhost:0"} {
		if l, e := Listen(addr); e == nil {
			l.Close()
			t.Fatal(addr)
		}
	}
	l, e := Listen("127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	l.Close()
}
func request(t *testing.T, s *Server, ts *httptest.Server, method, path, body string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.Token())
	req.Header.Set("Content-Type", "application/json")
	r, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return r.StatusCode, b
}
func TestFixtureRunSSE(t *testing.T) {
	root := t.TempDir()
	s, ts := setup(t, func(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if args[0] == "--version" {
			return inferyard.Execution{Stdout: []byte("inferyard 1.0"), ExitCode: 0}, nil
		}
		dir := filepath.Join(root, "fixture")
		os.MkdirAll(dir, 0700)
		raw, _ := os.ReadFile("../evidence/testdata/run/inputs.json")
		var input map[string]any
		json.Unmarshal(raw, &input)
		docs := input["documents"].(map[string]any)
		docs["selection"] = map[string]any{"payload": map[string]any{"case_ids": []string{"case1"}}}
		raw, _ = json.Marshal(input)
		os.WriteFile(filepath.Join(dir, "inputs.json"), raw, 0600)
		f, _ := os.Create(filepath.Join(dir, "events.jsonl"))
		defer f.Close()
		m, _ := os.Create(filepath.Join(dir, "memory.jsonl"))
		defer m.Close()
		for i := 1; i <= 40; i++ {
			fmt.Fprintf(f, "{\"seq\":%d,\"phase\":\"formal\",\"utc\":\"2026-10-12T00:00:00Z\",\"event_type\":\"http_response\",\"data\":{}}\n", i)
			if i%10 == 0 {
				fmt.Fprintf(m, "{\"seq\":%d,\"utc\":\"2026-10-12T00:00:00Z\",\"metric_name\":\"system_mem_available\",\"value\":1024}\n", i)
			}
			select {
			case <-ctx.Done():
				return inferyard.Execution{ExitCode: 130}, ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
		return inferyard.Execution{ExitCode: 0, Stdout: []byte(`{"command":"run","status":"complete","completeness":"complete"}`)}, nil
	})
	settings, _ := json.Marshal(map[string]string{"results_root": root, "inferyard_path": os.Args[0]})
	if code, b := request(t, s, ts, "PUT", "/api/settings", string(settings)); code != 200 {
		t.Fatal(code, string(b))
	}
	config := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(filepath.Join(filepath.Dir(config), "zh-core.json"), []byte(`{"bundle_id":"zh-core"}`), 0600)
	os.WriteFile(config, []byte(fmt.Sprintf("[output]\nroot = %q\n[bundle]\npath = \"zh-core.json\"\n[endpoint]\nserver_pid = 1\nprocess_start_ticks = 1\n", root)), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events?token="+s.Token(), nil)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	body, _ := json.Marshal(map[string]any{"config_paths": []string{config}, "allow_single_bundle": true})
	if code, b := request(t, s, ts, "POST", "/api/runs", string(body)); code != 202 {
		t.Fatal(code, string(b))
	}
	reader := bufio.NewReader(res.Body)
	kind := ""
	counts := map[string]int{}
	var last time.Time
	minimum := time.Hour
	first := ""
	for {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if strings.HasPrefix(line, "event: ") {
			kind = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
		}
		if strings.HasPrefix(line, "data: ") {
			if first == "" {
				first = kind
			}
			counts[kind]++
			if kind == "run.progress" {
				at := time.Now()
				if !last.IsZero() && at.Sub(last) < minimum {
					minimum = at.Sub(last)
				}
				last = at
			}
			if kind == "run.state" && strings.Contains(line, `"state":"done"`) {
				break
			}
		}
	}
	if first != "run.state" || counts["run.progress"] < 2 || counts["run.resources"] < 1 || counts["sync.reset"] != 0 {
		t.Fatal(first, counts)
	}
	if minimum < 450*time.Millisecond {
		t.Fatal("unthrottled", minimum)
	}
	t.Logf("fixture input=40 events/2s; SSE counts=%v; minimum progress separation=%s", counts, minimum)
}
func TestHeartbeatReplayAndReset(t *testing.T) {
	s, ts := setup(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events?token="+s.Token(), nil)
	r, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	reader := bufio.NewReader(r.Body)
	start := time.Now()
	for {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if line == ": hb\n" {
			break
		}
	}
	t.Logf("idle heartbeat observed after %s", time.Since(start))
	for i := 0; i < 2050; i++ {
		s.Core.Bus.Publish("run.state", map[string]int{"n": i})
	}
	req2, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events?token="+s.Token(), nil)
	req2.Header.Set("Last-Event-ID", "1")
	r2, e := http.DefaultClient.Do(req2)
	if e != nil {
		t.Fatal(e)
	}
	defer r2.Body.Close()
	reader2 := bufio.NewReader(r2.Body)
	for {
		line, e := reader2.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if line == "event: sync.reset\n" {
			break
		}
	}
}
func TestReportStaticAndValidation(t *testing.T) {
	s, ts := setup(t, nil)
	root := filepath.Join(t.TempDir(), "results")
	payload, _ := json.Marshal(map[string]string{"results_root": root})
	request(t, s, ts, "PUT", "/api/settings", string(payload))
	report := filepath.Join(filepath.Dir(root), "reports", "report-20261012T000000Z")
	os.MkdirAll(report, 0700)
	os.WriteFile(filepath.Join(report, "report.html"), []byte("fixture report"), 0600)
	for _, tc := range []struct {
		p    string
		code int
	}{{"/reports/report-20261012T000000Z/report.html", 200}, {"/reports/report-20261012T000000Z/missing", 404}, {"/reports/report-20261012T000000Z/../outside", 400}} {
		code, b := request(t, s, ts, "GET", tc.p, "")
		if code != tc.code {
			t.Fatal(code, string(b))
		}
	}
	for _, body := range []string{`null`, `{}`, `{"run_paths":[]}`, `{"run_paths":["/outside"]}`, `{"run_paths":[1]}`} {
		code, _ := request(t, s, ts, "POST", "/api/reports", body)
		if code != 400 {
			t.Fatal(body, code)
		}
	}
}
func TestReportSurvivesClientDisconnect(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	s, ts := setup(t, func(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if args[0] == "--version" {
			return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 1.0")}, nil
		}
		close(started)
		select {
		case <-ctx.Done():
			return inferyard.Execution{}, ctx.Err()
		case <-release:
		}
		path := args[len(args)-1]
		os.MkdirAll(path, 0700)
		os.WriteFile(filepath.Join(path, "report.html"), []byte("report"), 0600)
		return inferyard.Execution{ExitCode: 0, Stdout: []byte(`{"command":"report","status":"rendered","completeness":"complete"}`)}, nil
	})
	root := t.TempDir()
	dir := filepath.Join(root, "run")
	os.MkdirAll(dir, 0700)
	for _, name := range []string{"inputs.json", "events.jsonl"} {
		raw, _ := os.ReadFile(filepath.Join("../evidence/testdata/run", name))
		os.WriteFile(filepath.Join(dir, name), raw, 0600)
	}
	settings, _ := json.Marshal(map[string]string{"results_root": root, "inferyard_path": os.Args[0]})
	request(t, s, ts, "PUT", "/api/settings", string(settings))
	ctx, cancel := context.WithCancel(context.Background())
	body, _ := json.Marshal(map[string]any{"run_paths": []string{dir}})
	req, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+"/api/reports", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+s.Token())
	_, unsub, cursor := s.Core.Bus.Subscribe()
	defer unsub()
	done := make(chan struct{})
	go func() {
		res, _ := http.DefaultClient.Do(req)
		if res != nil {
			res.Body.Close()
		}
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("report did not start")
	}
	cancel()
	<-done
	close(release)
	end := time.Now().Add(3 * time.Second)
	for time.Now().Before(end) {
		events, _, _ := s.Core.Bus.After(cursor)
		for _, e := range events {
			if e.Type == "report.created" {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no report.created after disconnect")
}
func TestStaticRangeErrorsUseJSON(t *testing.T) {
	s, _ := setup(t, nil)
	req := httptest.NewRequest("GET", "/?token="+s.Token(), nil)
	req.Header.Set("Range", "bytes=999999-")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":"invalid_request"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
