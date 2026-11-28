package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/remote"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockPeerCall struct {
	Method string
	Params json.RawMessage
}

type mockNodePeer struct {
	mu         sync.Mutex
	info       remote.NodePeerInfo
	connected  bool
	calls      []mockPeerCall
	responses  map[string]any
	errors     map[string]error
	closeCount int
}

func newMockNodePeer(nodeID, platform string) *mockNodePeer {
	osName, arch, _ := strings.Cut(platform, "/")
	return &mockNodePeer{
		info: remote.NodePeerInfo{
			NodeID: nodeID, Hostname: nodeID + "-host", OS: osName, Arch: arch,
			Platform: platform, CLIVersion: "1.2.3", HardwareSummary: "test GPU",
			Transport: NodeTransportMTLS, ConnectedAt: time.Now().UTC(),
		},
		connected: true, responses: map[string]any{}, errors: map[string]error{},
	}
}

func (p *mockNodePeer) NodeID() string            { return p.info.NodeID }
func (p *mockNodePeer) Info() remote.NodePeerInfo { return p.info }
func (p *mockNodePeer) Connected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.connected
}
func (p *mockNodePeer) Call(_ context.Context, method string, params, result any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	rawParams, _ := json.Marshal(params)
	p.calls = append(p.calls, mockPeerCall{Method: method, Params: rawParams})
	if err := p.errors[method]; err != nil {
		return err
	}
	response, ok := p.responses[method]
	if !ok {
		response = map[string]any{}
	}
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, result)
}
func (p *mockNodePeer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeCount++
	p.connected = false
	return nil
}
func (p *mockNodePeer) setConnected(connected bool) {
	p.mu.Lock()
	p.connected = connected
	p.mu.Unlock()
}
func (p *mockNodePeer) methodCalls(method string) []mockPeerCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []mockPeerCall
	for _, call := range p.calls {
		if call.Method == method {
			out = append(out, call)
		}
	}
	return out
}

type mockRemoteHub struct {
	mu         sync.Mutex
	peers      map[string]*mockNodePeer
	events     chan remote.HubEvent
	closeCount int
	paired     remote.PairedNodeCredentials
	sshSession remote.SSHSession
	tunnels    []remote.TunnelInfo
}

func newMockRemoteHub() *mockRemoteHub {
	return &mockRemoteHub{peers: map[string]*mockNodePeer{}, events: make(chan remote.HubEvent, 32)}
}
func (h *mockRemoteHub) Start(context.Context) error { return nil }
func (h *mockRemoteHub) Close() error {
	h.mu.Lock()
	h.closeCount++
	h.mu.Unlock()
	return nil
}
func (h *mockRemoteHub) Addr() string         { return "127.0.0.1:1" }
func (h *mockRemoteHub) HTTPJoinAddr() string { return "127.0.0.1:2" }
func (h *mockRemoteHub) CASha256() string     { return "ca" }
func (h *mockRemoteHub) StartPairingSession(context.Context, remote.PairingSessionOptions) (remote.PairingSession, error) {
	return remote.PairingSession{Code: "123456"}, nil
}
func (h *mockRemoteHub) ActivePairingSession() (remote.PairingSession, bool) {
	return remote.PairingSession{Code: "123456"}, true
}
func (h *mockRemoteHub) CancelPairingSession(string) error               { return nil }
func (h *mockRemoteHub) PendingPairingRequests() []remote.PairingRequest { return nil }
func (h *mockRemoteHub) DecidePairing(remote.PairingDecision) (remote.PairedNodeCredentials, error) {
	return h.paired, nil
}
func (h *mockRemoteHub) Peers() []remote.NodePeer {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]remote.NodePeer, 0, len(h.peers))
	for _, peer := range h.peers {
		out = append(out, peer)
	}
	return out
}
func (h *mockRemoteHub) Peer(nodeID string) (remote.NodePeer, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	peer, ok := h.peers[nodeID]
	if !ok {
		return nil, false
	}
	return peer, true
}
func (h *mockRemoteHub) RevokeNode(nodeID string) error {
	h.mu.Lock()
	delete(h.peers, nodeID)
	h.mu.Unlock()
	return nil
}
func (h *mockRemoteHub) OpenTunnel(_ context.Context, req remote.OpenTunnelRequest) (remote.TunnelInfo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tunnel := remote.TunnelInfo{TunnelID: "tunnel-1", NodeID: req.NodeID, ServiceID: req.ServiceID, LocalPort: 18001}
	h.tunnels = append(h.tunnels, tunnel)
	return tunnel, nil
}
func (h *mockRemoteHub) CloseTunnel(tunnelID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, tunnel := range h.tunnels {
		if tunnel.TunnelID == tunnelID {
			h.tunnels = append(h.tunnels[:i], h.tunnels[i+1:]...)
		}
	}
	return nil
}
func (h *mockRemoteHub) Tunnels() []remote.TunnelInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]remote.TunnelInfo(nil), h.tunnels...)
}
func (h *mockRemoteHub) ParseSSHCommand(raw string) (remote.ParsedSSHCommand, error) {
	return remote.ParsedSSHCommand{User: "gpu", Host: "example.test", Port: 22, IdentityFile: "/tmp/private-key", RawTarget: raw}, nil
}
func (h *mockRemoteHub) ConnectSSH(context.Context, remote.SSHConnectRequest) (remote.SSHSession, error) {
	return h.sshSession, nil
}
func (h *mockRemoteHub) PendingHostKeyRequests() []remote.HostKeyRequest { return nil }
func (h *mockRemoteHub) DecideHostKey(remote.HostKeyDecision) error      { return nil }
func (h *mockRemoteHub) DisconnectSSH(string) error                      { return nil }
func (h *mockRemoteHub) SubscribeEvents() (<-chan remote.HubEvent, func()) {
	return h.events, func() {}
}

func newNodeTestService(t *testing.T, hub *mockRemoteHub) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	svc, err := New(context.Background(), Options{
		SettingsPath: filepath.Join(root, "config", "settings.json"), RemoteHub: hub,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	svc.mu.Lock()
	svc.settings.WorkspaceRoot = &workspace
	svc.mu.Unlock()
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })
	return svc, workspace
}

func TestNodeStoreHasNoSecretsAndFiltersCorruption(t *testing.T) {
	hub := newMockRemoteHub()
	hub.paired = remote.PairedNodeCredentials{
		NodeID: "node-a", DisplayName: "Node A", Hostname: "host-a", Platform: "linux/amd64",
		PairedAt: time.Now().UTC(), CertFingerprint: "fingerprint-only",
	}
	hub.sshSession = remote.SSHSession{NodeID: "node-ssh", Host: "example.test", User: "gpu", ConnectedAt: time.Now().UTC()}
	svc, _ := newNodeTestService(t, hub)
	if _, apiErr := svc.DecideNodePairing(remote.PairingDecision{RequestID: "req", Option: remote.PairingApprove}); apiErr != nil {
		t.Fatal(apiErr)
	}
	_, apiErr := svc.ConnectSSHNode(context.Background(), remote.SSHConnectRequest{
		Command: "ssh -i /tmp/private-key gpu@example.test", Password: "super-secret-password",
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	path := filepath.Join(filepath.Dir(svc.settingsPath), "nodes.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, secret := range []string{"super-secret-password", "private-key", "ssh -i"} {
		if strings.Contains(text, secret) {
			t.Fatalf("nodes.json contains secret %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "ssh gpu@example.test") {
		t.Fatalf("safe SSH target missing: %s", text)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("nodes.json mode = %v, err=%v", info.Mode(), err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"nodes":[{"node_id":"../bad","transport":"mtls"},{"node_id":"node-ok","transport":"mtls"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	records, err := loadNodeStore(path)
	if err != nil || len(records) != 1 || records["node-ok"].NodeID != "node-ok" {
		t.Fatalf("corrupt records were not filtered: records=%+v err=%v", records, err)
	}
	if err := os.WriteFile(path, []byte(`not-json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if records, err := loadNodeStore(path); err != nil || len(records) != 0 {
		t.Fatalf("malformed store must not fail startup: records=%+v err=%v", records, err)
	}
}

func TestNodeOperationsIgnoreActiveViewAndPreserveOpaquePath(t *testing.T) {
	hub := newMockRemoteHub()
	peerA, peerB := newMockNodePeer("node-a", "windows/amd64"), newMockNodePeer("node-b", "windows/amd64")
	hub.peers[peerA.info.NodeID], hub.peers[peerB.info.NodeID] = peerA, peerB
	for _, peer := range []*mockNodePeer{peerA, peerB} {
		peer.responses[remote.MethodServiceStart] = map[string]any{"service": map[string]any{"service_id": "same-service", "state": "running"}}
		peer.responses[remote.MethodServiceStop] = map[string]any{"service": map[string]any{"service_id": "same-service", "state": "stopping"}}
	}
	svc, _ := newNodeTestService(t, hub)
	req := NodeServiceStartRequest{Model: `C:\Models\Qwen3.gguf`, Engine: "llama.cpp", ContextSize: 4096}
	if _, apiErr := svc.StartNodeService(context.Background(), "node-a", req); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := svc.SetActiveViewNodeID("node-b"); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, apiErr := svc.StartNodeService(context.Background(), "node-b", req); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, apiErr := svc.StopNodeService(context.Background(), "node-a", "same-service"); apiErr != nil {
		t.Fatal(apiErr)
	}
	if len(peerA.methodCalls(remote.MethodServiceStop)) != 1 || len(peerB.methodCalls(remote.MethodServiceStop)) != 0 {
		t.Fatalf("stop routed by active view: A=%+v B=%+v", peerA.calls, peerB.calls)
	}
	starts := peerA.methodCalls(remote.MethodServiceStart)
	var params map[string]any
	if json.Unmarshal(starts[0].Params, &params) != nil || params["model"] != req.Model {
		t.Fatalf("opaque Windows path changed: %s", starts[0].Params)
	}
	stateA, _ := svc.Node("node-a")
	stateB, _ := svc.Node("node-b")
	if len(stateA.Services) != 1 || len(stateB.Services) != 1 || stateA.Services[0].State != "stopping" || stateB.Services[0].State != "running" {
		t.Fatalf("same-name services not isolated: A=%+v B=%+v", stateA.Services, stateB.Services)
	}
}

func TestNodeTargetPlatformAndShutdown(t *testing.T) {
	hub := newMockRemoteHub()
	peer := newMockNodePeer("node-linux", "linux/amd64")
	hub.peers[peer.info.NodeID] = peer
	peer.responses[remote.MethodNodeEngineInstall] = map[string]any{"operation_id": "op-1", "state": "running"}
	svc, _ := newNodeTestService(t, hub)
	catalog, apiErr := svc.EngineCatalogForNode("node-linux")
	if apiErr != nil || len(catalog) == 0 {
		t.Fatalf("catalog failed: %v", apiErr)
	}
	for _, family := range catalog {
		for _, release := range family.Releases {
			for _, build := range release.Builds {
				if build.Platform != "linux/amd64" && build.Platform != "any" {
					t.Fatalf("wrong target build: %+v", build)
				}
			}
		}
	}
	var request ManagedEngineInstallRequest
	found := false
	for _, family := range DefaultEngineCatalog().Families() {
		for _, release := range family.ReleasesForPlatform("linux/amd64", false) {
			for _, build := range release.Builds {
				if !found && build.CanManagedVenvInstallOn("linux/amd64") {
					request = ManagedEngineInstallRequest{Family: family.ID, ReleaseID: release.ID, BuildID: build.ID}
					found = true
				}
			}
		}
	}
	if !found {
		t.Skip("no linux managed engine fixture")
	}
	operation, apiErr := svc.InstallNodeManagedEngine(context.Background(), "node-linux", request)
	if apiErr != nil || operation.OperationID != "op-1" {
		t.Fatalf("remote install failed: op=%+v err=%v", operation, apiErr)
	}
	calls := peer.methodCalls(remote.MethodNodeEngineInstall)
	var params map[string]any
	if json.Unmarshal(calls[0].Params, &params) != nil || params["target_platform"] != "linux/amd64" {
		t.Fatalf("target platform not forwarded: %s", calls[0].Params)
	}
	before := len(peer.calls)
	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hub.closeCount != 1 || len(peer.calls) != before || peer.closeCount != 0 {
		t.Fatalf("shutdown touched remote work: hub closes=%d calls=%d peer closes=%d", hub.closeCount, len(peer.calls)-before, peer.closeCount)
	}
}
