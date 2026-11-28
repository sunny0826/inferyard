package remote

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestTunnelBidirectionalLargeStream(t *testing.T) {
	h := startTestHub(t)
	paired, certificate := approvePairing(t, h)
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = echoListener.Close() })
	go func() {
		for {
			conn, err := echoListener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	_, controlReader := h.dialControl(t, certificate, paired.NodeID)
	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		for {
			frame, err := decodeWireLine(controlReader)
			if err != nil {
				return
			}
			if frame.Event != EventOpenTunnel {
				continue
			}
			var request struct {
				StreamID   string `json:"stream_id"`
				TargetPort int    `json:"target_port"`
				ServiceID  string `json:"service_id"`
			}
			if err := json.Unmarshal(frame.Data, &request); err != nil {
				return
			}
			go func() {
				remoteService, err := net.Dial("tcp", echoListener.Addr().String())
				if err != nil {
					return
				}
				stream, err := tls.Dial("tcp", h.hub.Addr(), &tls.Config{
					MinVersion: tls.VersionTLS13, RootCAs: h.hub.ca.pool, ServerName: "localhost",
					Certificates: []tls.Certificate{certificate},
				})
				if err != nil {
					_ = remoteService.Close()
					return
				}
				_, _ = io.WriteString(stream, HandshakeTunnelPrefix+request.StreamID+"\n")
				var wg sync.WaitGroup
				wg.Add(2)
				go func() { defer wg.Done(); _, _ = io.Copy(stream, remoteService) }()
				go func() { defer wg.Done(); _, _ = io.Copy(remoteService, stream) }()
				wg.Wait()
				_ = stream.Close()
				_ = remoteService.Close()
			}()
		}
	}()
	info, err := h.hub.OpenTunnel(context.Background(), OpenTunnelRequest{
		NodeID: paired.NodeID, ServiceID: "svc-test", RemotePort: echoListener.Addr().(*net.TCPAddr).Port,
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.LocalPort < DefaultTunnelMin || info.LocalPort > DefaultTunnelMax {
		t.Fatalf("allocated port = %d", info.LocalPort)
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", info.LocalPort))
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 2*1024*1024+137)
	for index := range payload {
		payload[index] = byte(index*31 + 7)
	}
	writeDone := make(chan error, 1)
	go func() {
		_, err := conn.Write(payload)
		writeDone <- err
	}()
	received := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, received); err != nil {
		t.Fatal(err)
	}
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(received) != sha256.Sum256(payload) {
		t.Fatal("large tunnel payload was corrupted")
	}
	deadline := time.Now().Add(2 * time.Second)
	for h.hub.Tunnels()[0].ActiveConns != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := h.hub.Tunnels()[0].ActiveConns; got != 1 {
		t.Fatalf("active connections = %d", got)
	}
	if err := h.hub.CloseTunnel(info.TunnelID); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	h.waitEvent(t, HubEventTunnelClosed)
	if err := h.hub.CloseTunnel(info.TunnelID); !errors.Is(err, ErrTunnelNotFound) {
		t.Fatalf("second close error = %v", err)
	}
	_ = h.hub.RevokeNode(paired.NodeID)
	<-agentDone
}

func TestTunnelRejectsUnknownStreamAndPortExhaustion(t *testing.T) {
	h := startTestHub(t)
	paired, certificate := approvePairing(t, h)
	_, _ = h.dialControl(t, certificate, paired.NodeID)
	conn, reader := h.dialTLS(t, &certificate)
	_, _ = io.WriteString(conn, HandshakeTunnelPrefix+"unknown\n")
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := readWireLine(reader, 32); err == nil {
		t.Fatal("unknown tunnel stream accepted")
	}
	occupied, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", DefaultTunnelMin))
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	h.hub.cfg.TunnelPortMax = DefaultTunnelMin
	if _, err := h.hub.OpenTunnel(context.Background(), OpenTunnelRequest{
		NodeID: paired.NodeID, ServiceID: "svc-test", RemotePort: 8000,
	}); !errors.Is(err, ErrTunnelPortExhaust) {
		t.Fatalf("port exhaustion error = %v", err)
	}
}
