package remote

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type sshConnectResult struct {
	session SSHSession
	err     error
}

func connectWithHostKeyDecision(t *testing.T, h *testHub, command string, option HostKeyOption) (SSHSession, error) {
	t.Helper()
	resultCh := make(chan sshConnectResult, 1)
	go func() {
		session, err := h.hub.ConnectSSH(context.Background(), SSHConnectRequest{Command: command, Password: "secret", CLIVersion: "1.2.3"})
		resultCh <- sshConnectResult{session: session, err: err}
	}()
	request := h.waitEvent(t, HubEventHostKeyRequested).HostKey
	if request == nil {
		t.Fatal("missing host key request")
	}
	if err := h.hub.DecideHostKey(HostKeyDecision{RequestID: request.RequestID, Option: "accept"}); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("invalid host key option error = %v", err)
	}
	_ = h.hub.DecideHostKey(HostKeyDecision{RequestID: request.RequestID, Option: option})
	select {
	case result := <-resultCh:
		return result.session, result.err
	case <-time.After(5 * time.Second):
		t.Fatal("SSH connection did not resolve")
		return SSHSession{}, nil
	}
}

func TestSSHHostKeySaveDeployAndReverseBridge(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	server := startMockSSHServer(t)
	h := startTestHub(t)
	h.hub.cfg.KnownHostsPath = filepath.Join(t.TempDir(), "known_hosts")
	command := fmt.Sprintf("ssh -p %d root@%s", listenerPort(server.addr()), "127.0.0.1")
	session, err := connectWithHostKeyDecision(t, h, command, HostKeyTrustAndSave)
	if err != nil {
		t.Fatal(err)
	}
	if session.NodeID == "" || session.RemoteTunnelPort == 0 {
		t.Fatalf("SSH session = %+v", session)
	}
	knownHosts, err := os.ReadFile(h.hub.cfg.KnownHostsPath)
	if err != nil || !strings.Contains(string(knownHosts), "127.0.0.1") {
		t.Fatalf("known_hosts = %q, err = %v", knownHosts, err)
	}
	files := server.credentials()
	for _, name := range []string{"ca.pem", "client.pem", "client.key", "agent_config.json"} {
		if len(files[name]) == 0 {
			t.Fatalf("remote credential %s was not deployed", name)
		}
	}
	log := strings.Join(server.commandLog(), "\n")
	if !strings.Contains(log, "authorized_keys") || !strings.Contains(log, "agent run --state-dir ") || !strings.Contains(log, "--hub 127.0.0.1:") {
		t.Fatalf("deployment log = %s", log)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(files["ca.pem"]) {
		t.Fatal("invalid deployed CA")
	}
	certificate, err := tls.X509KeyPair(files["client.pem"], files["client.key"])
	if err != nil {
		t.Fatal(err)
	}
	channel, err := server.openForwardChannel()
	if err != nil {
		t.Fatal(err)
	}
	agentConn := tls.Client(&sshChannelConn{Channel: channel}, &tls.Config{
		MinVersion: tls.VersionTLS13, RootCAs: pool, ServerName: "localhost", Certificates: []tls.Certificate{certificate},
	})
	if err := agentConn.Handshake(); err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(agentConn, HandshakeControlPrefix+"\n")
	_ = writeJSONLine(agentConn, wireFrame{Event: EventHello, Data: []byte(`{"hostname":"cloud-gpu","platform":"linux/amd64"}`)})
	connected := h.waitEvent(t, HubEventPeerConnected)
	if connected.NodeID != session.NodeID || connected.Peer.Transport != "ssh_reverse" {
		t.Fatalf("reverse peer = %+v", connected.Peer)
	}
	_ = writeJSONLine(agentConn, wireFrame{Event: EventDeviceUpdated, Data: []byte(`{"gpu":"mock"}`)})
	event := h.waitEvent(t, HubEventPeerEvent)
	if event.NodeID != session.NodeID || event.EventName != EventDeviceUpdated {
		t.Fatalf("peer event = %+v", event)
	}
	_ = agentConn.Close()
	if err := h.hub.DisconnectSSH(session.NodeID); err != nil {
		t.Fatal(err)
	}
}

func TestSSHHostKeyTrustOnceAndReject(t *testing.T) {
	for _, option := range []HostKeyOption{HostKeyTrustOnce, HostKeyReject} {
		t.Run(string(option), func(t *testing.T) {
			t.Setenv("SSH_AUTH_SOCK", "")
			server := startMockSSHServer(t)
			h := startTestHub(t)
			h.hub.cfg.DisableAutoStart = true
			h.hub.cfg.KnownHostsPath = filepath.Join(t.TempDir(), "known_hosts")
			command := fmt.Sprintf("ssh -p %d root@127.0.0.1", listenerPort(server.addr()))
			_, err := connectWithHostKeyDecision(t, h, command, option)
			if option == HostKeyReject {
				if !errors.Is(err, ErrHostKeyRejected) {
					t.Fatalf("reject error = %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if _, statErr := os.Stat(h.hub.cfg.KnownHostsPath); !os.IsNotExist(statErr) {
				t.Fatalf("known_hosts unexpectedly written: %v", statErr)
			}
		})
	}
}
