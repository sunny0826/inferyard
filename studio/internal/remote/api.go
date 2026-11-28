package remote

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Wire protocol handshake lines and frame constants shared with Python CLI
// (cli/src/inferyard/contracts/node_protocol.py).
const (
	ProtocolVersion = 1

	HandshakePairPrefix    = "INFERYARD_PAIR 1"
	HandshakeControlPrefix = "INFERYARD_CONTROL 1"
	HandshakeTunnelPrefix  = "INFERYARD_TUNNEL 1 "

	DefaultHubPort      = 48860
	DefaultTunnelMin    = 18001
	DefaultTunnelMax    = 18099
	DefaultPairTTL      = 5 * time.Minute
	MaxControlLineBytes = 16 * 1024 * 1024 // 16 MiB (accommodates v3 studio.json export)
)

// Control-plane RPC method names (Studio -> Agent).
const (
	MethodNodeDeviceCheck   = "node.device_check"
	MethodNodeAssetsScan    = "node.assets_scan"
	MethodNodeEngineInstall = "node.engine_install"

	MethodServiceList  = "service.list"
	MethodServiceStart = "service.start"
	MethodServiceStop  = "service.stop"
	MethodServiceLogs  = "service.logs"

	MethodJobList     = "job.list"
	MethodJobStart    = "job.start"
	MethodJobCancel   = "job.cancel"
	MethodJobProgress = "job.progress"

	MethodReportList   = "report.list"
	MethodReportExport = "report.export"

	MethodWorkspaceClean = "workspace.clean"
)

// Control-plane one-way event names.
const (
	EventHello          = "hello"
	EventPing           = "ping"
	EventPong           = "pong"
	EventOpenTunnel     = "open_tunnel"
	EventServiceUpdated = "service.updated"
	EventServiceExited  = "service.exited"
	EventJobUpdated     = "job.updated"
	EventJobProgress    = "job.progress"
	EventDeviceUpdated  = "device.updated"
)

// Explicit option-based pairing decisions (must not degrade into bool).
type PairingOption string

const (
	PairingApprove PairingOption = "approve_pair"
	PairingReject  PairingOption = "reject_pair"
)

// Explicit option-based SSH host key decisions (must not degrade into bool).
type HostKeyOption string

const (
	HostKeyTrustOnce    HostKeyOption = "trust_once"
	HostKeyTrustAndSave HostKeyOption = "trust_and_save"
	HostKeyReject       HostKeyOption = "reject"
)

// Sentinel errors returned by Hub implementations.
var (
	ErrNotImplemented    = errors.New("remote hub not implemented")
	ErrPeerOffline       = errors.New("remote node peer offline")
	ErrPairingExpired    = errors.New("pairing session expired or unknown")
	ErrPairingRejected   = errors.New("pairing request rejected")
	ErrInvalidOption     = errors.New("invalid explicit decision option")
	ErrInvalidSSHCommand = errors.New("invalid or unsafe ssh command")
	ErrHostKeyRejected   = errors.New("ssh host key rejected")
	ErrTunnelNotFound    = errors.New("tunnel not found")
	ErrTunnelPortExhaust = errors.New("no available local tunnel ports")
)

// HubConfig configures the Studio embedded mTLS Hub and pairing HTTP endpoint.
type HubConfig struct {
	DataDir          string
	ListenAddr       string // e.g. ":0" or ":48860"
	HTTPJoinAddr     string // e.g. ":0" or ":48861"
	TunnelPortMin    int
	TunnelPortMax    int
	CLIVersion       string
	DownloadSource   string
	DownloadProxy    string
	CandidateHosts   []string // Optional override for deterministic tests
	KnownHostsPath   string
	DisableAutoStart bool
}

// PairingSessionOptions customizes a one-time pairing session.
type PairingSessionOptions struct {
	TTL            time.Duration
	CLIVersion     string
	DownloadSource string
	DownloadProxy  string
}

// PairingSession represents an active, single-use pairing code and join commands.
type PairingSession struct {
	Code            string    `json:"code"`
	FormattedCode   string    `json:"formatted_code"` // e.g. "839-204"
	CASha256        string    `json:"ca_sha256"`
	CandidateAddrs  []string  `json:"candidate_addrs"`
	UnixCommand     string    `json:"unix_command"`
	WindowsCommand  string    `json:"windows_command"`
	JoinScriptUnix  string    `json:"join_script_unix"`
	JoinScriptWin   string    `json:"join_script_win"`
	CreatedAt       time.Time `json:"created_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}

// PairingRequest is emitted when a remote machine dials in with a valid pairing code.
type PairingRequest struct {
	RequestID       string          `json:"request_id"`
	Code            string          `json:"code"`
	FormattedCode   string          `json:"formatted_code"`
	Hostname        string          `json:"hostname"`
	OS              string          `json:"os"`
	Arch            string          `json:"arch"`
	Platform        string          `json:"platform"` // e.g. "windows/amd64", "linux/amd64", "darwin/arm64"
	CLIVersion      string          `json:"cli_version"`
	HardwareSummary string          `json:"hardware_summary"`
	SourceIP        string          `json:"source_ip"`
	SourceHint      string          `json:"source_hint,omitempty"` // e.g. "Tailscale", "LAN"
	AutostartMode   string          `json:"autostart_mode,omitempty"`
	DeviceSnapshot  json.RawMessage `json:"device_snapshot,omitempty"`
	RequestedAt     time.Time       `json:"requested_at"`
	Options         []PairingOption `json:"options"`
}

// PairingDecision carries an explicit user choice for a pending PairingRequest.
type PairingDecision struct {
	RequestID   string        `json:"request_id"`
	Option      PairingOption `json:"option"`
	DisplayName string        `json:"display_name,omitempty"`
}

// PairedNodeCredentials describes the approved node identity (never includes private keys in JSON).
type PairedNodeCredentials struct {
	NodeID          string          `json:"node_id"`
	DisplayName     string          `json:"display_name"`
	Hostname        string          `json:"hostname"`
	OS              string          `json:"os"`
	Arch            string          `json:"arch"`
	Platform        string          `json:"platform"`
	CLIVersion      string          `json:"cli_version"`
	HardwareSummary string          `json:"hardware_summary"`
	CertSerial      string          `json:"cert_serial"`
	CertFingerprint string          `json:"cert_fingerprint"`
	PairedAt        time.Time       `json:"paired_at"`
	DeviceSnapshot  json.RawMessage `json:"device_snapshot,omitempty"`
}

// NodePeerInfo summarizes a connected remote node peer.
type NodePeerInfo struct {
	NodeID             string          `json:"node_id"`
	Hostname           string          `json:"hostname"`
	OS                 string          `json:"os"`
	Arch               string          `json:"arch"`
	Platform           string          `json:"platform"`
	CLIVersion         string          `json:"cli_version"`
	HardwareSummary    string          `json:"hardware_summary"`
	RemoteAddr         string          `json:"remote_addr"`
	Transport          string          `json:"transport"` // "mtls" | "ssh_reverse"
	AutostartInstalled bool            `json:"autostart_installed"`
	ConnectedAt        time.Time       `json:"connected_at"`
	LastHeartbeatAt    time.Time       `json:"last_heartbeat_at"`
	DeviceSnapshot     json.RawMessage `json:"device_snapshot,omitempty"`
}

// OpenTunnelRequest asks the Hub to expose a remote loopback port locally on 127.0.0.1.
type OpenTunnelRequest struct {
	NodeID      string `json:"node_id"`
	NodeName    string `json:"node_name,omitempty"`
	ServiceID   string `json:"service_id"`
	ModelName   string `json:"model_name,omitempty"`
	EngineName  string `json:"engine_name,omitempty"`
	RemotePort  int    `json:"remote_port"`
	LocalPort   int    `json:"local_port,omitempty"` // 0 = auto-allocate in [18001, 18099]
}

// TunnelInfo describes an active local OpenAI port forward backed by on-demand mTLS TUNNEL streams.
type TunnelInfo struct {
	TunnelID    string    `json:"tunnel_id"`
	NodeID      string    `json:"node_id"`
	NodeName    string    `json:"node_name,omitempty"`
	ServiceID   string    `json:"service_id"`
	ModelName   string    `json:"model_name,omitempty"`
	EngineName  string    `json:"engine_name,omitempty"`
	LocalPort   int       `json:"local_port"`
	LocalURL    string    `json:"local_url"` // e.g. "http://127.0.0.1:18001/v1"
	RemotePort  int       `json:"remote_port"`
	ActiveConns int64     `json:"active_conns"`
	CreatedAt   time.Time `json:"created_at"`
}

// ParsedSSHCommand is the sanitized result of parsing a pasted cloud `ssh ...` command.
type ParsedSSHCommand struct {
	User         string `json:"user"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	IdentityFile string `json:"identity_file,omitempty"`
	ProxyJump    string `json:"proxy_jump,omitempty"`
	RawTarget    string `json:"raw_target"`
}

// SSHConnectRequest initiates cloud GPU onboarding via a pasted `ssh ...` command.
// Password is kept in memory only for first-time public-key installation and is never persisted.
type SSHConnectRequest struct {
	Command        string `json:"command"`
	DisplayName    string `json:"display_name,omitempty"`
	Password       string `json:"-"` // In-memory only; never marshaled or persisted
	CLIVersion     string `json:"cli_version,omitempty"`
	DownloadSource string `json:"download_source,omitempty"`
	DownloadProxy  string `json:"download_proxy,omitempty"`
}

// HostKeyRequest asks the user for an explicit decision on an unknown SSH host key.
type HostKeyRequest struct {
	RequestID         string          `json:"request_id"`
	Host              string          `json:"host"`
	Port              int             `json:"port"`
	KeyType           string          `json:"key_type"`
	FingerprintSHA256 string          `json:"fingerprint_sha256"`
	KnownHostsPath    string          `json:"known_hosts_path,omitempty"`
	RequestedAt       time.Time       `json:"requested_at"`
	Options           []HostKeyOption `json:"options"`
}

// HostKeyDecision carries an explicit user choice for a pending HostKeyRequest.
type HostKeyDecision struct {
	RequestID string        `json:"request_id"`
	Option    HostKeyOption `json:"option"`
}

// SSHSession describes an active SSH reverse-tunnel bridge session.
type SSHSession struct {
	NodeID           string    `json:"node_id"`
	Host             string    `json:"host"`
	Port             int       `json:"port"`
	User             string    `json:"user"`
	RemoteTunnelPort int       `json:"remote_tunnel_port"`
	ConnectedAt      time.Time `json:"connected_at"`
}

// RPCError is a structured error returned over the control plane.
type RPCError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Code + ": " + e.Message
	}
	return e.Code
}

// HubEventType classifies asynchronous Hub lifecycle and peer events.
type HubEventType string

const (
	HubEventPairingRequested HubEventType = "pairing.requested"
	HubEventPairingResolved  HubEventType = "pairing.resolved"
	HubEventHostKeyRequested HubEventType = "ssh.hostkey_requested"
	HubEventHostKeyResolved  HubEventType = "ssh.hostkey_resolved"
	HubEventPeerConnected    HubEventType = "peer.connected"
	HubEventPeerDisconnected HubEventType = "peer.disconnected"
	HubEventPeerEvent        HubEventType = "peer.event"
	HubEventTunnelOpened     HubEventType = "tunnel.opened"
	HubEventTunnelClosed     HubEventType = "tunnel.closed"
)

// HubEvent carries asynchronous notifications from the Hub to core.NodeManager.
type HubEvent struct {
	Type           HubEventType    `json:"type"`
	NodeID         string          `json:"node_id,omitempty"`
	EventName      string          `json:"event_name,omitempty"`
	Peer           *NodePeerInfo   `json:"peer,omitempty"`
	Pairing        *PairingRequest `json:"pairing,omitempty"`
	HostKey        *HostKeyRequest `json:"host_key,omitempty"`
	Tunnel         *TunnelInfo     `json:"tunnel,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	Reason         string          `json:"reason,omitempty"`
	Timestamp      time.Time       `json:"timestamp"`
}

// NodePeer represents an authenticated control-plane connection to a single remote node.
type NodePeer interface {
	NodeID() string
	Info() NodePeerInfo
	Connected() bool
	Call(ctx context.Context, method string, params any, result any) error
	Close() error
}

// Hub is the Studio-side remote control hub, CA, pairing server, tunnel pool, and SSH bridge.
type Hub interface {
	Start(ctx context.Context) error
	Close() error
	Addr() string
	HTTPJoinAddr() string
	CASha256() string

	StartPairingSession(ctx context.Context, opts PairingSessionOptions) (PairingSession, error)
	ActivePairingSession() (PairingSession, bool)
	CancelPairingSession(code string) error
	PendingPairingRequests() []PairingRequest
	DecidePairing(decision PairingDecision) (PairedNodeCredentials, error)

	Peers() []NodePeer
	Peer(nodeID string) (NodePeer, bool)
	RevokeNode(nodeID string) error

	OpenTunnel(ctx context.Context, req OpenTunnelRequest) (TunnelInfo, error)
	CloseTunnel(tunnelID string) error
	Tunnels() []TunnelInfo

	ParseSSHCommand(raw string) (ParsedSSHCommand, error)
	ConnectSSH(ctx context.Context, req SSHConnectRequest) (SSHSession, error)
	PendingHostKeyRequests() []HostKeyRequest
	DecideHostKey(decision HostKeyDecision) error
	DisconnectSSH(nodeID string) error

	SubscribeEvents() (<-chan HubEvent, func())
}
