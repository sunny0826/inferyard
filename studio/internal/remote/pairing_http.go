package remote

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"
)

func listenerPort(addr string) int {
	_, rawPort, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	var port int
	_, _ = fmt.Sscanf(rawPort, "%d", &port)
	return port
}

func firstCandidateHost(addrs []string) string {
	if len(addrs) == 0 {
		return "127.0.0.1"
	}
	host, _, err := net.SplitHostPort(addrs[0])
	if err == nil {
		return host
	}
	return "127.0.0.1"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sourceHint(host string) string {
	ip := net.ParseIP(host)
	if isTailscaleIP(ip) {
		return "Tailscale"
	}
	if ip != nil && ip.IsPrivate() {
		return "LAN"
	}
	return ""
}

func isTailscaleIP(ip net.IP) bool {
	_, network, _ := net.ParseCIDR("100.64.0.0/10")
	return ip != nil && network.Contains(ip)
}

func (h *hub) candidateHosts() []string {
	if len(h.cfg.CandidateHosts) > 0 {
		var hosts []string
		for _, host := range h.cfg.CandidateHosts {
			host = strings.TrimSpace(host)
			if parsedHost, _, err := net.SplitHostPort(host); err == nil {
				host = parsedHost
			}
			if net.ParseIP(strings.Trim(host, "[]")) != nil || validDNSName(host) {
				hosts = append(hosts, strings.Trim(host, "[]"))
			}
		}
		return hosts
	}
	var tailscale, lan []string
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil || ip.To4() == nil || ip.IsLoopback() {
				continue
			}
			if isTailscaleIP(ip) {
				tailscale = append(tailscale, ip.String())
			} else {
				lan = append(lan, ip.String())
			}
		}
	}
	slices.Sort(tailscale)
	slices.Sort(lan)
	hosts := append(tailscale, lan...)
	if hostname, err := os.Hostname(); err == nil {
		hosts = append(hosts, hostname+".local")
	}
	if len(hosts) == 0 {
		hosts = append(hosts, "127.0.0.1")
	}
	return hosts
}

func (h *hub) candidateAddresses(port int) []string {
	hosts := h.candidateHosts()
	addresses := make([]string, 0, len(hosts))
	for _, host := range hosts {
		addresses = append(addresses, net.JoinHostPort(host, fmt.Sprint(port)))
	}
	return addresses
}

func (h *hub) joinHTTPHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		path := strings.TrimPrefix(request.URL.Path, "/join/")
		if path == request.URL.Path {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		windows := strings.HasSuffix(path, ".ps1")
		code := strings.TrimSuffix(path, ".ps1")
		normalized, err := normalizePairCode(code)
		if err != nil {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		h.mu.RLock()
		active := h.pairing
		valid := active != nil && active.session.Code == normalized && time.Now().Before(active.session.ExpiresAt)
		var session PairingSession
		if valid {
			session = clonePairingSession(active.session)
		}
		h.mu.RUnlock()
		if !valid {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		if windows {
			writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = writer.Write([]byte(session.JoinScriptWin))
		} else {
			writer.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
			_, _ = writer.Write([]byte(session.JoinScriptUnix))
		}
	})
}
