package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type sshBridgeSession struct {
	hub        *hub
	info       SSHSession
	client     *ssh.Client
	jumpClient *ssh.Client
	listener   net.Listener
	done       chan struct{}
	closeOnce  sync.Once
	wg         sync.WaitGroup
}

func (h *hub) ConnectSSH(ctx context.Context, req SSHConnectRequest) (SSHSession, error) {
	parsed, err := ParseSSHCommand(req.Command)
	if err != nil {
		return SSHSession{}, err
	}
	nodeID := sshNodeID(parsed)
	password := []byte(req.Password)
	defer clear(password)
	hostKeyCallback, err := h.hostKeyCallback(parsed)
	if err != nil {
		return SSHSession{}, err
	}
	authMethods, cleanupAuth, err := h.sshAuthMethods(parsed, password, nodeID)
	if err != nil {
		return SSHSession{}, err
	}
	defer cleanupAuth()
	config := &ssh.ClientConfig{
		User: parsed.User, Auth: authMethods, HostKeyCallback: hostKeyCallback,
		Timeout: 20 * time.Second,
	}
	conn, jumpClient, err := h.dialSSH(ctx, parsed, config, password)
	if err != nil {
		return SSHSession{}, err
	}
	stopCancellation := context.AfterFunc(ctx, func() { _ = conn.Close() })
	clientConn, channels, requests, err := ssh.NewClientConn(conn, sshAddress(parsed.Host, parsed.Port), config)
	stopCancellation()
	if err != nil {
		_ = conn.Close()
		if jumpClient != nil {
			_ = jumpClient.Close()
		}
		return SSHSession{}, fmt.Errorf("SSH handshake: %w", err)
	}
	client := ssh.NewClient(clientConn, channels, requests)
	failed := true
	defer func() {
		if failed {
			_ = client.Close()
			if jumpClient != nil {
				_ = jumpClient.Close()
			}
		}
	}()
	if len(password) > 0 {
		if err := h.installManagedSSHKey(client, nodeID); err != nil {
			return SSHSession{}, fmt.Errorf("install SSH public key: %w", err)
		}
	}
	issued, err := h.ca.issueNode(nodeID)
	if err != nil {
		return SSHSession{}, err
	}
	version := firstNonEmpty(req.CLIVersion, h.cfg.CLIVersion, "latest")
	source := firstNonEmpty(req.DownloadSource, h.cfg.DownloadSource)
	proxy := firstNonEmpty(req.DownloadProxy, h.cfg.DownloadProxy)
	if err := deployRemoteAgent(client, issued, version, source, proxy); err != nil {
		return SSHSession{}, err
	}
	listener, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return SSHSession{}, fmt.Errorf("open SSH reverse forward: %w", err)
	}
	session := &sshBridgeSession{
		hub: h, client: client, jumpClient: jumpClient, listener: listener, done: make(chan struct{}),
		info: SSHSession{
			NodeID: nodeID, Host: parsed.Host, Port: parsed.Port, User: parsed.User,
			RemoteTunnelPort: listener.Addr().(*net.TCPAddr).Port, ConnectedAt: time.Now(),
		},
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = listener.Close()
		return SSHSession{}, ErrHostKeyRejected
	}
	previous := h.sshSessions[nodeID]
	h.sshSessions[nodeID] = session
	h.mu.Unlock()
	if previous != nil {
		_ = previous.close()
	}
	session.wg.Add(1)
	go session.acceptLoop()
	if !h.cfg.DisableAutoStart {
		if err := startRemoteAgent(client, nodeID, session.info.RemoteTunnelPort, version); err != nil {
			h.mu.Lock()
			if h.sshSessions[nodeID] == session {
				delete(h.sshSessions, nodeID)
			}
			h.mu.Unlock()
			_ = session.close()
			return SSHSession{}, err
		}
	}
	failed = false
	return session.info, nil
}

func (h *hub) dialSSH(ctx context.Context, parsed ParsedSSHCommand, config *ssh.ClientConfig, password []byte) (net.Conn, *ssh.Client, error) {
	dialer := net.Dialer{Timeout: 20 * time.Second}
	if parsed.ProxyJump == "" {
		conn, err := dialer.DialContext(ctx, "tcp", sshAddress(parsed.Host, parsed.Port))
		return conn, nil, err
	}
	jump, err := ParseSSHCommand("ssh " + parsed.ProxyJump)
	if err != nil {
		return nil, nil, ErrInvalidSSHCommand
	}
	jumpHostKey, err := h.hostKeyCallback(jump)
	if err != nil {
		return nil, nil, err
	}
	jumpAuth, cleanup, err := h.sshAuthMethods(jump, password, sshNodeID(jump))
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()
	jumpConfig := &ssh.ClientConfig{User: jump.User, Auth: jumpAuth, HostKeyCallback: jumpHostKey, Timeout: 20 * time.Second}
	jumpConn, err := dialer.DialContext(ctx, "tcp", sshAddress(jump.Host, jump.Port))
	if err != nil {
		return nil, nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = jumpConn.Close() })
	conn, channels, requests, err := ssh.NewClientConn(jumpConn, sshAddress(jump.Host, jump.Port), jumpConfig)
	stop()
	if err != nil {
		_ = jumpConn.Close()
		return nil, nil, err
	}
	jumpClient := ssh.NewClient(conn, channels, requests)
	targetConn, err := jumpClient.DialContext(ctx, "tcp", sshAddress(parsed.Host, parsed.Port))
	if err != nil {
		_ = jumpClient.Close()
		return nil, nil, err
	}
	return targetConn, jumpClient, nil
}

func (h *hub) sshAuthMethods(parsed ParsedSSHCommand, password []byte, nodeID string) ([]ssh.AuthMethod, func(), error) {
	var methods []ssh.AuthMethod
	var closers []io.Closer
	cleanup := func() {
		for _, closer := range closers {
			_ = closer.Close()
		}
	}
	if socket := os.Getenv("SSH_AUTH_SOCK"); socket != "" {
		conn, err := net.DialTimeout("unix", socket, 2*time.Second)
		if err == nil {
			agentClient := agent.NewClient(conn)
			closers = append(closers, conn)
			methods = append(methods, ssh.PublicKeysCallback(agentClient.Signers))
		}
	}
	identityFiles := []string{parsed.IdentityFile}
	if parsed.IdentityFile == "" {
		home, _ := os.UserHomeDir()
		identityFiles = []string{
			filepath.Join(h.cfg.DataDir, "ssh", nodeID, "id_ed25519"),
			filepath.Join(home, ".ssh", "id_ed25519"), filepath.Join(home, ".ssh", "id_ecdsa"),
			filepath.Join(home, ".ssh", "id_rsa"),
		}
	}
	var signers []ssh.Signer
	for _, identityFile := range identityFiles {
		if identityFile == "" {
			continue
		}
		data, err := os.ReadFile(identityFile)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err == nil {
			signers = append(signers, signer)
		}
	}
	if len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}
	if len(password) > 0 {
		methods = append(methods, ssh.Password(string(password)))
	}
	if len(methods) == 0 {
		cleanup()
		return nil, func() {}, fmt.Errorf("no SSH authentication method available")
	}
	return methods, cleanup, nil
}

func (h *hub) DisconnectSSH(nodeID string) error {
	h.mu.Lock()
	session := h.sshSessions[nodeID]
	if session != nil {
		delete(h.sshSessions, nodeID)
	}
	h.mu.Unlock()
	if session == nil {
		return ErrPeerOffline
	}
	return session.close()
}

func (s *sshBridgeSession) acceptLoop() {
	defer s.wg.Done()
	for {
		remote, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.forward(remote)
		}()
	}
}

func (s *sshBridgeSession) forward(remote net.Conn) {
	defer remote.Close()
	port := listenerPort(s.hub.Addr())
	if port == 0 {
		return
	}
	local, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 10*time.Second)
	if err != nil {
		return
	}
	defer local.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(local, remote); done <- struct{}{} }()
	go func() { _, _ = io.Copy(remote, local); done <- struct{}{} }()
	<-done
	_ = local.Close()
	_ = remote.Close()
	<-done
}

func (s *sshBridgeSession) close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		_ = s.client.Close()
		if s.jumpClient != nil {
			_ = s.jumpClient.Close()
		}
		_ = s.listener.Close()
	})
	s.wg.Wait()
	return nil
}

func sshNodeID(parsed ParsedSSHCommand) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s@%s:%d", parsed.User, parsed.Host, parsed.Port)))
	return "node-" + hex.EncodeToString(digest[:8])
}
