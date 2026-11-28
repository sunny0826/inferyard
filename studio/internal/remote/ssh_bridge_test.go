package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
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
	fragments := []string{
		"authorized_keys", "agent run --state-dir ", "--hub 127.0.0.1:",
		"agent.pid", "kill -TERM", "agent_pid=$!", "chmod 600",
	}
	for _, fragment := range fragments {
		if !strings.Contains(log, fragment) {
			t.Fatalf("deployment log missing %q: %s", fragment, log)
		}
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

func TestProxyJumpDoesNotReceiveTargetPassword(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	server := startMockSSHServer(t)
	var passwordAttempts []string
	server.config.PasswordCallback = func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		server.mu.Lock()
		passwordAttempts = append(passwordAttempts, string(password))
		server.mu.Unlock()
		return nil, fmt.Errorf("password authentication disabled")
	}
	server.config.PublicKeyCallback = func(_ ssh.ConnMetadata, _ ssh.PublicKey) (*ssh.Permissions, error) {
		return nil, fmt.Errorf("test key rejected")
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "jump_key")
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	h := startTestHub(t)
	parsed := ParsedSSHCommand{
		User: "root", Host: "target.example.com", Port: 22,
		ProxyJump: fmt.Sprintf("-i %s -p %d root@127.0.0.1", keyPath, listenerPort(server.addr())),
	}
	result := make(chan error, 1)
	go func() {
		_, _, err := h.hub.dialSSH(context.Background(), parsed, &ssh.ClientConfig{}, []byte("target-secret"))
		result <- err
	}()
	request := h.waitEvent(t, HubEventHostKeyRequested).HostKey
	if request == nil {
		t.Fatal("missing jump host key request")
	}
	if err := h.hub.DecideHostKey(HostKeyDecision{RequestID: request.RequestID, Option: HostKeyTrustOnce}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("jump authentication unexpectedly succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("jump authentication did not resolve")
	}
	server.mu.Lock()
	attempts := append([]string(nil), passwordAttempts...)
	server.mu.Unlock()
	if len(attempts) != 0 {
		t.Fatalf("jump received target password attempts: %q", attempts)
	}
}
