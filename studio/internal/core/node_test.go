package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/remote"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

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
	var params struct {
		TargetPlatform string   `json:"target_platform"`
		SourceURL      string   `json:"source_url"`
		ReleaseVersion string   `json:"release_version"`
		Python         string   `json:"python"`
		Packages       []string `json:"packages"`
		WheelURLs      []string `json:"wheel_urls"`
	}
	if json.Unmarshal(calls[0].Params, &params) != nil || params.TargetPlatform != "linux/amd64" {
		t.Fatalf("target platform not forwarded: %s", calls[0].Params)
	}
	request.TargetPlatform = "linux/amd64"
	_, release, build, failure := managedEngineBuild(request)
	if failure != nil {
		t.Fatal(failure)
	}
	if params.SourceURL != managedEngineSourceURL(release, build) || params.ReleaseVersion != release.Version || params.Python != build.ManagedSpec.Python ||
		!reflect.DeepEqual(params.Packages, build.ManagedSpec.Packages) || !reflect.DeepEqual(params.WheelURLs, build.ManagedSpec.WheelURLs) {
		t.Fatalf("managed build not forwarded: %+v", params)
	}
	peer.mu.Lock()
	before := len(peer.calls)
	peer.mu.Unlock()
	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	peer.mu.Lock()
	callCount, closeCount := len(peer.calls), peer.closeCount
	peer.mu.Unlock()
	if hub.closeCount != 1 || callCount != before || closeCount != 0 {
		t.Fatalf("shutdown touched remote work: hub closes=%d calls=%d peer closes=%d", hub.closeCount, callCount-before, closeCount)
	}
}
