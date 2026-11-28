package remote

import "context"

// NewHub constructs a Hub instance. Gate 0 ships a minimal stub so downstream
// packages compile before Lane W1-B replaces this file with the real Hub.
func NewHub(cfg HubConfig) (Hub, error) {
	return &noopHub{cfg: cfg}, nil
}

type noopHub struct {
	cfg HubConfig
}

func (h *noopHub) Start(context.Context) error { return nil }
func (h *noopHub) Close() error                { return nil }
func (h *noopHub) Addr() string                { return "" }
func (h *noopHub) HTTPJoinAddr() string        { return "" }
func (h *noopHub) CASha256() string            { return "" }

func (h *noopHub) StartPairingSession(context.Context, PairingSessionOptions) (PairingSession, error) {
	return PairingSession{}, ErrNotImplemented
}
func (h *noopHub) ActivePairingSession() (PairingSession, bool) { return PairingSession{}, false }
func (h *noopHub) CancelPairingSession(string) error            { return ErrNotImplemented }
func (h *noopHub) PendingPairingRequests() []PairingRequest     { return nil }
func (h *noopHub) DecidePairing(PairingDecision) (PairedNodeCredentials, error) {
	return PairedNodeCredentials{}, ErrNotImplemented
}

func (h *noopHub) Peers() []NodePeer            { return nil }
func (h *noopHub) Peer(string) (NodePeer, bool) { return nil, false }
func (h *noopHub) RevokeNode(string) error      { return ErrNotImplemented }

func (h *noopHub) OpenTunnel(context.Context, OpenTunnelRequest) (TunnelInfo, error) {
	return TunnelInfo{}, ErrNotImplemented
}
func (h *noopHub) CloseTunnel(string) error { return ErrNotImplemented }
func (h *noopHub) Tunnels() []TunnelInfo    { return nil }

func (h *noopHub) ParseSSHCommand(string) (ParsedSSHCommand, error) {
	return ParsedSSHCommand{}, ErrNotImplemented
}
func (h *noopHub) ConnectSSH(context.Context, SSHConnectRequest) (SSHSession, error) {
	return SSHSession{}, ErrNotImplemented
}
func (h *noopHub) PendingHostKeyRequests() []HostKeyRequest { return nil }
func (h *noopHub) DecideHostKey(HostKeyDecision) error      { return ErrNotImplemented }
func (h *noopHub) DisconnectSSH(string) error               { return ErrNotImplemented }

func (h *noopHub) SubscribeEvents() (<-chan HubEvent, func()) {
	ch := make(chan HubEvent)
	return ch, func() { close(ch) }
}
