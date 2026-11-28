package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type mockSSHServer struct {
	t        *testing.T
	listener net.Listener
	config   *ssh.ServerConfig

	mu          sync.Mutex
	conn        *ssh.ServerConn
	forwardPort uint32
	commands    []string
	files       map[string][]byte
}

func startMockSSHServer(t *testing.T) *mockSSHServer {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if conn.User() == "root" && string(password) == "secret" {
			return nil, nil
		}
		return nil, fmt.Errorf("invalid credentials")
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &mockSSHServer{t: t, listener: listener, config: config, files: make(map[string][]byte)}
	go server.acceptLoop()
	t.Cleanup(server.close)
	return server
}

func (s *mockSSHServer) addr() string { return s.listener.Addr().String() }

func (s *mockSSHServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *mockSSHServer) handleConn(netConn net.Conn) {
	serverConn, channels, requests, err := ssh.NewServerConn(netConn, s.config)
	if err != nil {
		_ = netConn.Close()
		return
	}
	s.mu.Lock()
	s.conn = serverConn
	s.mu.Unlock()
	go s.handleGlobalRequests(requests)
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go s.handleSession(channel, channelRequests)
	}
}

func (s *mockSSHServer) handleGlobalRequests(requests <-chan *ssh.Request) {
	for request := range requests {
		if request.Type == "cancel-tcpip-forward" {
			_ = request.Reply(true, nil)
			continue
		}
		if request.Type != "tcpip-forward" {
			_ = request.Reply(false, nil)
			continue
		}
		var payload struct {
			BindAddr string
			BindPort uint32
		}
		if err := ssh.Unmarshal(request.Payload, &payload); err != nil {
			_ = request.Reply(false, nil)
			continue
		}
		port := payload.BindPort
		if port == 0 {
			port = 41000 + uint32(time.Now().UnixNano()%10000)
		}
		s.mu.Lock()
		s.forwardPort = port
		s.mu.Unlock()
		_ = request.Reply(true, ssh.Marshal(struct{ Port uint32 }{port}))
	}
}

func (s *mockSSHServer) handleSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	for request := range requests {
		if request.Type != "exec" {
			_ = request.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		if err := ssh.Unmarshal(request.Payload, &payload); err != nil {
			_ = request.Reply(false, nil)
			continue
		}
		_ = request.Reply(true, nil)
		var input []byte
		if strings.Contains(payload.Command, "cat >") {
			input, _ = io.ReadAll(channel)
		}
		s.mu.Lock()
		s.commands = append(s.commands, payload.Command)
		for _, name := range []string{"ca.pem", "client.pem", "client-key.pem"} {
			if strings.Contains(payload.Command, "cat >") && strings.Contains(payload.Command, "/"+name) {
				s.files[name] = append([]byte(nil), input...)
			}
		}
		s.mu.Unlock()
		_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		return
	}
}

func (s *mockSSHServer) openForwardChannel() (ssh.Channel, error) {
	s.mu.Lock()
	conn := s.conn
	port := s.forwardPort
	s.mu.Unlock()
	if conn == nil || port == 0 {
		return nil, fmt.Errorf("reverse forwarding is not ready")
	}
	payload := struct {
		ConnectedAddress  string
		ConnectedPort     uint32
		OriginatorAddress string
		OriginatorPort    uint32
	}{"127.0.0.1", port, "127.0.0.1", 12345}
	channel, requests, err := conn.OpenChannel("forwarded-tcpip", ssh.Marshal(&payload))
	if err == nil {
		go ssh.DiscardRequests(requests)
	}
	return channel, err
}

func (s *mockSSHServer) credentials() map[string][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	files := make(map[string][]byte, len(s.files))
	for name, data := range s.files {
		files[name] = append([]byte(nil), data...)
	}
	return files
}

func (s *mockSSHServer) commandLog() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...)
}

func (s *mockSSHServer) close() {
	s.mu.Lock()
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.mu.Unlock()
	_ = s.listener.Close()
}

type sshChannelConn struct {
	ssh.Channel
}

func (c *sshChannelConn) LocalAddr() net.Addr              { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }
func (c *sshChannelConn) RemoteAddr() net.Addr             { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }
func (c *sshChannelConn) SetDeadline(time.Time) error      { return nil }
func (c *sshChannelConn) SetReadDeadline(time.Time) error  { return nil }
func (c *sshChannelConn) SetWriteDeadline(time.Time) error { return nil }
