package remote

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"slices"
	"testing"
	"time"
)

func (h *testHub) beginPairingWithTTL(t *testing.T, ttl time.Duration) (PairingSession, *tls.Conn, *bufio.Reader, PairingRequest) {
	t.Helper()
	session, err := h.hub.StartPairingSession(context.Background(), PairingSessionOptions{TTL: ttl})
	if err != nil {
		t.Fatal(err)
	}
	conn, reader, request := h.requestPairing(t, session, "gpu-node")
	return session, conn, reader, request
}

func (h *testHub) requestPairing(t *testing.T, session PairingSession, hostname string) (*tls.Conn, *bufio.Reader, PairingRequest) {
	t.Helper()
	conn, reader := h.dialTLS(t, nil)
	if _, err := io.WriteString(conn, HandshakePairPrefix+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLine(conn, pairHello{
		Code: session.Code, Hostname: hostname, OS: "linux", Arch: "amd64",
		Platform: "linux/amd64", CLIVersion: "1.2.3", HardwareSummary: "Test GPU",
	}); err != nil {
		t.Fatal(err)
	}
	request := h.waitEvent(t, HubEventPairingRequested).Pairing
	if request == nil {
		t.Fatal("missing pairing request")
	}
	return conn, reader, *request
}

func TestPairingApprovalRechecksExpiry(t *testing.T) {
	h := startTestHub(t)
	_, _, reader, request := h.beginPairing(t)
	h.hub.mu.Lock()
	h.hub.pairing.session.ExpiresAt = time.Now().Add(-time.Second)
	h.hub.mu.Unlock()
	if _, err := h.hub.DecidePairing(PairingDecision{
		RequestID: request.RequestID, Option: PairingApprove,
	}); !errors.Is(err, ErrPairingExpired) {
		t.Fatalf("approval error = %v", err)
	}
	response := decodePairingResponse(t, reader)
	if response.OK || response.Error != "pair_rejected" {
		t.Fatalf("expired approval response = %+v", response)
	}
	if requests := h.hub.PendingPairingRequests(); len(requests) != 0 {
		t.Fatalf("expired request remained pending: %+v", requests)
	}
}

func TestPairingApprovalConsumesCodeForOtherRequests(t *testing.T) {
	h := startTestHub(t)
	session, err := h.hub.StartPairingSession(context.Background(), PairingSessionOptions{TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	_, firstReader, first := h.requestPairing(t, session, "gpu-one")
	_, secondReader, second := h.requestPairing(t, session, "gpu-two")
	credentials, err := h.hub.DecidePairing(PairingDecision{RequestID: first.RequestID, Option: PairingApprove})
	if err != nil {
		t.Fatal(err)
	}
	firstResponse := decodePairingResponse(t, firstReader)
	if !firstResponse.OK || firstResponse.NodeID != credentials.NodeID {
		t.Fatalf("first response = %+v", firstResponse)
	}
	secondResponse := decodePairingResponse(t, secondReader)
	if secondResponse.OK || secondResponse.Error != "pair_rejected" {
		t.Fatalf("second response = %+v", secondResponse)
	}
	if _, err := h.hub.DecidePairing(PairingDecision{
		RequestID: second.RequestID, Option: PairingApprove,
	}); !errors.Is(err, ErrPairingExpired) {
		t.Fatalf("second approval error = %v", err)
	}
	if _, ok := h.hub.ActivePairingSession(); ok {
		t.Fatal("approved pairing code remained active")
	}
	if requests := h.hub.PendingPairingRequests(); len(requests) != 0 {
		t.Fatalf("duplicate requests remained pending: %+v", requests)
	}
}

func TestPairingDisconnectRemovesPendingRequest(t *testing.T) {
	h := startTestHub(t)
	_, conn, _, request := h.beginPairing(t)
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	event := h.waitEvent(t, HubEventPairingResolved)
	if event.Pairing == nil || event.Pairing.RequestID != request.RequestID || event.Reason != "peer_disconnected" {
		t.Fatalf("disconnect event = %+v", event)
	}
	if requests := h.hub.PendingPairingRequests(); len(requests) != 0 {
		t.Fatalf("disconnected request remained pending: %+v", requests)
	}
	if _, err := h.hub.DecidePairing(PairingDecision{
		RequestID: request.RequestID, Option: PairingApprove,
	}); !errors.Is(err, ErrPairingExpired) {
		t.Fatalf("disconnected approval error = %v", err)
	}
}

func TestCandidateHostsFiltersTUNLinkLocalDownAndNormalizesMDNS(t *testing.T) {
	ifaces := []candidateInterface{
		{flags: net.FlagUp | net.FlagLoopback, addrs: []string{"127.0.0.1/8"}},
		{flags: 0, addrs: []string{"192.168.50.9/24"}},
		{flags: net.FlagUp, addrs: []string{
			"198.18.0.1/30",
			"198.19.10.2/24",
			"169.254.12.34/16",
			"192.0.2.10/24",
			"192.168.1.105/24",
			"192.168.1.105/24",
			"100.101.2.3/32",
			"8.8.4.4/32",
			"fe80::1/64",
		}},
	}
	got := collectCandidateHosts(ifaces, "XudongdeMac-Mini.local.")
	want := []string{"100.101.2.3", "192.168.1.105", "8.8.4.4", "XudongdeMac-Mini.local"}
	if !slices.Equal(got, want) {
		t.Fatalf("collectCandidateHosts(macOS) = %v, want %v", got, want)
	}
	if gotLinux := collectCandidateHosts(nil, "omarchy"); !slices.Equal(gotLinux, []string{"omarchy.local"}) {
		t.Fatalf("collectCandidateHosts(linux) = %v", gotLinux)
	}
	if gotFQDN := collectCandidateHosts(nil, "gpu01.corp.internal"); !slices.Equal(gotFQDN, []string{"gpu01.corp.internal"}) {
		t.Fatalf("collectCandidateHosts(fqdn) = %v", gotFQDN)
	}
	if gotEmpty := collectCandidateHosts(nil, ""); !slices.Equal(gotEmpty, []string{"127.0.0.1"}) {
		t.Fatalf("collectCandidateHosts(empty) = %v", gotEmpty)
	}
}

func TestHubRefreshesServerCertificateWhenCandidateHostsChange(t *testing.T) {
	h := startTestHub(t)
	h.hub.mu.Lock()
	h.hub.cfg.CandidateHosts = []string{"127.0.0.1", "localhost", "gpu-dynamic.local"}
	h.hub.mu.Unlock()
	conn, err := tls.Dial("tcp", h.hub.Addr(), &tls.Config{
		MinVersion: tls.VersionTLS13, RootCAs: h.hub.ca.pool, ServerName: "gpu-dynamic.local",
	})
	if err != nil {
		t.Fatalf("dynamic SAN TLS dial failed: %v", err)
	}
	_ = conn.Close()
}

func TestBootstrapCommandsFallbackAcrossCandidateURLs(t *testing.T) {
	urls := joinHTTPURLs([]string{"100.84.1.2:48860", "192.168.1.105:48860", "mac.local:48860"}, 48861, "123456")
	if len(urls) != 3 {
		t.Fatalf("joinHTTPURLs = %v", urls)
	}
	unix := formatUnixBootstrapCommand(urls)
	if unix == "" || unix[0] != '(' {
		t.Fatalf("expected multi-URL unix fallback command, got %q", unix)
	}
	win := formatWindowsBootstrapCommand(urls)
	if win == "" || win[:2] != "$s" {
		t.Fatalf("expected multi-URL windows fallback command, got %q", win)
	}
}
