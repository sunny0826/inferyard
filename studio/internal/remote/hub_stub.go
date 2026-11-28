package remote

import (
	"fmt"
	"strings"
)

func NewHub(cfg HubConfig) (Hub, error) {
	if strings.TrimSpace(cfg.DataDir) == "" {
		return nil, fmt.Errorf("remote hub data directory is required")
	}
	if cfg.HTTPJoinAddr == "" {
		cfg.HTTPJoinAddr = fmt.Sprintf(":%d", DefaultHubPort+1)
	}
	if cfg.TunnelPortMin == 0 {
		cfg.TunnelPortMin = DefaultTunnelMin
	}
	if cfg.TunnelPortMax == 0 {
		cfg.TunnelPortMax = DefaultTunnelMax
	}
	if cfg.TunnelPortMin < 1 || cfg.TunnelPortMax > 65535 || cfg.TunnelPortMin > cfg.TunnelPortMax {
		return nil, fmt.Errorf("invalid tunnel port range")
	}
	ca, err := loadCertificateAuthority(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	return &hub{
		cfg: cfg, ca: ca,
		pairRequests:   make(map[string]*pendingPairing),
		peers:          make(map[string]*nodePeer),
		tunnels:        make(map[string]*nodeTunnel),
		pendingStreams: make(map[string]*pendingTunnelStream),
		hostRequests:   make(map[string]*pendingHostKey),
		sshSessions:    make(map[string]*sshBridgeSession),
	}, nil
}
