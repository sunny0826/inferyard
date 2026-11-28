package remote

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

type tunnelAttachment struct {
	conn net.Conn
}

type pendingTunnelStream struct {
	id     string
	nodeID string
	tunnel *nodeTunnel
	ready  chan tunnelAttachment
	done   chan struct{}
	once   sync.Once
}

func (p *pendingTunnelStream) close() { p.once.Do(func() { close(p.done) }) }

type nodeTunnel struct {
	hub       *hub
	info      TunnelInfo
	listener  net.Listener
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	active atomic.Int64
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(data []byte) (int, error) { return c.reader.Read(data) }

func (h *hub) OpenTunnel(ctx context.Context, req OpenTunnelRequest) (TunnelInfo, error) {
	if req.RemotePort < 1 || req.RemotePort > 65535 || req.ServiceID == "" {
		return TunnelInfo{}, fmt.Errorf("invalid tunnel request")
	}
	peer, ok := h.Peer(req.NodeID)
	if !ok {
		return TunnelInfo{}, ErrPeerOffline
	}
	_ = peer
	listener, err := h.listenTunnel(req.LocalPort)
	if err != nil {
		return TunnelInfo{}, err
	}
	tunnelID, err := randomID("tunnel-")
	if err != nil {
		_ = listener.Close()
		return TunnelInfo{}, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	tunnel := &nodeTunnel{
		hub: h, listener: listener, done: make(chan struct{}), conns: make(map[net.Conn]struct{}),
		info: TunnelInfo{
			TunnelID: tunnelID, NodeID: req.NodeID, NodeName: req.NodeName, ServiceID: req.ServiceID,
			ModelName: req.ModelName, EngineName: req.EngineName, LocalPort: port,
			LocalURL: fmt.Sprintf("http://127.0.0.1:%d/v1", port), RemotePort: req.RemotePort,
			CreatedAt: time.Now(),
		},
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = listener.Close()
		return TunnelInfo{}, ErrPeerOffline
	}
	h.tunnels[tunnelID] = tunnel
	h.mu.Unlock()
	tunnel.wg.Add(1)
	go tunnel.acceptLoop()
	info := tunnel.snapshot()
	h.events.emit(HubEvent{Type: HubEventTunnelOpened, NodeID: req.NodeID, Tunnel: &info})
	return info, nil
}

func (h *hub) listenTunnel(requested int) (net.Listener, error) {
	if requested != 0 {
		if requested < 1 || requested > 65535 {
			return nil, fmt.Errorf("invalid local tunnel port")
		}
		return net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", requested))
	}
	for port := h.cfg.TunnelPortMin; port <= h.cfg.TunnelPortMax; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return listener, nil
		}
	}
	return nil, ErrTunnelPortExhaust
}

func (h *hub) CloseTunnel(tunnelID string) error {
	h.mu.Lock()
	tunnel := h.tunnels[tunnelID]
	if tunnel != nil {
		delete(h.tunnels, tunnelID)
	}
	h.mu.Unlock()
	if tunnel == nil {
		return ErrTunnelNotFound
	}
	return tunnel.close()
}

func (h *hub) Tunnels() []TunnelInfo {
	h.mu.RLock()
	tunnels := make([]*nodeTunnel, 0, len(h.tunnels))
	for _, tunnel := range h.tunnels {
		tunnels = append(tunnels, tunnel)
	}
	h.mu.RUnlock()
	slices.SortFunc(tunnels, func(a, b *nodeTunnel) int { return a.info.CreatedAt.Compare(b.info.CreatedAt) })
	infos := make([]TunnelInfo, 0, len(tunnels))
	for _, tunnel := range tunnels {
		infos = append(infos, tunnel.snapshot())
	}
	return infos
}

func (t *nodeTunnel) snapshot() TunnelInfo {
	info := t.info
	info.ActiveConns = t.active.Load()
	return info
}

func (t *nodeTunnel) acceptLoop() {
	defer t.wg.Done()
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			return
		}
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			t.handleLocal(conn)
		}()
	}
}

func (t *nodeTunnel) handleLocal(local net.Conn) {
	streamID, err := randomID("stream-")
	if err != nil {
		_ = local.Close()
		return
	}
	pending := &pendingTunnelStream{
		id: streamID, nodeID: t.info.NodeID, tunnel: t,
		ready: make(chan tunnelAttachment, 1), done: make(chan struct{}),
	}
	t.hub.mu.Lock()
	if t.hub.closed {
		t.hub.mu.Unlock()
		_ = local.Close()
		return
	}
	t.hub.pendingStreams[streamID] = pending
	t.hub.mu.Unlock()
	defer func() {
		t.hub.mu.Lock()
		if t.hub.pendingStreams[streamID] == pending {
			delete(t.hub.pendingStreams, streamID)
		}
		t.hub.mu.Unlock()
		pending.close()
	}()
	peer, ok := t.hub.Peer(t.info.NodeID)
	if !ok {
		_ = local.Close()
		return
	}
	data, _ := json.Marshal(map[string]any{
		"stream_id": streamID, "target_port": t.info.RemotePort, "service_id": t.info.ServiceID,
	})
	if err := peer.(*nodePeer).writeFrame(wireFrame{Event: EventOpenTunnel, Data: data}); err != nil {
		_ = local.Close()
		return
	}
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	var remote net.Conn
	select {
	case attachment := <-pending.ready:
		remote = attachment.conn
	case <-pending.done:
	case <-t.done:
	case <-timer.C:
	}
	if remote == nil {
		select {
		case late := <-pending.ready:
			_ = late.conn.Close()
		default:
		}
		_ = local.Close()
		return
	}
	t.track(local, remote)
	defer t.untrack(local, remote)
	copyDone := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remote, local)
		copyDone <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(local, remote)
		copyDone <- struct{}{}
	}()
	<-copyDone
	_ = local.Close()
	_ = remote.Close()
	<-copyDone
}

func (h *hub) handleTunnelStream(conn *tls.Conn, reader *bufio.Reader, streamID string) {
	nodeID, err := h.authenticatedNode(conn)
	if err != nil || !validStreamID(streamID) {
		_ = conn.Close()
		return
	}
	h.mu.Lock()
	pending := h.pendingStreams[streamID]
	if pending != nil && pending.nodeID == nodeID {
		delete(h.pendingStreams, streamID)
	} else {
		pending = nil
	}
	h.mu.Unlock()
	if pending == nil {
		_ = conn.Close()
		return
	}
	attachment := tunnelAttachment{conn: &bufferedConn{Conn: conn, reader: reader}}
	select {
	case pending.ready <- attachment:
	case <-pending.done:
		_ = conn.Close()
	}
}

func (t *nodeTunnel) track(conns ...net.Conn) {
	t.mu.Lock()
	for _, conn := range conns {
		t.conns[conn] = struct{}{}
	}
	t.mu.Unlock()
	t.active.Add(1)
}

func (t *nodeTunnel) untrack(conns ...net.Conn) {
	_ = conns[0].Close()
	_ = conns[1].Close()
	t.mu.Lock()
	for _, conn := range conns {
		delete(t.conns, conn)
	}
	t.mu.Unlock()
	t.active.Add(-1)
}

func (t *nodeTunnel) close() error {
	t.closeOnce.Do(func() {
		close(t.done)
		_ = t.listener.Close()
		t.mu.Lock()
		for conn := range t.conns {
			_ = conn.Close()
		}
		t.mu.Unlock()
		info := t.snapshot()
		t.hub.events.emit(HubEvent{Type: HubEventTunnelClosed, NodeID: t.info.NodeID, Tunnel: &info})
	})
	t.wg.Wait()
	return nil
}

func validStreamID(streamID string) bool {
	if len(streamID) < 1 || len(streamID) > 128 {
		return false
	}
	for _, r := range streamID {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}
