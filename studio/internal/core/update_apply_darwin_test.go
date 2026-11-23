//go:build darwin

package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func setupFakeDarwinApp(t *testing.T, root, name, versionText string) (appDir, exePath string) {
	t.Helper()
	appDir = filepath.Join(root, name)
	exePath = filepath.Join(appDir, "Contents", "MacOS", "inferyard-studio")
	if err := os.MkdirAll(filepath.Dir(exePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exePath, []byte(versionText), 0o755); err != nil {
		t.Fatal(err)
	}
	return appDir, exePath
}

func TestApplyDarwinUpdateSwapAndRollbackBeforeShutdown(t *testing.T) {
	tmp := t.TempDir()
	installParent := filepath.Join(tmp, "apps")
	stagingDir := filepath.Join(tmp, "staging")
	if err := os.MkdirAll(installParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	appDir, exePath := setupFakeDarwinApp(t, installParent, "InferYard Studio.app", "old-version")

	// Mock DMG runner creates mounted app on hdiutil attach and copies it on ditto.
	var cmds []string
	var shutdownCalls, quitCalls int
	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		cmds = append(cmds, name+" "+args[0])
		switch name {
		case "hdiutil":
			if args[0] == "attach" {
				mountPoint := args[4]
				_, _ = setupFakeDarwinApp(t, mountPoint, "InferYard Studio.app", "new-version")
			}
		case "ditto":
			srcApp, dstApp := args[0], args[1]
			_, _ = setupFakeDarwinApp(t, filepath.Dir(dstApp), filepath.Base(dstApp), "new-version")
			_ = srcApp
		}
		return nil, nil
	}

	// Case 1: Swap failure rolls back original .app and does NOT call Shutdown or RequestQuit.
	renameCount := 0
	planFail := platformApplyPlan{
		ArtifactPath: filepath.Join(stagingDir, "update.dmg"),
		StagingDir:   stagingDir,
		ExePath:      exePath,
		AppPath:      appDir,
		InstallDir:   installParent,
		SkipRelaunch: true,
		Shutdown:     func() error { shutdownCalls++; return nil },
		RequestQuit:  func() { quitCalls++ },
		RunCmd:       runner,
	}
	err := applyDarwinUpdate(context.Background(), planFail, func(oldPath, newPath string) error {
		renameCount++
		if renameCount == 2 {
			return errors.New("simulated swap error")
		}
		return os.Rename(oldPath, newPath)
	})
	if err == nil {
		t.Fatal("expected swap failure")
	}
	if shutdownCalls != 0 || quitCalls != 0 {
		t.Fatalf("failed swap must not shut down service or quit: shutdown=%d quit=%d", shutdownCalls, quitCalls)
	}
	gotExe, readErr := os.ReadFile(exePath)
	if readErr != nil || string(gotExe) != "old-version" {
		t.Fatalf("rollback failed: err=%v content=%q", readErr, string(gotExe))
	}

	// Case 2: Successful swap detaches DMG, replaces .app, cleans staging, then calls Shutdown & RequestQuit.
	cmds = nil
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := applyDarwinUpdate(context.Background(), planFail, os.Rename); err != nil {
		t.Fatalf("successful darwin apply failed: %v", err)
	}
	if shutdownCalls != 1 || quitCalls != 1 {
		t.Fatalf("expected 1 shutdown and 1 quit call, got shutdown=%d quit=%d", shutdownCalls, quitCalls)
	}
	gotExe, readErr = os.ReadFile(exePath)
	if readErr != nil || string(gotExe) != "new-version" {
		t.Fatalf("updated binary mismatch: err=%v content=%q", readErr, string(gotExe))
	}
	if _, statErr := os.Stat(stagingDir); !os.IsNotExist(statErr) {
		t.Fatalf("stagingDir not cleaned up: %v", statErr)
	}
	if !slices.Contains(cmds, "hdiutil attach") || !slices.Contains(cmds, "hdiutil detach") || !slices.Contains(cmds, "ditto "+filepath.Join(stagingDir, "mount", "InferYard Studio.app")) {
		t.Fatalf("unexpected macOS apply commands: %v", cmds)
	}
}
