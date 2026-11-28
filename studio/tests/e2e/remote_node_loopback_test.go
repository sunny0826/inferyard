package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/remote"
)

func TestRemoteNodeGoHubPythonAgentLoopback(t *testing.T) {
	cliPath := ResolveCLI(t)
	env := NewEnv(t, cliPath)
	hubDir := filepath.Join(t.TempDir(), "hub")
	stateDir := filepath.Join(t.TempDir(), "agent-state")

	hub, err := remote.NewHub(remote.HubConfig{
		DataDir:        hubDir,
		ListenAddr:     "127.0.0.1:0",
		HTTPJoinAddr:   "127.0.0.1:0",
		CandidateHosts: []string{"127.0.0.1"},
		CLIVersion:     "0.0.23",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	env.Options.RemoteHub = hub
	svc, err := core.New(context.Background(), env.Options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = svc.Shutdown(ctx)
	}()

	// 1. Start pairing session and verify HTTP join script delivery.
	session, apiErr := svc.StartNodePairing(context.Background(), remote.PairingSessionOptions{})
	if apiErr != nil {
		t.Fatalf("StartNodePairing: %+v", apiErr)
	}
	resp, err := http.Get(fmt.Sprintf("http://%s/join/%s", hub.HTTPJoinAddr(), session.Code))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /join/%s failed: resp=%v err=%v", session.Code, resp, err)
	}
	_ = resp.Body.Close()

	// 2. Run real Python `inferyard join` against Go Hub and approve via explicit option.
	joinDone := make(chan error, 1)
	var joinOut []byte
	go func() {
		cmd := exec.Command(cliPath, "join",
			"--hub", hub.Addr(),
			"--code", session.Code,
			"--ca-sha256", session.CASha256,
			"--state-dir", stateDir,
			"--no-autostart",
			"--source-hint", "LAN",
		)
		var runErr error
		joinOut, runErr = cmd.CombinedOutput()
		joinDone <- runErr
	}()

	var pairReq remote.PairingRequest
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		pending := svc.PendingPairingRequests()
		if len(pending) > 0 {
			pairReq = pending[0]
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pairReq.RequestID == "" {
		t.Fatalf("did not receive pairing request from python inferyard join; out=%s", string(joinOut))
	}
	creds, apiErr := svc.DecideNodePairing(remote.PairingDecision{
		RequestID:   pairReq.RequestID,
		Option:      remote.PairingApprove,
		DisplayName: "Loopback-Py-Node",
	})
	if apiErr != nil {
		t.Fatalf("DecideNodePairing: %+v", apiErr)
	}
	if joinErr := <-joinDone; joinErr != nil {
		t.Fatalf("inferyard join failed: %v\noutput: %s", joinErr, string(joinOut))
	}

	// 3. Spin up a local SSE server and authorize its port in agent_config.json for tunnel test.
	sseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, chunk := range []string{"data: hello\n\n", "data: world\n\n"} {
			_, _ = io.WriteString(w, chunk)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer sseServer.Close()
	parsedSSE, _ := url.Parse(sseServer.URL)
	ssePort, _ := strconv.Atoi(parsedSSE.Port())
	allowAgentTunnelPort(t, stateDir, ssePort)

	// 4. Launch real Python `inferyard agent run` and wait for mTLS peer online.
	agentCtx, cancelAgent := context.WithCancel(context.Background())
	agentCmd := exec.CommandContext(agentCtx, cliPath, "agent", "run",
		"--state-dir", stateDir,
		"--hub", hub.Addr(),
	)
	var agentBuf strings.Builder
	agentCmd.Stdout = &agentBuf
	agentCmd.Stderr = &agentBuf
	if err := agentCmd.Start(); err != nil {
		t.Fatalf("start inferyard agent run: %v", err)
	}
	defer func() {
		cancelAgent()
		_ = agentCmd.Wait()
	}()

	onlineDeadline := time.Now().Add(15 * time.Second)
	var online bool
	for time.Now().Before(onlineDeadline) {
		if snap, apiErr := svc.Node(creds.NodeID); apiErr == nil && snap.Online {
			online = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !online {
		t.Fatalf("python agent did not come online on Go Hub; agent output:\n%s", agentBuf.String())
	}

	// 5. Verify cross-process RPC (`node.device_check`) and normalized platform.
	snap, _ := svc.Node(creds.NodeID)
	if !strings.Contains(snap.Record.Platform, "/") {
		t.Fatalf("expected normalized goos/goarch platform, got %q", snap.Record.Platform)
	}
	devResp, apiErr := svc.RefreshNodeDevice(context.Background(), creds.NodeID)
	if apiErr != nil || len(devResp.Snapshot) == 0 {
		t.Fatalf("RefreshNodeDevice failed: resp=%+v err=%#v\nagent output:\n%s", devResp, apiErr, agentBuf.String())
	}

	// 6. Open on-demand mTLS TUNNEL and stream SSE through Go Hub <-> Python Agent.
	tunnel, apiErr := svc.OpenNodeTunnel(context.Background(), remote.OpenTunnelRequest{
		NodeID:     creds.NodeID,
		ServiceID:  "sse-test-svc",
		RemotePort: ssePort,
	})
	if apiErr != nil {
		t.Fatalf("OpenNodeTunnel: %+v", apiErr)
	}
	tunnelResp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", tunnel.LocalPort))
	if err != nil {
		t.Fatalf("GET via tunnel: %v", err)
	}
	defer tunnelResp.Body.Close()
	scanner := bufio.NewScanner(tunnelResp.Body)
	var lines []string
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	if got := strings.Join(lines, "|"); got != "data: hello|data: world" {
		t.Fatalf("unexpected tunneled SSE payload: %q", got)
	}
}

func allowAgentTunnelPort(t *testing.T, stateDir string, port int) {
	t.Helper()
	cfgPath := filepath.Join(stateDir, "agent_config.json")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read agent_config.json: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse agent_config.json: %v", err)
	}
	cfg["allowed_tunnel_ports"] = []int{port}
	updated, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, append(updated, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
