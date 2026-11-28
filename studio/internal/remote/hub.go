package remote

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

type hub struct {
	cfg HubConfig
	ca  *certificateAuthority

	mu       sync.RWMutex
	listener net.Listener
	httpLn   net.Listener
	httpSrv  *http.Server
	started  bool
	closed   bool
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	events   eventBus

	pairing        *activePairing
	pairRequests   map[string]*pendingPairing
	peers          map[string]*nodePeer
	tunnels        map[string]*nodeTunnel
	pendingStreams map[string]*pendingTunnelStream
	hostRequests   map[string]*pendingHostKey
	sshSessions    map[string]*sshBridgeSession
}

func (h *hub) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return fmt.Errorf("remote hub is closed")
	}
	if h.started {
		h.mu.Unlock()
		return nil
	}
	h.ctx, h.cancel = context.WithCancel(ctx)
	hosts := h.candidateHosts()
	certificate, err := h.ca.serverCertificate(hosts)
	if err != nil {
		h.mu.Unlock()
		return err
	}
	baseListener, err := net.Listen("tcp", h.cfg.ListenAddr)
	if err != nil {
		h.cancel()
		h.mu.Unlock()
		return fmt.Errorf("listen for remote hub: %w", err)
	}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate},
		ClientAuth: tls.RequestClientCert,
	}
	h.listener = tls.NewListener(baseListener, tlsConfig)
	httpLn, err := net.Listen("tcp", h.cfg.HTTPJoinAddr)
	if err != nil {
		baseListener.Close()
		h.cancel()
		h.mu.Unlock()
		return fmt.Errorf("listen for pairing HTTP: %w", err)
	}
	h.httpLn = httpLn
	h.httpSrv = &http.Server{Handler: h.joinHTTPHandler(), ReadHeaderTimeout: 10 * time.Second}
	h.started = true
	h.wg.Add(2)
	go h.acceptLoop()
	go func() {
		defer h.wg.Done()
		_ = h.httpSrv.Serve(httpLn)
	}()
	h.mu.Unlock()
	return nil
}

func (h *hub) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	if h.cancel != nil {
		h.cancel()
	}
	listener, httpLn, httpSrv := h.listener, h.httpLn, h.httpSrv
	peers := make([]*nodePeer, 0, len(h.peers))
	for _, peer := range h.peers {
		peers = append(peers, peer)
	}
	tunnels := make([]*nodeTunnel, 0, len(h.tunnels))
	for _, tunnel := range h.tunnels {
		tunnels = append(tunnels, tunnel)
	}
	streams := make([]*pendingTunnelStream, 0, len(h.pendingStreams))
	for _, stream := range h.pendingStreams {
		streams = append(streams, stream)
	}
	h.pendingStreams = make(map[string]*pendingTunnelStream)
	pairings := make([]*pendingPairing, 0, len(h.pairRequests))
	for _, request := range h.pairRequests {
		pairings = append(pairings, request)
	}
	hostKeys := make([]*pendingHostKey, 0, len(h.hostRequests))
	for _, request := range h.hostRequests {
		hostKeys = append(hostKeys, request)
	}
	sshSessions := make([]*sshBridgeSession, 0, len(h.sshSessions))
	for _, session := range h.sshSessions {
		sshSessions = append(sshSessions, session)
	}
	h.mu.Unlock()

	if listener != nil {
		_ = listener.Close()
	}
	if httpSrv != nil {
		_ = httpSrv.Close()
	} else if httpLn != nil {
		_ = httpLn.Close()
	}
	for _, stream := range streams {
		stream.close()
	}
	for _, request := range pairings {
		request.finish(pairingResult{err: ErrPairingExpired})
	}
	for _, request := range hostKeys {
		request.finish(ErrHostKeyRejected)
	}
	for _, peer := range peers {
		_ = peer.Close()
	}
	for _, tunnel := range tunnels {
		_ = tunnel.close()
	}
	for _, session := range sshSessions {
		_ = session.close()
	}
	h.events.close()
	h.wg.Wait()
	return nil
}

func (h *hub) Addr() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.listener == nil {
		return ""
	}
	return h.listener.Addr().String()
}

func (h *hub) HTTPJoinAddr() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.httpLn == nil {
		return ""
	}
	return h.httpLn.Addr().String()
}

func (h *hub) CASha256() string { return h.ca.sha256() }

func (h *hub) SubscribeEvents() (<-chan HubEvent, func()) { return h.events.subscribe() }

func (h *hub) acceptLoop() {
	defer h.wg.Done()
	for {
		conn, err := h.listener.Accept()
		if err != nil {
			if !h.isClosed() && !errors.Is(err, net.ErrClosed) {
				continue
			}
			return
		}
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			h.handleConnection(conn)
		}()
	}
}

func (h *hub) handleConnection(conn net.Conn) {
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		_ = conn.Close()
		return
	}
	handshakeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = tlsConn.SetDeadline(time.Now().Add(15 * time.Second))
	if err := tlsConn.HandshakeContext(handshakeCtx); err != nil {
		_ = tlsConn.Close()
		return
	}
	reader := bufio.NewReaderSize(tlsConn, 64*1024)
	line, err := readWireLine(reader, 256)
	if err != nil {
		_ = tlsConn.Close()
		return
	}
	_ = tlsConn.SetDeadline(time.Time{})
	switch strings.TrimSpace(string(line)) {
	case HandshakePairPrefix:
		h.handlePairing(tlsConn, reader)
	case HandshakeControlPrefix:
		h.handleControl(tlsConn, reader)
	default:
		if strings.HasPrefix(string(line), HandshakeTunnelPrefix) {
			streamID := strings.TrimSpace(strings.TrimPrefix(string(line), HandshakeTunnelPrefix))
			h.handleTunnelStream(tlsConn, reader, streamID)
			return
		}
		_ = tlsConn.Close()
	}
}

func (h *hub) authenticatedNode(conn *tls.Conn) (string, error) {
	if err := conn.Handshake(); err != nil {
		return "", err
	}
	return h.ca.verifyPeer(conn.ConnectionState().PeerCertificates)
}

func (h *hub) handleControl(conn *tls.Conn, reader *bufio.Reader) {
	nodeID, err := h.authenticatedNode(conn)
	if err != nil {
		_ = conn.Close()
		return
	}
	frame, err := decodeWireLine(reader)
	if err != nil || frame.Event != EventHello {
		_ = conn.Close()
		return
	}
	peer := newNodePeer(h, nodeID, conn, reader)
	if err := json.Unmarshal(frame.Data, &peer.info); err != nil {
		_ = conn.Close()
		return
	}
	peer.info.NodeID = nodeID
	peer.info.RemoteAddr = conn.RemoteAddr().String()
	if peer.info.Transport == "" {
		peer.info.Transport = "mtls"
	}
	peer.info.ConnectedAt = time.Now()
	peer.info.LastHeartbeatAt = peer.info.ConnectedAt
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = conn.Close()
		return
	}
	if h.sshSessions[nodeID] != nil {
		peer.info.Transport = "ssh_reverse"
	}
	previous := h.peers[nodeID]
	h.peers[nodeID] = peer
	h.mu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
	info := peer.Info()
	h.events.emit(HubEvent{Type: HubEventPeerConnected, NodeID: nodeID, Peer: &info})
	peer.readLoop()
}

func (h *hub) removePeer(peer *nodePeer, reason string) {
	h.mu.Lock()
	if h.peers[peer.nodeID] != peer {
		h.mu.Unlock()
		return
	}
	delete(h.peers, peer.nodeID)
	h.mu.Unlock()
	info := peer.Info()
	h.events.emit(HubEvent{Type: HubEventPeerDisconnected, NodeID: peer.nodeID, Peer: &info, Reason: reason})
}

func (h *hub) Peers() []NodePeer {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]string, 0, len(h.peers))
	for id := range h.peers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	peers := make([]NodePeer, 0, len(ids))
	for _, id := range ids {
		peers = append(peers, h.peers[id])
	}
	return peers
}

func (h *hub) Peer(nodeID string) (NodePeer, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	peer, ok := h.peers[nodeID]
	return peer, ok && peer.Connected()
}

func (h *hub) RevokeNode(nodeID string) error {
	if err := h.ca.revoke(nodeID); err != nil {
		return err
	}
	h.mu.Lock()
	peer := h.peers[nodeID]
	sshSession := h.sshSessions[nodeID]
	delete(h.sshSessions, nodeID)
	tunnels := make([]*nodeTunnel, 0)
	for id, tunnel := range h.tunnels {
		if tunnel.info.NodeID == nodeID {
			tunnels = append(tunnels, tunnel)
			delete(h.tunnels, id)
		}
	}
	pending := make([]*pendingTunnelStream, 0)
	for id, stream := range h.pendingStreams {
		if stream.nodeID == nodeID {
			pending = append(pending, stream)
			delete(h.pendingStreams, id)
		}
	}
	h.mu.Unlock()
	for _, stream := range pending {
		stream.close()
	}
	for _, tunnel := range tunnels {
		_ = tunnel.close()
	}
	if sshSession != nil {
		_ = sshSession.close()
	}
	if peer != nil {
		return peer.Close()
	}
	return nil
}

func (h *hub) isClosed() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.closed
}

func (h *hub) context() context.Context {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.ctx != nil {
		return h.ctx
	}
	return context.Background()
}
