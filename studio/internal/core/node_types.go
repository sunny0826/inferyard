package core

import (
	"encoding/json"
	"inferyard/studio/internal/remote"
	"strings"
	"sync"
	"time"
)

const (
	LocalNodeID        = "local"
	NodeTransportLocal = "local"
	NodeTransportMTLS  = "mtls"
	NodeTransportSSH   = "ssh_reverse"
)

type NodeRecord struct {
	NodeID          string     `json:"node_id"`
	DisplayName     string     `json:"display_name"`
	Hostname        string     `json:"hostname"`
	OS              string     `json:"os"`
	Arch            string     `json:"arch"`
	Platform        string     `json:"platform"`
	CLIVersion      string     `json:"cli_version"`
	HardwareSummary string     `json:"hardware_summary"`
	Transport       string     `json:"transport"`
	SSHCommandSafe  string     `json:"ssh_command_safe,omitempty"`
	PairedAt        time.Time  `json:"paired_at"`
	LastSeenAt      *time.Time `json:"last_seen_at,omitempty"`
	Revoked         bool       `json:"revoked"`
}

type RemoteNodeJob struct {
	Task     RunTask      `json:"task"`
	Progress *RunProgress `json:"progress,omitempty"`
}

type RemoteNodeRuntime struct {
	mu             sync.RWMutex
	NodeID         string              `json:"node_id"`
	Online         bool                `json:"online"`
	Refreshing     bool                `json:"refreshing"`
	DeviceSnapshot DeviceResponse      `json:"device_snapshot"`
	AssetsSnapshot AssetsState         `json:"assets_snapshot"`
	Services       []EngineState       `json:"services"`
	ExitNotices    []ServiceExitNotice `json:"exit_notices"`
	Jobs           []RemoteNodeJob     `json:"jobs"`
	Tunnels        []remote.TunnelInfo `json:"tunnels"`
	CLIUpgrade     CLIUpgradeState     `json:"cli_upgrade"`
}

type NodeSnapshot struct {
	Record         NodeRecord          `json:"record"`
	Online         bool                `json:"online"`
	Refreshing     bool                `json:"refreshing"`
	DeviceSnapshot DeviceResponse      `json:"device_snapshot"`
	AssetsSnapshot AssetsState         `json:"assets_snapshot"`
	Services       []EngineState       `json:"services"`
	ExitNotices    []ServiceExitNotice `json:"exit_notices"`
	Jobs           []RemoteNodeJob     `json:"jobs"`
	Tunnels        []remote.TunnelInfo `json:"tunnels"`
	CLIUpgrade     CLIUpgradeState     `json:"cli_upgrade"`
}

type NodeServiceStartRequest = StandaloneStartRequest
type NodeBenchmarkStartRequest = StartRunsRequest
type NodeBenchmarkResumeRequest = ResumeRunRequest

type NodeOperation struct {
	OperationID string          `json:"operation_id,omitempty"`
	State       string          `json:"state,omitempty"`
	Detail      json.RawMessage `json:"detail,omitempty"`
}

func validNodeID(nodeID string) bool {
	return validRemoteCacheID(nodeID) && nodeID != LocalNodeID
}

func validRemoteCacheID(value string) bool {
	if value == "" || value == "." || value == ".." || len(value) > 128 {
		return false
	}
	for i, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || i > 0 && r == '.' {
			continue
		}
		return false
	}
	return true
}

func validOpaqueRemotePath(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsRune(value, 0)
}

func cloneRemoteJob(job RemoteNodeJob) RemoteNodeJob {
	return RemoteNodeJob{Task: cloneRunTask(job.Task), Progress: cloneRunProgress(job.Progress)}
}

func snapshotRemoteRuntime(runtime *RemoteNodeRuntime) NodeSnapshot {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	jobs := make([]RemoteNodeJob, len(runtime.Jobs))
	for i, job := range runtime.Jobs {
		jobs[i] = cloneRemoteJob(job)
	}
	return NodeSnapshot{
		Online:         runtime.Online,
		Refreshing:     runtime.Refreshing,
		DeviceSnapshot: clone(runtime.DeviceSnapshot),
		AssetsSnapshot: cloneAssetsState(runtime.AssetsSnapshot),
		Services:       clone(runtime.Services),
		ExitNotices:    clone(runtime.ExitNotices),
		Jobs:           jobs,
		Tunnels:        clone(runtime.Tunnels),
		CLIUpgrade:     clone(runtime.CLIUpgrade),
	}
}

func nodeEventError(err error) *ApiError {
	if err == nil {
		return nil
	}
	switch err {
	case remote.ErrPeerOffline:
		return Err("node_offline")
	case remote.ErrTunnelNotFound:
		return Err("not_found")
	case remote.ErrInvalidOption, remote.ErrInvalidSSHCommand:
		return Err("invalid_request")
	case remote.ErrPairingExpired, remote.ErrPairingRejected, remote.ErrHostKeyRejected:
		return Conflict("request_rejected")
	}
	if rpc, ok := err.(*remote.RPCError); ok {
		return &ApiError{Code: rpc.Code, Message: rpc.Message, Details: rpc.Details, Limitations: []string{}}
	}
	return Err("remote_rpc_failed")
}
