package remote

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type pendingHostKey struct {
	request HostKeyRequest
	key     ssh.PublicKey
	result  chan error
	once    sync.Once
}

func (p *pendingHostKey) finish(err error) { p.once.Do(func() { p.result <- err }) }

func (h *hub) knownHostsPath() string {
	if h.cfg.KnownHostsPath != "" {
		return h.cfg.KnownHostsPath
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "known_hosts")
}

func (h *hub) hostKeyCallback(parsed ParsedSSHCommand) (ssh.HostKeyCallback, error) {
	path := h.knownHostsPath()
	var knownCallback ssh.HostKeyCallback
	if _, err := os.Stat(path); err == nil {
		var err error
		knownCallback, err = knownhosts.New(path)
		if err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if knownCallback != nil {
			err := knownCallback(hostname, remote, key)
			if err == nil {
				return nil
			}
			var keyError *knownhosts.KeyError
			if !errors.As(err, &keyError) || len(keyError.Want) != 0 {
				return err
			}
		}
		requestID, err := randomID("hostkey-")
		if err != nil {
			return err
		}
		request := HostKeyRequest{
			RequestID: requestID, Host: parsed.Host, Port: parsed.Port, KeyType: key.Type(),
			FingerprintSHA256: ssh.FingerprintSHA256(key), KnownHostsPath: path,
			RequestedAt: time.Now(), Options: []HostKeyOption{HostKeyTrustOnce, HostKeyTrustAndSave, HostKeyReject},
		}
		pending := &pendingHostKey{request: request, key: key, result: make(chan error, 1)}
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return ErrHostKeyRejected
		}
		h.hostRequests[requestID] = pending
		h.mu.Unlock()
		h.events.emit(HubEvent{Type: HubEventHostKeyRequested, HostKey: &request})
		select {
		case err := <-pending.result:
			return err
		case <-h.context().Done():
			return ErrHostKeyRejected
		}
	}, nil
}

func (h *hub) PendingHostKeyRequests() []HostKeyRequest {
	h.mu.RLock()
	defer h.mu.RUnlock()
	requests := make([]HostKeyRequest, 0, len(h.hostRequests))
	for _, pending := range h.hostRequests {
		requests = append(requests, pending.request)
	}
	slices.SortFunc(requests, func(a, b HostKeyRequest) int { return a.RequestedAt.Compare(b.RequestedAt) })
	return requests
}

func (h *hub) DecideHostKey(decision HostKeyDecision) error {
	if decision.Option != HostKeyTrustOnce && decision.Option != HostKeyTrustAndSave && decision.Option != HostKeyReject {
		return ErrInvalidOption
	}
	h.mu.Lock()
	pending := h.hostRequests[decision.RequestID]
	if pending != nil {
		delete(h.hostRequests, decision.RequestID)
	}
	h.mu.Unlock()
	if pending == nil {
		return fmt.Errorf("unknown SSH host key request")
	}
	var err error
	switch decision.Option {
	case HostKeyReject:
		err = ErrHostKeyRejected
	case HostKeyTrustAndSave:
		err = h.saveHostKey(pending)
	}
	pending.finish(err)
	h.events.emit(HubEvent{Type: HubEventHostKeyResolved, HostKey: &pending.request, Reason: string(decision.Option)})
	return err
}

func (h *hub) saveHostKey(pending *pendingHostKey) error {
	request := pending.request
	path := request.KnownHostsPath
	if path == "" {
		return fmt.Errorf("known hosts path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	line := knownhosts.Line([]string{sshAddress(request.Host, request.Port)}, pending.key)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	_, err = file.WriteString(line + "\n")
	return err
}
