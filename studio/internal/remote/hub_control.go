package remote

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"slices"
	"time"
)

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
