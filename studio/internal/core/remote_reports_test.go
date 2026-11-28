package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/remote"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func makeRemoteReportRaw(t *testing.T, runID, model string, tps float64) json.RawMessage {
	t.Helper()
	run := makeReportRunFixture(runID, model, `C:\Engines\llama-server.exe`, "llama.cpp", &tps)
	run["source"] = map[string]any{"run_id": runID, "path": `C:\Runs\` + runID}
	raw, err := json.Marshal(map[string]any{"schema_version": 3, "kind": "studio-report.v1", "runs": []any{run}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func setRemoteReportResponse(peer *mockNodePeer, raw json.RawMessage) {
	peer.responses[remote.MethodReportExport] = map[string]any{
		"report": raw, "verified_from_raw": true,
	}
}

func setRemoteReportMemory(t *testing.T, raw json.RawMessage, value any) json.RawMessage {
	t.Helper()
	var document map[string]any
	if json.Unmarshal(raw, &document) != nil {
		t.Fatal("invalid report fixture")
	}
	run := document["runs"].([]any)[0].(map[string]any)
	dashboard := run["dashboard"].(map[string]any)
	dashboard["sampled_peak_rss_bytes"] = value
	if value == nil {
		dashboard["sampled_peak_rss_reason"] = "no_samples"
	}
	updated, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func TestRemoteReportsIsolationComparisonAndOfflineRouting(t *testing.T) {
	hub := newMockRemoteHub()
	peerA, peerB := newMockNodePeer("node-a", "windows/amd64"), newMockNodePeer("node-b", "windows/amd64")
	hub.peers[peerA.info.NodeID], hub.peers[peerB.info.NodeID] = peerA, peerB
	svc, workspace := newNodeTestService(t, hub)
	model := `C:\Models\Qwen3.gguf`
	rawA := makeRemoteReportRaw(t, "run-a", model, 40)
	rawB := makeRemoteReportRaw(t, "run-b", model, 80)
	setRemoteReportResponse(peerA, rawA)
	setRemoteReportResponse(peerB, rawB)
	if _, apiErr := svc.SyncNodeReport(context.Background(), "node-a", "report-shared"); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, apiErr := svc.SyncNodeReport(context.Background(), "node-b", "report-shared"); apiErr != nil {
		t.Fatal(apiErr)
	}
	rawA = setRemoteReportMemory(t, makeRemoteReportRaw(t, "run-a", model, 60), nil)
	setRemoteReportResponse(peerA, rawA)
	summaryA, apiErr := svc.SyncNodeReport(context.Background(), "node-a", "report-shared")
	if apiErr != nil || !summaryA.VerifiedFromRaw || !summaryA.SourceOnline || summaryA.CanResume || !summaryA.CanRerun {
		t.Fatalf("single-run imported summary must allow rerun only: %+v err=%v", summaryA, apiErr)
	}
	if _, apiErr := svc.ResumeRemoteReport(context.Background(), "node-a", "report-shared"); apiErr == nil || apiErr.Details["reason"] != "not_resumable" {
		t.Fatalf("single-run report must reject resume: %+v", apiErr)
	}
	peerA.responses[remote.MethodReportExport] = map[string]any{
		"report":               rawA,
		"verified_from_raw":    true,
		"resumable_run_paths":  []string{`C:\Runs\batch\runs\run-a`},
		"rerunnable_run_paths": []string{`C:\Runs\run-a`},
	}
	summaryA, apiErr = svc.SyncNodeReport(context.Background(), "node-a", "report-shared")
	if apiErr != nil || !summaryA.CanResume || !summaryA.CanRerun {
		t.Fatalf("batch imported summary should allow resume: %+v err=%v", summaryA, apiErr)
	}
	for _, nodeID := range []string{"node-a", "node-b"} {
		path := filepath.Join(workspace, "remote_reports", nodeID, "report-shared", "studio.json")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing isolated report %s: %v", path, err)
		}
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil || len(entries) != 2 {
			t.Fatalf("sync is not idempotent for %s: entries=%v err=%v", nodeID, entries, err)
		}
	}
	meta, apiErr := svc.RemoteReportMetadata("node-a", "report-shared")
	if apiErr != nil || len(meta.RemoteRunPaths) != 1 || meta.RemoteRunPaths[0] != `C:\Runs\run-a` {
		t.Fatalf("opaque report path changed: %+v err=%v", meta, apiErr)
	}
	if _, apiErr := svc.SyncNodeReport(context.Background(), "node-a", "../escape"); apiErr == nil || apiErr.Code != "invalid_request" {
		t.Fatalf("path traversal accepted: %v", apiErr)
	}
	if _, apiErr := svc.RemoteReportIndex("node-a", "../report-shared"); apiErr == nil || apiErr.Code != "invalid_request" {
		t.Fatalf("index path traversal accepted: %v", apiErr)
	}

	localTPS := 999.0
	writeStudioReportFixture(t, filepath.Join(workspace, "reports"), "local-report", time.Now(), []map[string]any{
		makeReportRunFixture("run-local", "Qwen3.gguf", "/opt/llama-server", "llama.cpp", &localTPS),
	})
	all, apiErr := svc.AllReports()
	if apiErr != nil || len(all) != 3 {
		t.Fatalf("unified reports mismatch: reports=%+v err=%v", all, apiErr)
	}
	if best, ok := svc.BestSpeedForNode("node-a", model, "llama.cpp"); !ok || best.TokensPerSecond != 60 || best.TTFTMs != nil {
		t.Fatalf("node A evidence polluted or TTFT fabricated: best=%+v ok=%v", best, ok)
	}
	if best, ok := svc.BestSpeedForNode("node-b", model, "llama.cpp"); !ok || best.TokensPerSecond != 80 {
		t.Fatalf("node B evidence polluted: best=%+v ok=%v", best, ok)
	}
	if best, ok := svc.BestSpeedForNode(LocalNodeID, "Qwen3.gguf", "llama.cpp"); !ok || best.TokensPerSecond != 999 {
		t.Fatalf("local evidence polluted: best=%+v ok=%v", best, ok)
	}
	if best, ok := svc.BestSpeedFor("Qwen3.gguf", "llama.cpp"); !ok || best.TokensPerSecond != 999 {
		t.Fatalf("legacy local evidence changed: best=%+v ok=%v", best, ok)
	}

	comparison, apiErr := svc.CompareReports(summaryA.ReportRef, RemoteReportReference("node-b", "report-shared"))
	if apiErr != nil || len(comparison.Runs) != 1 || len(comparison.Cases) != 1 {
		t.Fatalf("comparison failed: %+v err=%v", comparison, apiErr)
	}
	pair := comparison.Runs[0]
	if pair.SpeedupRatio == nil || *pair.SpeedupRatio < 1.333 || *pair.SpeedupRatio > 1.334 {
		t.Fatalf("wrong speedup ratio: %+v", pair.SpeedupRatio)
	}
	if pair.Left.PeakMemoryBytes != nil || pair.Left.TTFTP95MS != nil || pair.Left.PeakWorkingSetBytes != nil || pair.Left.PeakTemperatureCelsius != nil {
		t.Fatalf("missing metrics were zero-filled: %+v", pair.Left)
	}
	if pair.Right.PeakMemoryBytes == nil || comparison.Cases[0].Left == nil || comparison.Cases[0].Right == nil || comparison.Cases[0].CaseID != "c1" {
		t.Fatalf("comparison data not aligned: %+v %+v", pair.Right, comparison.Cases[0])
	}

	peerA.responses[remote.MethodJobStart] = map[string]any{"job": map[string]any{"task_id": "job-a", "state": "starting"}}
	if _, apiErr := svc.ResumeRemoteReport(context.Background(), "node-a", "report-shared"); apiErr != nil {
		t.Fatal(apiErr)
	}
	calls := peerA.methodCalls(remote.MethodJobStart)
	var params map[string]any
	if json.Unmarshal(calls[len(calls)-1].Params, &params) != nil || params["action"] != "resume" {
		t.Fatalf("resume did not route to source node: %s", calls[len(calls)-1].Params)
	}
	if params["from_run"] != `C:\Runs\batch\runs\run-a` {
		t.Fatalf("resume did not submit single resumable batch run path: %+v", params)
	}
	if _, apiErr := svc.RerunRemoteReport(context.Background(), "node-a", "report-shared"); apiErr != nil {
		t.Fatal(apiErr)
	}
	calls = peerA.methodCalls(remote.MethodJobStart)
	if json.Unmarshal(calls[len(calls)-1].Params, &params) != nil || params["action"] != "rerun" {
		t.Fatalf("rerun did not route to source node: %s", calls[len(calls)-1].Params)
	}
	paths := params["remote_run_paths"].([]any)
	if paths[0] != `C:\Runs\run-a` {
		t.Fatalf("rerun changed opaque path: %+v", paths)
	}
	peerA.setConnected(false)
	svc.nodes.peerDisconnected("node-a")
	all, apiErr = svc.AllReports()
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	for _, summary := range all {
		if summary.NodeID == "node-a" && (summary.SourceOnline || summary.CanResume || summary.CanRerun) {
			t.Fatalf("offline report remains runnable: %+v", summary)
		}
	}
	if _, apiErr := svc.ResumeRemoteReport(context.Background(), "node-a", "report-shared"); apiErr == nil || apiErr.Code != "node_offline" {
		t.Fatalf("offline resume accepted: %v", apiErr)
	}
	if _, apiErr := svc.RerunRemoteReport(context.Background(), "node-a", "report-shared"); apiErr == nil || apiErr.Code != "node_offline" {
		t.Fatalf("offline rerun accepted: %v", apiErr)
	}
}

func TestRemoteAndLocalCleanupAreIndependent(t *testing.T) {
	hub := newMockRemoteHub()
	peerA, peerB := newMockNodePeer("node-a", "linux/amd64"), newMockNodePeer("node-b", "linux/amd64")
	hub.peers[peerA.info.NodeID], hub.peers[peerB.info.NodeID] = peerA, peerB
	svc, workspace := newNodeTestService(t, hub)
	initTestWorkspaceMarker(t, workspace)
	for _, peer := range []*mockNodePeer{peerA, peerB} {
		setRemoteReportResponse(peer, makeRemoteReportRaw(t, "run-"+peer.info.NodeID, "Qwen3.gguf", 50))
		if _, apiErr := svc.SyncNodeReport(context.Background(), peer.info.NodeID, "shared"); apiErr != nil {
			t.Fatal(apiErr)
		}
	}
	tps := 25.0
	writeStudioReportFixture(t, filepath.Join(workspace, "reports"), "local", time.Now(), []map[string]any{
		makeReportRunFixture("run-local", "Qwen3.gguf", "/opt/llama-server", "llama.cpp", &tps),
	})
	if _, apiErr := svc.CleanWorkspaceArtifacts(); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, err := os.Stat(filepath.Join(workspace, "reports", "local")); !os.IsNotExist(err) {
		t.Fatalf("local report cache survived cleanup: %v", err)
	}
	for _, nodeID := range []string{"node-a", "node-b"} {
		if _, err := os.Stat(filepath.Join(workspace, "remote_reports", nodeID, "shared", "studio.json")); err != nil {
			t.Fatalf("local cleanup deleted remote cache: %v", err)
		}
	}
	if _, apiErr := svc.CleanRemoteReportCache("node-a"); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, err := os.Stat(filepath.Join(workspace, "remote_reports", "node-a")); !os.IsNotExist(err) {
		t.Fatalf("node A cache survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "remote_reports", "node-b", "shared", "studio.json")); err != nil {
		t.Fatalf("node B cache was deleted with node A: %v", err)
	}
	if _, apiErr := svc.CleanRemoteReportCache(); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, err := os.Stat(filepath.Join(workspace, "remote_reports")); !os.IsNotExist(err) {
		t.Fatalf("remote cache root survived full cleanup: %v", err)
	}
}
