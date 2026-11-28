package remote

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type testHub struct {
	hub    *hub
	events <-chan HubEvent
}

func startTestHub(t *testing.T) *testHub {
	t.Helper()
	value, err := NewHub(HubConfig{
		DataDir: t.TempDir(), ListenAddr: "127.0.0.1:0", HTTPJoinAddr: "127.0.0.1:0",
		CandidateHosts: []string{"127.0.0.1", "localhost"}, CLIVersion: "1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	h := value.(*hub)
	if err := h.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := h.SubscribeEvents()
	t.Cleanup(func() { unsubscribe(); _ = h.Close() })
	return &testHub{hub: h, events: events}
}

func (h *testHub) waitEvent(t *testing.T, eventType HubEventType) HubEvent {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-h.events:
			if event.Type == eventType {
				return event
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s", eventType)
		}
	}
}

func (h *testHub) dialTLS(t *testing.T, certificate *tls.Certificate) (*tls.Conn, *bufio.Reader) {
	t.Helper()
	config := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: h.hub.ca.pool, ServerName: "localhost"}
	if certificate != nil {
		config.Certificates = []tls.Certificate{*certificate}
	}
	conn, err := tls.Dial("tcp", h.hub.Addr(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, bufio.NewReader(conn)
}

func (h *testHub) beginPairing(t *testing.T) (PairingSession, *tls.Conn, *bufio.Reader, PairingRequest) {
	t.Helper()
	session, err := h.hub.StartPairingSession(context.Background(), PairingSessionOptions{TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	conn, reader := h.dialTLS(t, nil)
	if _, err := io.WriteString(conn, HandshakePairPrefix+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLine(conn, pairHello{
		Code: session.Code, Hostname: "gpu-node", OS: "linux", Arch: "amd64",
		Platform: "linux/amd64", CLIVersion: "1.2.3", HardwareSummary: "Test GPU",
	}); err != nil {
		t.Fatal(err)
	}
	request := h.waitEvent(t, HubEventPairingRequested).Pairing
	if request == nil {
		t.Fatal("missing pairing request")
	}
	return session, conn, reader, *request
}

type pairingResponse struct {
	OK              bool     `json:"ok"`
	Decision        string   `json:"decision"`
	Error           string   `json:"error"`
	NodeID          string   `json:"node_id"`
	CACertPEM       string   `json:"ca_cert_pem"`
	ClientCertPEM   string   `json:"client_cert_pem"`
	ClientKeyPEM    string   `json:"client_key_pem"`
	CertSerial      string   `json:"cert_serial"`
	CertFingerprint string   `json:"cert_fingerprint"`
	HubAddrs        []string `json:"hub_addrs"`
}

func decodePairingResponse(t *testing.T, reader *bufio.Reader) pairingResponse {
	t.Helper()
	line, err := readWireLine(reader, MaxControlLineBytes)
	if err != nil {
		t.Fatal(err)
	}
	var response pairingResponse
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func approvePairing(t *testing.T, h *testHub) (pairingResponse, tls.Certificate) {
	t.Helper()
	_, _, reader, request := h.beginPairing(t)
	credentials, err := h.hub.DecidePairing(PairingDecision{
		RequestID: request.RequestID, Option: PairingApprove, DisplayName: "Cloud GPU",
	})
	if err != nil {
		t.Fatal(err)
	}
	response := decodePairingResponse(t, reader)
	if !response.OK || response.NodeID != credentials.NodeID || response.CertSerial != credentials.CertSerial {
		t.Fatalf("unexpected pairing response: %+v", response)
	}
	certificate, err := tls.X509KeyPair([]byte(response.ClientCertPEM), []byte(response.ClientKeyPEM))
	if err != nil {
		t.Fatal(err)
	}
	return response, certificate
}

func (h *testHub) dialControl(t *testing.T, certificate tls.Certificate, nodeID string) (*tls.Conn, *bufio.Reader) {
	t.Helper()
	conn, reader := h.dialTLS(t, &certificate)
	if _, err := io.WriteString(conn, HandshakeControlPrefix+"\n"); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(NodePeerInfo{NodeID: nodeID, Hostname: "gpu-node", Platform: "linux/amd64"})
	if err := writeJSONLine(conn, wireFrame{Event: EventHello, Data: data}); err != nil {
		t.Fatal(err)
	}
	event := h.waitEvent(t, HubEventPeerConnected)
	if event.NodeID != nodeID {
		t.Fatalf("connected node = %q, want %q", event.NodeID, nodeID)
	}
	return conn, reader
}

func TestPairingApproveControlRPCAndRevoke(t *testing.T) {
	h := startTestHub(t)
	session, conn, reader, request := h.beginPairing(t)
	if session.Code != request.Code || session.FormattedCode != request.FormattedCode {
		t.Fatalf("pairing code mismatch")
	}
	if len(session.CandidateAddrs) == 0 || !strings.Contains(session.JoinScriptUnix, "inferyard join") ||
		!strings.Contains(session.JoinScriptUnix, "0.13.6") || !strings.Contains(session.JoinScriptWin, "$Hubs") ||
		!strings.Contains(session.JoinScriptWin, "0.13.6") {
		t.Fatalf("incomplete join scripts")
	}
	response, err := http.Get(fmt.Sprintf("http://%s/join/%s", h.hub.HTTPJoinAddr(), session.Code))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), session.Code) {
		t.Fatalf("join HTTP status=%d body=%s", response.StatusCode, body)
	}
	if _, err := h.hub.DecidePairing(PairingDecision{RequestID: request.RequestID, Option: "yes"}); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("invalid option error = %v", err)
	}
	credentials, err := h.hub.DecidePairing(PairingDecision{RequestID: request.RequestID, Option: PairingApprove})
	if err != nil {
		t.Fatal(err)
	}
	pairResponse := decodePairingResponse(t, reader)
	certificate, err := tls.X509KeyPair([]byte(pairResponse.ClientCertPEM), []byte(pairResponse.ClientKeyPEM))
	if err != nil {
		t.Fatal(err)
	}
	if pairResponse.NodeID != credentials.NodeID || pairResponse.CACertPEM == "" || len(pairResponse.HubAddrs) == 0 {
		t.Fatalf("bad credentials: %+v", pairResponse)
	}
	_ = conn.Close()
	control, controlReader := h.dialControl(t, certificate, credentials.NodeID)
	peer, ok := h.hub.Peer(credentials.NodeID)
	if !ok {
		t.Fatal("peer not registered")
	}
	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		for {
			frame, err := decodeWireLine(controlReader)
			if err != nil {
				return
			}
			if frame.Method == "echo" {
				_ = writeJSONLine(control, wireFrame{ID: frame.ID, Result: frame.Params})
			} else if frame.Method == "fail" {
				_ = writeJSONLine(control, wireFrame{ID: frame.ID, Error: &RPCError{Code: "bad_request", Message: "failed"}})
			}
		}
	}()
	var wg sync.WaitGroup
	for index := 0; index < 24; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			var result map[string]int
			err := peer.Call(context.Background(), "echo", map[string]int{"value": index}, &result)
			if err != nil || result["value"] != index {
				t.Errorf("RPC %d: result=%v err=%v", index, result, err)
			}
		}(index)
	}
	wg.Wait()
	var rpcErr *RPCError
	if err := peer.Call(context.Background(), "fail", nil, nil); !errors.As(err, &rpcErr) || rpcErr.Code != "bad_request" {
		t.Fatalf("RPC error = %v", err)
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := peer.Call(cancelCtx, "echo", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled RPC error = %v", err)
	}
	if err := h.hub.RevokeNode(credentials.NodeID); err != nil {
		t.Fatal(err)
	}
	h.waitEvent(t, HubEventPeerDisconnected)
	<-agentDone
	if peer.Connected() {
		t.Fatal("revoked peer remains connected")
	}
	rejected, rejectedReader := h.dialTLS(t, &certificate)
	_, _ = io.WriteString(rejected, HandshakeControlPrefix+"\n")
	_ = writeJSONLine(rejected, wireFrame{Event: EventHello, Data: json.RawMessage(`{}`)})
	_ = rejected.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := readWireLine(rejectedReader, 128); err == nil {
		t.Fatal("revoked certificate was accepted")
	}
	if _, ok := h.hub.ActivePairingSession(); ok {
		t.Fatal("approved pairing code was not consumed")
	}
}

func TestPairingRejectAndExpiry(t *testing.T) {
	h := startTestHub(t)
	_, _, reader, request := h.beginPairing(t)
	if _, err := h.hub.DecidePairing(PairingDecision{RequestID: request.RequestID, Option: PairingReject}); !errors.Is(err, ErrPairingRejected) {
		t.Fatal(err)
	}
	response := decodePairingResponse(t, reader)
	if response.OK || response.Decision != string(PairingReject) || response.Error != "pair_rejected" {
		t.Fatalf("reject response = %+v", response)
	}
	session, err := h.hub.StartPairingSession(context.Background(), PairingSessionOptions{TTL: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, ok := h.hub.ActivePairingSession(); ok {
		t.Fatal("expired session remained active")
	}
	responseHTTP, err := http.Get(fmt.Sprintf("http://%s/join/%s", h.hub.HTTPJoinAddr(), session.Code))
	if err != nil {
		t.Fatal(err)
	}
	_ = responseHTTP.Body.Close()
	if responseHTTP.StatusCode != http.StatusNotFound {
		t.Fatalf("expired join status = %d", responseHTTP.StatusCode)
	}
}

func TestServerChainMatchesSPKIFingerprint(t *testing.T) {
	h := startTestHub(t)
	conn, err := tls.Dial("tcp", h.hub.Addr(), &tls.Config{
		MinVersion: tls.VersionTLS13, InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) != 2 {
				return fmt.Errorf("certificate chain length = %d", len(rawCerts))
			}
			root, err := x509.ParseCertificate(rawCerts[1])
			if err != nil {
				return err
			}
			digest := sha256.Sum256(root.RawSubjectPublicKeyInfo)
			if hex.EncodeToString(digest[:]) != h.hub.CASha256() {
				return fmt.Errorf("CA fingerprint mismatch")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}

func TestControlRequiresClientCertificate(t *testing.T) {
	h := startTestHub(t)
	conn, reader := h.dialTLS(t, nil)
	_, _ = io.WriteString(conn, HandshakeControlPrefix+"\n")
	_ = writeJSONLine(conn, wireFrame{Event: EventHello, Data: json.RawMessage(`{}`)})
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := readWireLine(reader, 128); err == nil {
		t.Fatal("unauthenticated control connection accepted")
	}
}

func TestHubAddrHelpers(t *testing.T) {
	h := startTestHub(t)
	if listenerPort(h.hub.Addr()) == 0 || listenerPort(h.hub.HTTPJoinAddr()) == 0 {
		t.Fatal("listener ports not available")
	}
	if got := h.hub.PendingPairingRequests(); len(got) != 0 {
		t.Fatal(got)
	}
}
