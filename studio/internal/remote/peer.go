package remote

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type rpcResponse struct {
	result json.RawMessage
	err    *RPCError
}

type nodePeer struct {
	hub       *hub
	nodeID    string
	conn      *tls.Conn
	reader    *bufio.Reader
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan rpcResponse
	done      chan struct{}
	closeOnce sync.Once

	infoMu sync.RWMutex
	info   NodePeerInfo
}

func newNodePeer(h *hub, nodeID string, conn *tls.Conn, reader *bufio.Reader) *nodePeer {
	return &nodePeer{
		hub: h, nodeID: nodeID, conn: conn, reader: reader,
		pending: make(map[string]chan rpcResponse), done: make(chan struct{}),
		info: NodePeerInfo{NodeID: nodeID},
	}
}

func (p *nodePeer) NodeID() string { return p.nodeID }

func (p *nodePeer) Info() NodePeerInfo {
	p.infoMu.RLock()
	defer p.infoMu.RUnlock()
	info := p.info
	if p.info.DeviceSnapshot != nil {
		info.DeviceSnapshot = append(json.RawMessage(nil), p.info.DeviceSnapshot...)
	}
	return info
}

func (p *nodePeer) Connected() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *nodePeer) Call(ctx context.Context, method string, params any, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if method == "" {
		return fmt.Errorf("RPC method is required")
	}
	if !p.Connected() {
		return ErrPeerOffline
	}
	requestID, err := randomID("rpc-")
	if err != nil {
		return err
	}
	var rawParams json.RawMessage
	if params != nil {
		rawParams, err = json.Marshal(params)
		if err != nil {
			return err
		}
	}
	responseCh := make(chan rpcResponse, 1)
	p.pendingMu.Lock()
	if !p.Connected() {
		p.pendingMu.Unlock()
		return ErrPeerOffline
	}
	p.pending[requestID] = responseCh
	p.pendingMu.Unlock()
	defer p.removePending(requestID)
	if err := p.writeFrame(wireFrame{ID: requestID, Method: method, Params: rawParams}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return ErrPeerOffline
	case response := <-responseCh:
		if response.err != nil {
			return response.err
		}
		if result != nil && len(response.result) > 0 {
			if err := json.Unmarshal(response.result, result); err != nil {
				return fmt.Errorf("decode RPC result: %w", err)
			}
		}
		return nil
	}
}

func (p *nodePeer) Close() error {
	err := p.conn.Close()
	p.shutdown("closed")
	return err
}

func (p *nodePeer) writeFrame(frame wireFrame) error {
	data, err := encodeWireFrame(frame)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	select {
	case <-p.done:
		return ErrPeerOffline
	default:
	}
	_, err = p.conn.Write(data)
	return err
}

func (p *nodePeer) readLoop() {
	go p.heartbeatLoop()
	defer p.shutdown("connection closed")
	for {
		frame, err := decodeWireLine(p.reader)
		if err != nil {
			return
		}
		p.infoMu.Lock()
		p.info.LastHeartbeatAt = time.Now()
		p.infoMu.Unlock()
		if frame.ID != "" && frame.Method == "" && frame.Event == "" {
			p.pendingMu.Lock()
			responseCh := p.pending[frame.ID]
			p.pendingMu.Unlock()
			if responseCh != nil {
				responseCh <- rpcResponse{result: frame.Result, err: frame.Error}
			}
			continue
		}
		if frame.Event == EventPing {
			_ = p.writeFrame(wireFrame{Event: EventPong, Data: frame.Data})
		}
		if frame.Event == "" {
			continue
		}
		payload := append(json.RawMessage(nil), frame.Data...)
		p.hub.events.emit(HubEvent{
			Type: HubEventPeerEvent, NodeID: p.nodeID, EventName: frame.Event, Payload: payload,
		})
	}
}

func (p *nodePeer) heartbeatLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			if err := p.writeFrame(wireFrame{Event: EventPing, Data: json.RawMessage(`{}`)}); err != nil {
				_ = p.Close()
				return
			}
		}
	}
}

func (p *nodePeer) removePending(requestID string) {
	p.pendingMu.Lock()
	delete(p.pending, requestID)
	p.pendingMu.Unlock()
}

func (p *nodePeer) shutdown(reason string) {
	p.closeOnce.Do(func() {
		close(p.done)
		_ = p.conn.Close()
		p.pendingMu.Lock()
		p.pending = make(map[string]chan rpcResponse)
		p.pendingMu.Unlock()
		p.hub.removePeer(p, reason)
	})
}
