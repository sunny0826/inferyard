package remote

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"regexp"
	"slices"
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
	request PairingRequest
	result  chan pairingResult
	once    sync.Once
}

func (p *pendingPairing) finish(result pairingResult) {
	p.once.Do(func() { p.result <- result })
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

func (h *hub) StartPairingSession(ctx context.Context, opts PairingSessionOptions) (PairingSession, error) {
	if err := ctx.Err(); err != nil {
		return PairingSession{}, err
	}
	port := listenerPort(h.Addr())
	if port == 0 {
		return PairingSession{}, fmt.Errorf("remote hub is not started")
	}
	ttl := opts.TTL
	if ttl == 0 {
		ttl = DefaultPairTTL
	}
	if ttl < 0 {
		return PairingSession{}, fmt.Errorf("pairing TTL must be positive")
	}
	value, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return PairingSession{}, err
	}
	code := fmt.Sprintf("%06d", value.Int64())
	version := firstNonEmpty(opts.CLIVersion, h.cfg.CLIVersion, "latest")
	source := firstNonEmpty(opts.DownloadSource, h.cfg.DownloadSource)
	proxy := firstNonEmpty(opts.DownloadProxy, h.cfg.DownloadProxy)
	now := time.Now()
	session := PairingSession{
		Code: code, FormattedCode: code[:3] + "-" + code[3:], CASha256: h.ca.sha256(),
		CandidateAddrs: h.candidateAddresses(port), CreatedAt: now, ExpiresAt: now.Add(ttl),
	}
	session.JoinScriptUnix, err = unixJoinScript(session, version, source, proxy)
	if err != nil {
		return PairingSession{}, err
	}
	session.JoinScriptWin, err = windowsJoinScript(session, version, source, proxy)
	if err != nil {
		return PairingSession{}, err
	}
	httpPort := listenerPort(h.HTTPJoinAddr())
	httpHost := firstCandidateHost(session.CandidateAddrs)
	joinURL := fmt.Sprintf("http://%s/join/%s", net.JoinHostPort(httpHost, fmt.Sprint(httpPort)), code)
	session.UnixCommand = "curl --fail --silent --show-error " + shellQuote(joinURL) + " | sh"
	session.WindowsCommand = "irm '" + joinURL + ".ps1' | iex"

	h.mu.Lock()
	oldRequests := make([]*pendingPairing, 0)
	if h.pairing != nil {
		for id, request := range h.pairRequests {
			if request.request.Code == h.pairing.session.Code {
				oldRequests = append(oldRequests, request)
				delete(h.pairRequests, id)
			}
		}
	}
	h.pairing = &activePairing{session: session}
	h.mu.Unlock()
	for _, request := range oldRequests {
		request.finish(pairingResult{err: ErrPairingExpired})
	}
	return session, nil
}

func (h *hub) ActivePairingSession() (PairingSession, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pairing == nil {
		return PairingSession{}, false
	}
	if time.Now().After(h.pairing.session.ExpiresAt) {
		h.pairing = nil
		return PairingSession{}, false
	}
	return clonePairingSession(h.pairing.session), true
}

func (h *hub) CancelPairingSession(code string) error {
	normalized, err := normalizePairCode(code)
	if err != nil {
		return err
	}
	h.mu.Lock()
	if h.pairing == nil || h.pairing.session.Code != normalized {
		h.mu.Unlock()
		return ErrPairingExpired
	}
	h.pairing = nil
	pendingRequests := make([]*pendingPairing, 0)
	for id, request := range h.pairRequests {
		if request.request.Code == normalized {
			pendingRequests = append(pendingRequests, request)
			delete(h.pairRequests, id)
		}
	}
	h.mu.Unlock()
	for _, request := range pendingRequests {
		request.finish(pairingResult{err: ErrPairingExpired})
	}
	return nil
}

func (h *hub) PendingPairingRequests() []PairingRequest {
	h.mu.RLock()
	defer h.mu.RUnlock()
	requests := make([]PairingRequest, 0, len(h.pairRequests))
	for _, pending := range h.pairRequests {
		requests = append(requests, clonePairingRequest(pending.request))
	}
	slices.SortFunc(requests, func(a, b PairingRequest) int { return a.RequestedAt.Compare(b.RequestedAt) })
	return requests
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
	delete(h.pairRequests, decision.RequestID)
	h.mu.Unlock()
	if decision.Option == PairingReject {
		pending.finish(pairingResult{decision: decision.Option, err: ErrPairingRejected})
		h.events.emit(HubEvent{Type: HubEventPairingResolved, Pairing: &pending.request, Reason: string(decision.Option)})
		return PairedNodeCredentials{}, ErrPairingRejected
	}
	nodeID, err := randomID("node-")
	if err != nil {
		return PairedNodeCredentials{}, err
	}
	issued, err := h.ca.issueNode(nodeID)
	if err != nil {
		return PairedNodeCredentials{}, err
	}
	request := pending.request
	credentials := PairedNodeCredentials{
		NodeID: nodeID, DisplayName: decision.DisplayName, Hostname: request.Hostname,
		OS: request.OS, Arch: request.Arch, Platform: request.Platform, CLIVersion: request.CLIVersion,
		HardwareSummary: request.HardwareSummary, CertSerial: issued.Serial,
		CertFingerprint: issued.Fingerprint, PairedAt: time.Now(), DeviceSnapshot: request.DeviceSnapshot,
	}
	h.mu.Lock()
	if h.pairing != nil && h.pairing.session.Code == request.Code {
		h.pairing = nil
	}
	h.mu.Unlock()
	pending.finish(pairingResult{credentials: credentials, issued: issued, decision: decision.Option})
	h.events.emit(HubEvent{Type: HubEventPairingResolved, NodeID: nodeID, Pairing: &request, Reason: string(decision.Option)})
	return credentials, nil
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
	if err != nil || !h.validPairingCode(code) {
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
	pending := &pendingPairing{request: request, result: make(chan pairingResult, 1)}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.pairRequests[requestID] = pending
	h.mu.Unlock()
	h.events.emit(HubEvent{Type: HubEventPairingRequested, Pairing: &request})
	select {
	case result := <-pending.result:
		h.writePairingResult(conn, request, result)
	case <-h.context().Done():
	}
}

func (h *hub) validPairingCode(code string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pairing == nil || h.pairing.session.Code != code || time.Now().After(h.pairing.session.ExpiresAt) {
		return false
	}
	return true
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

func clonePairingSession(session PairingSession) PairingSession {
	session.CandidateAddrs = slices.Clone(session.CandidateAddrs)
	return session
}

func clonePairingRequest(request PairingRequest) PairingRequest {
	request.Options = slices.Clone(request.Options)
	request.DeviceSnapshot = append(json.RawMessage(nil), request.DeviceSnapshot...)
	return request
}

func listenerPort(addr string) int {
	_, rawPort, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	var port int
	_, _ = fmt.Sscanf(rawPort, "%d", &port)
	return port
}

func firstCandidateHost(addrs []string) string {
	if len(addrs) == 0 {
		return "127.0.0.1"
	}
	host, _, err := net.SplitHostPort(addrs[0])
	if err == nil {
		return host
	}
	return "127.0.0.1"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sourceHint(host string) string {
	ip := net.ParseIP(host)
	if isTailscaleIP(ip) {
		return "Tailscale"
	}
	if ip != nil && ip.IsPrivate() {
		return "LAN"
	}
	return ""
}

func isTailscaleIP(ip net.IP) bool {
	_, network, _ := net.ParseCIDR("100.64.0.0/10")
	return ip != nil && network.Contains(ip)
}

func (h *hub) candidateHosts() []string {
	if len(h.cfg.CandidateHosts) > 0 {
		var hosts []string
		for _, host := range h.cfg.CandidateHosts {
			host = strings.TrimSpace(host)
			if parsedHost, _, err := net.SplitHostPort(host); err == nil {
				host = parsedHost
			}
			if net.ParseIP(strings.Trim(host, "[]")) != nil || validDNSName(host) {
				hosts = append(hosts, strings.Trim(host, "[]"))
			}
		}
		return hosts
	}
	var tailscale, lan []string
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil || ip.To4() == nil || ip.IsLoopback() {
				continue
			}
			if isTailscaleIP(ip) {
				tailscale = append(tailscale, ip.String())
			} else {
				lan = append(lan, ip.String())
			}
		}
	}
	slices.Sort(tailscale)
	slices.Sort(lan)
	hosts := append(tailscale, lan...)
	if hostname, err := os.Hostname(); err == nil {
		hosts = append(hosts, hostname+".local")
	}
	if len(hosts) == 0 {
		hosts = append(hosts, "127.0.0.1")
	}
	return hosts
}

func (h *hub) candidateAddresses(port int) []string {
	hosts := h.candidateHosts()
	addresses := make([]string, 0, len(hosts))
	for _, host := range hosts {
		addresses = append(addresses, net.JoinHostPort(host, fmt.Sprint(port)))
	}
	return addresses
}

func (h *hub) joinHTTPHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		path := strings.TrimPrefix(request.URL.Path, "/join/")
		if path == request.URL.Path {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		windows := strings.HasSuffix(path, ".ps1")
		code := strings.TrimSuffix(path, ".ps1")
		normalized, err := normalizePairCode(code)
		if err != nil {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		h.mu.RLock()
		active := h.pairing
		valid := active != nil && active.session.Code == normalized && time.Now().Before(active.session.ExpiresAt)
		var session PairingSession
		if valid {
			session = clonePairingSession(active.session)
		}
		h.mu.RUnlock()
		if !valid {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		if windows {
			writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = writer.Write([]byte(session.JoinScriptWin))
		} else {
			writer.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
			_, _ = writer.Write([]byte(session.JoinScriptUnix))
		}
	})
}
