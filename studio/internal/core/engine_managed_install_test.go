package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

func managedInstallTarget(t *testing.T) (EngineFamilyCatalog, EngineRelease, EnginePlatformBuild) {
	t.Helper()
	platform := runtime.GOOS + "/" + runtime.GOARCH
	for _, family := range DefaultEngineCatalog().Families() {
		for _, release := range family.ReleasesForPlatform(platform, false) {
			for _, build := range release.Builds {
				if build.CanManagedVenvInstallOn(platform) {
					return family, release, build
				}
			}
		}
	}
	t.Skip("no managed venv engine on this platform")
	return EngineFamilyCatalog{}, EngineRelease{}, EnginePlatformBuild{}
}

func managedInstallExecutor(t *testing.T) Executor {
	t.Helper()
	return func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		if len(args) >= 2 && args[0] == "engine" && args[1] == "init-installation" {
			var family, executable, outPath string
			for i := 0; i+1 < len(args); i++ {
				switch args[i] {
				case "--family":
					family = args[i+1]
				case "--executable":
					executable = args[i+1]
				case "--out":
					outPath = args[i+1]
				}
			}
			declaration := map[string]any{
				"schema_version": 3, "definition": "local-engine-installation.v1", "family": family,
				"executable": executable, "working_directory": filepath.Dir(outPath),
				"runtime_roots": []string{filepath.Dir(filepath.Dir(executable))}, "environment": map[string]string{},
			}
			raw, _ := json.Marshal(declaration)
			if err := os.WriteFile(outPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			payload, _ := json.Marshal(map[string]any{
				"command": "engine init-installation", "status": "prepared", "completeness": "complete", "limitations": []string{},
				"run_id": nil, "evidence_dir": nil,
				"details": map[string]any{
					"definition": "local-engine-init.v1", "out": outPath, "family": family,
					"installation": declaration, "verified": true, "model_requests_sent": 0,
				},
			})
			return inferyard.Execution{Stdout: payload}, nil
		}
		if len(args) >= 4 && args[0] == "engine" && args[1] == "check" && args[2] == "--installation" {
			raw, err := os.ReadFile(args[3])
			if err != nil {
				return inferyard.Execution{}, err
			}
			var declaration struct{ Family string }
			_ = json.Unmarshal(raw, &declaration)
			payload, _ := json.Marshal(map[string]any{
				"command": "engine check", "status": "checked", "completeness": "complete", "limitations": []string{},
				"run_id": nil, "evidence_dir": nil,
				"details": map[string]any{
					"definition": "local-engine-check.v2", "eligible": true, "reason": nil, "family": declaration.Family,
					"declaration_sha256": strings.Repeat("a", 64), "verification_scope": "installation_runtime_closure_only",
					"model_requests_sent": 0, "ready_to_run": false,
				},
			})
			return inferyard.Execution{Stdout: payload}, nil
		}
		return assetTestExec(context.Background(), "", args...)
	}
}

func stubManagedUV(t *testing.T, command func(context.Context, string, ...string) ([]byte, error)) {
	t.Helper()
	oldFind, oldDownload, oldCommand := managedEngineFindUV, managedEngineDownloadUV, managedEngineCommandOutput
	t.Cleanup(func() {
		managedEngineFindUV, managedEngineDownloadUV, managedEngineCommandOutput = oldFind, oldDownload, oldCommand
	})
	managedEngineFindUV = func(context.Context, string) (string, bool) { return "/fake/uv", false }
	managedEngineDownloadUV = func(context.Context, string) (string, *ApiError) {
		t.Fatal("UV download should not be needed")
		return "", nil
	}
	managedEngineCommandOutput = command
}

func createManagedPython(t *testing.T, venvDir string) {
	t.Helper()
	python := managedEnginePython(venvDir)
	mustWriteFile(t, python, []byte("python"), 0o755)
}

func TestCreateManagedEngineInstallOffline(t *testing.T) {
	family, release, build := managedInstallTarget(t)
	s := testService(t, managedInstallExecutor(t))
	workspace := t.TempDir()
	proxy := "http://127.0.0.1:7890"
	s.settings.WorkspaceRoot, s.settings.DownloadProxy = &workspace, &proxy
	var mu sync.Mutex
	var commands [][]string
	venvStarted := make(chan struct{})
	allowVenv := make(chan struct{})
	stubManagedUV(t, func(ctx context.Context, path string, args ...string) ([]byte, error) {
		mu.Lock()
		commands = append(commands, append([]string{path}, args...))
		mu.Unlock()
		if got := installProxyFromContext(ctx); got != proxy {
			t.Errorf("proxy = %q, want %q", got, proxy)
		}
		if len(args) > 0 && args[0] == "venv" {
			createManagedPython(t, args[len(args)-1])
			close(venvStarted)
			select {
			case <-allowVenv:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return nil, nil
	})
	req := ManagedEngineInstallRequest{Family: family.ID, ReleaseID: release.ID, BuildID: build.ID}
	task, failure := s.CreateManagedEngineInstall(context.Background(), req)
	if failure != nil {
		t.Fatalf("CreateManagedEngineInstall: %+v", failure)
	}
	<-venvStarted
	duplicate, failure := s.CreateManagedEngineInstall(context.Background(), req)
	if failure != nil || duplicate.TaskId != task.TaskId {
		t.Fatalf("duplicate = %+v, %+v; want %s", duplicate, failure, task.TaskId)
	}
	close(allowVenv)
	wait(t, func() bool {
		for _, current := range s.Downloads() {
			if current.TaskId == task.TaskId {
				return current.State == "done" || current.State == "failed" || current.State == "cancelled"
			}
		}
		return false
	})
	var completed DownloadTask
	for _, current := range s.Downloads() {
		if current.TaskId == task.TaskId {
			completed = current
		}
	}
	if completed.State != "done" || completed.InstallationPath == nil || completed.EnginePath == nil || completed.Stage != "" {
		t.Fatalf("incomplete task result: %+v; error code=%s message=%s", completed, completed.Error.Code, completed.Error.Message)
	}
	if !slices.Contains(s.userSettings().EngineInstallations, *completed.InstallationPath) {
		t.Fatalf("installation not registered: %+v", s.userSettings().EngineInstallations)
	}
	receipt, ok := readManagedEngineReceipt(task.OutDir)
	if !ok || receipt.Family != family.ID || receipt.Version != release.Version || receipt.InstallMode != managedEngineVenvInstallMode {
		t.Fatalf("unexpected receipt: %+v, %t", receipt, ok)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(commands) != 2 || commands[0][1] != "venv" || commands[1][1] != "pip" {
		t.Fatalf("unexpected UV commands: %+v", commands)
	}
}

func TestCreateManagedEngineInstallCancellation(t *testing.T) {
	family, release, build := managedInstallTarget(t)
	s := testService(t, managedInstallExecutor(t))
	workspace := t.TempDir()
	s.settings.WorkspaceRoot = &workspace
	pipStarted := make(chan struct{})
	blockPip := true
	stubManagedUV(t, func(ctx context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "venv" {
			createManagedPython(t, args[len(args)-1])
		}
		if len(args) > 0 && args[0] == "pip" && blockPip {
			close(pipStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return nil, nil
	})
	task, failure := s.CreateManagedEngineInstall(context.Background(), ManagedEngineInstallRequest{
		Family: family.ID, ReleaseID: release.ID, BuildID: build.ID,
	})
	if failure != nil {
		t.Fatal(failure)
	}
	<-pipStarted
	if _, failure = s.CancelDownload(task.TaskId); failure != nil {
		t.Fatal(failure)
	}
	wait(t, func() bool { return s.Downloads()[0].State == "cancelled" })
	if _, err := os.Stat(filepath.Join(task.OutDir, managedEngineReceiptName)); !os.IsNotExist(err) {
		t.Fatalf("cancelled install wrote receipt: %v", err)
	}
	blockPip = false
	retried, failure := s.RetryDownload(task.TaskId)
	if failure != nil {
		t.Fatalf("retry: %+v", failure)
	}
	wait(t, func() bool {
		for _, current := range s.Downloads() {
			if current.TaskId == retried.TaskId {
				return current.State == "done" || current.State == "failed"
			}
		}
		return false
	})
	for _, current := range s.Downloads() {
		if current.TaskId == retried.TaskId && current.State != "done" {
			t.Fatalf("managed retry entered wrong workflow: %+v", current)
		}
	}
	if _, ok := readManagedEngineReceipt(retried.OutDir); !ok {
		t.Fatalf("managed retry did not write receipt: %+v", retried)
	}
}

func TestManagedVenvRegistryInspectAndDelete(t *testing.T) {
	_, workspace := isolatedHomeAndBase(t)
	s := testService(t, assetTestExec)
	s.settings.WorkspaceRoot = &workspace
	installDir := filepath.Join(workspace, "engines", "mlx-lm-0.32.2")
	installationPath := filepath.Join(installDir, "installation.json")
	createManagedPython(t, filepath.Join(installDir, "venv"))
	declaration := []byte(`{"schema_version":3,"definition":"local-engine-installation.v1","family":"mlx-lm","executable":"python","environment":{}}`)
	mustWriteFile(t, installationPath, declaration, 0o600)
	if failure := writeManagedEngineReceipt(installDir, managedEngineReceipt{
		Definition: managedEngineDefinition, Family: "mlx-lm", Version: "0.32.2", InstallMode: managedEngineVenvInstallMode,
	}); failure != nil {
		t.Fatal(failure)
	}
	externalPath := filepath.Join(workspace, "external", "installation.json")
	mustWriteFile(t, externalPath, declaration, 0o600)
	discoveredDir := filepath.Join(workspace, "engines", "mlx-lm-discovered")
	discoveredPath := filepath.Join(discoveredDir, "installation.json")
	mustWriteFile(t, discoveredPath, declaration, 0o600)
	if failure := writeManagedEngineReceipt(discoveredDir, managedEngineReceipt{
		Definition: managedEngineDefinition, Family: "mlx-lm", Version: "0.32.2", InstallMode: managedEngineVenvInstallMode,
	}); failure != nil {
		t.Fatal(failure)
	}
	s.settings.EngineInstallations = []string{installationPath, externalPath}

	var managed *EngineAsset
	discovered := false
	for _, eng := range s.Registry().Engines {
		if eng.Path == discoveredPath && eng.Managed && eng.Deletable {
			discovered = true
		}
		if eng.Path == installationPath {
			managed = &eng
		}
		if eng.Path == externalPath && (eng.Managed || eng.Deletable || eng.InstallMode != "external") {
			t.Fatalf("external engine marked managed: %+v", eng)
		}
	}
	if !discovered {
		t.Fatal("unregistered managed installation was not discovered")
	}
	if managed == nil || !managed.Managed || !managed.Deletable || managed.InstallMode != managedEngineVenvInstallMode || managed.Version != "0.32.2" {
		t.Fatalf("managed registry entry = %+v", managed)
	}
	info, failure := s.InspectDeleteAsset("engine", managed.Name)
	if failure != nil || info.Scope != "managed_venv_engine" || info.TargetPath != installDir || info.ReclaimableBytes == nil {
		t.Fatalf("inspect = %+v, %+v", info, failure)
	}
	if failure := s.DeleteManagedAssetVerified("engine", managed.Name, info.TargetPath, info.Fingerprint); failure != nil {
		t.Fatalf("delete: %+v", failure)
	}
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Fatalf("managed install still exists: %v", err)
	}
	remaining := s.userSettings().EngineInstallations
	if slices.Contains(remaining, installationPath) || !slices.Contains(remaining, externalPath) {
		t.Fatalf("unexpected registrations after delete: %+v", remaining)
	}
}

func TestCreateManagedEngineInstallRejectsUnknownBuild(t *testing.T) {
	s := testService(t, assetTestExec)
	_, failure := s.CreateManagedEngineInstall(context.Background(), ManagedEngineInstallRequest{
		Family: "llama-cpp", ReleaseID: "unknown", BuildID: "unknown",
	})
	if failure == nil || failure.Code != "invalid_request" {
		t.Fatalf("expected invalid_request, got %+v", failure)
	}
}
