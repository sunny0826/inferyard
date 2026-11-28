package remote

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

type activePairing struct {
	session PairingSession
}

type pairingResult struct {
	credentials PairedNodeCredentials
	issued      issuedClientCredentials
	decision    PairingOption
	err         error
}

type pendingPairing struct {
	request      PairingRequest
	result       chan pairingResult
	disconnected chan struct{}
	once         sync.Once
}

func (p *pendingPairing) finish(result pairingResult) {
	p.once.Do(func() { p.result <- result })
}

func (p *pendingPairing) isDisconnected() bool {
	select {
	case <-p.disconnected:
		return true
	default:
		return false
	}
}

type pairHello struct {
	Code            string          `json:"code"`
	Hostname        string          `json:"hostname"`
	OS              string          `json:"os"`
	Arch            string          `json:"arch"`
	Platform        string          `json:"platform"`
	CLIVersion      string          `json:"cli_version"`
	HardwareSummary string          `json:"hardware_summary"`
	AutostartMode   string          `json:"autostart_mode"`
	DeviceSnapshot  json.RawMessage `json:"device_snapshot"`
}

func normalizePairCode(raw string) (string, error) {
	code := strings.NewReplacer("-", "", " ", "", "\t", "").Replace(strings.TrimSpace(raw))
	if matched, _ := regexp.MatchString(`^[0-9]{6}$`, code); !matched {
		return "", ErrPairingExpired
	}
	return code, nil
}

func (h *hub) DecidePairing(decision PairingDecision) (PairedNodeCredentials, error) {
	if decision.Option != PairingApprove && decision.Option != PairingReject {
		return PairedNodeCredentials{}, ErrInvalidOption
	}
	h.mu.Lock()
	pending := h.pairRequests[decision.RequestID]
	if pending == nil {
		h.mu.Unlock()
		return PairedNodeCredentials{}, ErrPairingExpired
	}
	valid := h.pairing != nil && h.pairing.session.Code == pending.request.Code &&
		time.Now().Before(h.pairing.session.ExpiresAt) && !pending.isDisconnected()
	if !valid {
		delete(h.pairRequests, decision.RequestID)
		h.mu.Unlock()
		h.resolvePairingError(pending, ErrPairingExpired, "pairing_expired")
		return PairedNodeCredentials{}, ErrPairingExpired
	}
	delete(h.pairRequests, decision.RequestID)
	var others []*pendingPairing
	if decision.Option == PairingApprove {
		h.pairing = nil
		for id, candidate := range h.pairRequests {
			if candidate.request.Code == pending.request.Code {
				others = append(others, candidate)
				delete(h.pairRequests, id)
			}
		}
	}
	request := pending.request
	h.mu.Unlock()
	for _, other := range others {
		h.resolvePairingError(other, ErrPairingExpired, "pairing_expired")
	}
	if decision.Option == PairingReject {
		h.resolvePairingError(pending, ErrPairingRejected, string(decision.Option))
		return PairedNodeCredentials{}, ErrPairingRejected
	}
	nodeID, err := randomID("node-")
	if err != nil {
		h.resolvePairingError(pending, err, "pairing_failed")
		return PairedNodeCredentials{}, err
	}
	issued, err := h.ca.issueNode(nodeID)
	if err != nil {
		h.resolvePairingError(pending, err, "pairing_failed")
		return PairedNodeCredentials{}, err
	}
	credentials := PairedNodeCredentials{
		NodeID: nodeID, DisplayName: decision.DisplayName, Hostname: request.Hostname,
		OS: request.OS, Arch: request.Arch, Platform: request.Platform, CLIVersion: request.CLIVersion,
		HardwareSummary: request.HardwareSummary, CertSerial: issued.Serial,
		CertFingerprint: issued.Fingerprint, PairedAt: time.Now(), DeviceSnapshot: request.DeviceSnapshot,
	}
	pending.finish(pairingResult{credentials: credentials, issued: issued, decision: decision.Option})
	h.events.emit(HubEvent{Type: HubEventPairingResolved, NodeID: nodeID, Pairing: &request, Reason: string(decision.Option)})
	return credentials, nil
}

func (h *hub) resolvePairingError(pending *pendingPairing, err error, reason string) {
	pending.finish(pairingResult{decision: PairingReject, err: err})
	request := pending.request
	h.events.emit(HubEvent{Type: HubEventPairingResolved, Pairing: &request, Reason: reason})
}

func (h *hub) handlePairing(conn *tls.Conn, reader *bufio.Reader) {
	defer conn.Close()
	line, err := readWireLine(reader, MaxControlLineBytes)
	if err != nil {
		return
	}
	var hello pairHello
	if err := json.Unmarshal(line, &hello); err != nil {
		return
	}
	if hello.Code == "" {
		var envelope wireFrame
		if err := json.Unmarshal(line, &envelope); err != nil || envelope.Data == nil {
			return
		}
		if err := json.Unmarshal(envelope.Data, &hello); err != nil {
			return
		}
	}
	code, err := normalizePairCode(hello.Code)
	expiresAt, valid := h.validPairingCode(code)
	if err != nil || !valid {
		_ = writeJSONLine(conn, map[string]any{"ok": false, "error": "pairing_expired"})
		return
	}
	requestID, err := randomID("pair-")
	if err != nil {
		return
	}
	host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	request := PairingRequest{
		RequestID: requestID, Code: code, FormattedCode: code[:3] + "-" + code[3:],
		Hostname: hello.Hostname, OS: hello.OS, Arch: hello.Arch, Platform: hello.Platform,
		CLIVersion: hello.CLIVersion, HardwareSummary: hello.HardwareSummary, SourceIP: host,
		SourceHint: sourceHint(host), AutostartMode: hello.AutostartMode,
		DeviceSnapshot: hello.DeviceSnapshot, RequestedAt: time.Now(),
		Options: []PairingOption{PairingApprove, PairingReject},
	}
	pending := &pendingPairing{
		request: request, result: make(chan pairingResult, 1), disconnected: make(chan struct{}),
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.pairRequests[requestID] = pending
	h.mu.Unlock()
	h.events.emit(HubEvent{Type: HubEventPairingRequested, Pairing: &request})
	_ = conn.SetReadDeadline(expiresAt)
	go monitorPairingConnection(conn, pending.disconnected)
	defer h.cleanupPairingRequest(pending)
	select {
	case result := <-pending.result:
		h.writePairingResult(conn, request, result)
	case <-pending.disconnected:
	case <-h.context().Done():
	}
}

func monitorPairingConnection(conn *tls.Conn, disconnected chan<- struct{}) {
	defer close(disconnected)
	var buffer [1]byte
	for {
		if _, err := conn.Read(buffer[:]); err != nil {
			return
		}
	}
}

func (h *hub) cleanupPairingRequest(pending *pendingPairing) {
	h.mu.Lock()
	current := h.pairRequests[pending.request.RequestID]
	if current == pending {
		delete(h.pairRequests, pending.request.RequestID)
	}
	h.mu.Unlock()
	if current == pending {
		h.resolvePairingError(pending, ErrPairingExpired, "peer_disconnected")
	}
}

func (h *hub) validPairingCode(code string) (time.Time, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.pairing == nil || h.pairing.session.Code != code || !time.Now().Before(h.pairing.session.ExpiresAt) {
		return time.Time{}, false
	}
	return h.pairing.session.ExpiresAt, true
}

func (h *hub) writePairingResult(conn *tls.Conn, request PairingRequest, result pairingResult) {
	if result.err != nil {
		decision := result.decision
		if decision == "" {
			decision = PairingReject
		}
		_ = writeJSONLine(conn, map[string]any{"ok": false, "decision": decision, "error": "pair_rejected"})
		return
	}
	issued := result.issued
	_ = writeJSONLine(conn, map[string]any{
		"ok": true, "decision": PairingApprove, "node_id": issued.NodeID,
		"ca_cert_pem": string(issued.CAPEM), "client_cert_pem": string(issued.CertPEM),
		"client_key_pem": string(issued.KeyPEM), "hub_addrs": requestHubAddresses(h, request.Code),
		"cert_serial": issued.Serial, "cert_fingerprint": issued.Fingerprint,
	})
}

func requestHubAddresses(h *hub, _ string) []string {
	port := listenerPort(h.Addr())
	return h.candidateAddresses(port)
}

func clonePairingRequest(request PairingRequest) PairingRequest {
	request.Options = append([]PairingOption(nil), request.Options...)
	request.DeviceSnapshot = append(json.RawMessage(nil), request.DeviceSnapshot...)
	return request
}
