package remote

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
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
