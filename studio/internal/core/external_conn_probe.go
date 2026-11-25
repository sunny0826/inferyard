package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	ExclusivityExclusive = "exclusive"
	ExclusivityExternal  = "external"
	ExclusivityUnknown   = "unknown"

	exclusivityProbeTimeout = 2 * time.Second
)

type ExternalConnProbe struct {
	Status  string   `json:"status"`            // exclusive | external | unknown
	Clients []string `json:"clients,omitempty"` // e.g. ["Cherry Studio (PID 4182)"]
	Count   int      `json:"count,omitempty"`
	Port    int      `json:"port,omitempty"`
}

type lsofExecFunc func(ctx context.Context, port int) (stdout, stderr []byte, exitCode int, err error)

var (
	lsofRunner         lsofExecFunc = runLsofEstablished
	probePortFunc                   = probePortWithLsof
	descendantPIDsFunc              = descendantPIDsOf
)

// ProbePortExternalConnections inspects ESTABLISHED TCP connections on port and reports
// whether any process outside excludePIDs and the current Studio process is connected.
// When probing is unsupported or fails, Status is strictly ExclusivityUnknown (never exclusive).
func ProbePortExternalConnections(ctx context.Context, port int, excludePIDs []int) ExternalConnProbe {
	if port <= 0 || port > 65535 {
		return ExternalConnProbe{Status: ExclusivityUnknown, Port: port}
	}
	return probePortFunc(ctx, port, excludePIDs)
}

func probePortWithLsof(ctx context.Context, port int, excludePIDs []int) ExternalConnProbe {
	if runtime.GOOS == "windows" {
		return ExternalConnProbe{Status: ExclusivityUnknown, Port: port}
	}
	return probePortUsingRunner(ctx, port, excludePIDs, lsofRunner)
}

func probePortUsingRunner(ctx context.Context, port int, excludePIDs []int, runner lsofExecFunc) ExternalConnProbe {
	if port <= 0 || port > 65535 || runner == nil {
		return ExternalConnProbe{Status: ExclusivityUnknown, Port: port}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, exclusivityProbeTimeout)
	defer cancel()

	stdout, stderr, exitCode, err := runner(probeCtx, port)
	if err != nil {
		if probeCtx.Err() != nil || errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return ExternalConnProbe{Status: ExclusivityUnknown, Port: port}
		}
		// lsof exits with code 1 and empty stdout/stderr when no matching connections exist.
		if exitCode == 1 && len(bytes.TrimSpace(stdout)) == 0 && len(bytes.TrimSpace(stderr)) == 0 {
			return ExternalConnProbe{Status: ExclusivityExclusive, Port: port}
		}
		return ExternalConnProbe{Status: ExclusivityUnknown, Port: port}
	}
	if exitCode != 0 {
		return ExternalConnProbe{Status: ExclusivityUnknown, Port: port}
	}

	exclude := make(map[int]bool, len(excludePIDs)+4)
	selfPID := os.Getpid()
	exclude[selfPID] = true
	if descendantPIDsFunc != nil {
		for _, pid := range descendantPIDsFunc(selfPID) {
			if pid > 0 {
				exclude[pid] = true
			}
		}
	}
	for _, pid := range excludePIDs {
		if pid > 0 {
			exclude[pid] = true
		}
	}

	clients := parseLsofEstablished(stdout, exclude)
	if len(clients) > 0 {
		return ExternalConnProbe{
			Status:  ExclusivityExternal,
			Clients: clients,
			Count:   len(clients),
			Port:    port,
		}
	}
	return ExternalConnProbe{Status: ExclusivityExclusive, Port: port}
}

func runLsofEstablished(ctx context.Context, port int) ([]byte, []byte, int, error) {
	lsofPath, err := exec.LookPath("lsof")
	if err != nil {
		return nil, nil, -1, err
	}
	cmd := exec.CommandContext(
		ctx,
		lsofPath,
		"-nP",
		fmt.Sprintf("-iTCP:%d", port),
		"-sTCP:ESTABLISHED",
		"-F", "pcn",
	)
	var stdout, stderr boundedProcessOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.Bytes(), stderr.Bytes(), exitErr.ExitCode(), err
		}
		return stdout.Bytes(), stderr.Bytes(), -1, err
	}
	return stdout.Bytes(), stderr.Bytes(), 0, nil
}

func parseLsofEstablished(output []byte, exclude map[int]bool) []string {
	byPID := map[int]string{}
	remoteHosts := map[string]bool{}
	currentPID := 0
	currentCmd := ""
	flush := func() {
		if currentPID <= 0 || exclude[currentPID] {
			return
		}
		if _, exists := byPID[currentPID]; !exists || byPID[currentPID] == "" {
			byPID[currentPID] = currentCmd
		}
	}
	for _, rawLine := range strings.Split(string(output), "\n") {
		line := strings.TrimRight(rawLine, "\r")
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			flush()
			pid, err := strconv.Atoi(strings.TrimSpace(line[1:]))
			if err != nil || pid <= 0 {
				currentPID = 0
			} else {
				currentPID = pid
			}
			currentCmd = ""
		case 'c':
			if currentPID > 0 {
				currentCmd = unescapeLsofField(strings.TrimSpace(line[1:]))
			}
		case 'n':
			if currentPID > 0 && exclude[currentPID] {
				if host, nonLoopback := nonLoopbackRemoteHost(strings.TrimSpace(line[1:])); nonLoopback {
					remoteHosts[host] = true
				}
			}
		}
	}
	flush()
	if len(byPID) == 0 && len(remoteHosts) == 0 {
		return nil
	}
	pids := make([]int, 0, len(byPID))
	for pid := range byPID {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	clients := make([]string, 0, len(pids)+len(remoteHosts))
	for _, pid := range pids {
		name := strings.TrimSpace(byPID[pid])
		if name == "" {
			clients = append(clients, fmt.Sprintf("PID %d", pid))
		} else {
			clients = append(clients, fmt.Sprintf("%s (PID %d)", name, pid))
		}
	}
	if len(remoteHosts) > 0 {
		hosts := make([]string, 0, len(remoteHosts))
		for host := range remoteHosts {
			hosts = append(hosts, host)
		}
		sort.Strings(hosts)
		for _, host := range hosts {
			clients = append(clients, fmt.Sprintf("LAN (%s)", host))
		}
	}
	return clients
}

func nonLoopbackRemoteHost(networkName string) (string, bool) {
	_, remote, ok := strings.Cut(strings.TrimSpace(networkName), "->")
	if !ok {
		return "", false
	}
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", false
	}
	host := remote
	if h, _, err := net.SplitHostPort(remote); err == nil {
		host = h
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if zoneIdx := strings.IndexByte(host, '%'); zoneIdx > 0 {
		host = host[:zoneIdx]
	}
	if host == "" {
		return "", false
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return "", false
		}
		return ip.String(), true
	}
	if strings.EqualFold(host, "localhost") {
		return "", false
	}
	return host, true
}

func unescapeLsofField(s string) string {
	if !strings.Contains(s, `\x`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if i+3 < len(s) && s[i] == '\\' && s[i+1] == 'x' {
			if val, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil {
				b.WriteByte(byte(val))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// ProbeServiceConnections probes external TCP connections on a managed service's port.
func (s *Service) ProbeServiceConnections(ctx context.Context, serviceID string) ExternalConnProbe {
	id := strings.TrimSpace(serviceID)
	if id == "" {
		return ExternalConnProbe{Status: ExclusivityUnknown}
	}
	s.mu.Lock()
	var entry *engineEntry
	if e, ok := s.services[id]; ok && e != nil {
		entry = e
	} else if s.engine != nil && s.engine.state.ServiceID == id {
		entry = s.engine
	}
	if entry == nil {
		s.mu.Unlock()
		return ExternalConnProbe{Status: ExclusivityUnknown}
	}
	port := entry.state.Port
	if port <= 0 {
		port = portFromEndpoint(entry.state.Endpoint)
	}
	exclude := entryProcessPIDsLocked(entry)
	s.mu.Unlock()
	if port <= 0 {
		return ExternalConnProbe{Status: ExclusivityUnknown}
	}
	return ProbePortExternalConnections(ctx, port, exclude)
}

// ProbeEndpointConnections probes external TCP connections on an endpoint URL's port.
// Optional extraExcludePIDs can supply the known PID(s) of a discovered external server.
func (s *Service) ProbeEndpointConnections(ctx context.Context, endpoint string, extraExcludePIDs ...int) ExternalConnProbe {
	port := portFromEndpoint(endpoint)
	if port <= 0 {
		return ExternalConnProbe{Status: ExclusivityUnknown}
	}
	exclude := s.managedPIDsOnPort(port)
	for _, pid := range extraExcludePIDs {
		if pid > 0 {
			exclude = append(exclude, pid)
		}
	}
	return ProbePortExternalConnections(ctx, port, exclude)
}

func (s *Service) managedPIDsOnPort(port int) []int {
	if port <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var pids []int
	collect := func(e *engineEntry) {
		if e == nil || !isActiveServiceState(e.state.State) {
			return
		}
		p := e.state.Port
		if p <= 0 {
			p = portFromEndpoint(e.state.Endpoint)
		}
		if p == port {
			pids = append(pids, entryProcessPIDsLocked(e)...)
		}
	}
	collect(s.engine)
	for _, e := range s.services {
		collect(e)
	}
	return pids
}
