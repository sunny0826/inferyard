package core

import (
	"context"
	"inferyard/studio/internal/remote"
	"path/filepath"
	"testing"
	"time"
)

func TestNodeDownloadSettingsDefaultsAndOverrides(t *testing.T) {
	hub := newMockRemoteHub()
	hub.sshSession = remote.SSHSession{NodeID: "node-ssh", Host: "example.test", ConnectedAt: time.Now().UTC()}
	svc, _ := newNodeTestService(t, hub)
	proxy := "http://127.0.0.1:7890"
	svc.mu.Lock()
	svc.settings.DownloadSource = " HuggingFace "
	svc.settings.DownloadProxy = &proxy
	svc.mu.Unlock()

	config := defaultNodeHubConfig(svc, filepath.Dir(svc.settingsPath))
	if config.ListenAddr != "" || config.DownloadSource != DownloadSourceHuggingFace || config.DownloadProxy != proxy {
		t.Fatalf("unexpected hub defaults: %+v", config)
	}
	if _, apiErr := svc.StartNodePairing(context.Background(), remote.PairingSessionOptions{}); apiErr != nil {
		t.Fatal(apiErr)
	}
	if hub.pairingOptions.DownloadSource != DownloadSourceHuggingFace || hub.pairingOptions.DownloadProxy != proxy {
		t.Fatalf("pairing settings not forwarded: %+v", hub.pairingOptions)
	}
	if _, apiErr := svc.ConnectSSHNode(context.Background(), remote.SSHConnectRequest{Command: "ssh gpu@example.test"}); apiErr != nil {
		t.Fatal(apiErr)
	}
	if hub.sshRequest.DownloadSource != DownloadSourceHuggingFace || hub.sshRequest.DownloadProxy != proxy {
		t.Fatalf("SSH settings not forwarded: %+v", hub.sshRequest)
	}

	explicit := remote.PairingSessionOptions{DownloadSource: DownloadSourceModelScope, DownloadProxy: "http://explicit:8080"}
	if _, apiErr := svc.StartNodePairing(context.Background(), explicit); apiErr != nil {
		t.Fatal(apiErr)
	}
	if hub.pairingOptions.DownloadSource != explicit.DownloadSource || hub.pairingOptions.DownloadProxy != explicit.DownloadProxy {
		t.Fatalf("explicit pairing settings overwritten: %+v", hub.pairingOptions)
	}
}
