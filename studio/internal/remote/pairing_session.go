package remote

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"slices"
	"time"
)

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
		h.resolvePairingError(request, ErrPairingExpired, "pairing_expired")
	}
	return session, nil
}

func (h *hub) ActivePairingSession() (PairingSession, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pairing == nil {
		return PairingSession{}, false
	}
	if !time.Now().Before(h.pairing.session.ExpiresAt) {
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
		h.resolvePairingError(request, ErrPairingExpired, "pairing_expired")
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

func clonePairingSession(session PairingSession) PairingSession {
	session.CandidateAddrs = slices.Clone(session.CandidateAddrs)
	return session
}
