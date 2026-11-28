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
	ip4 := ip.To4()
	return ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
}

func isFilteredCandidateIPv4(ip4 net.IP) bool {
	if ip4 == nil || ip4.IsLoopback() || ip4.IsUnspecified() || ip4.IsMulticast() ||
		ip4.IsLinkLocalUnicast() || ip4.IsLinkLocalMulticast() || ip4.Equal(net.IPv4bcast) {
		return true
	}
	// RFC 2544 benchmarking subnet (198.18.0.0/15), widely used by TUN proxies (e.g. 198.18.0.1 Fake-IP).
	if ip4[0] == 198 && (ip4[1]&0xfe) == 18 {
		return true
	}
	// RFC 5737 documentation subnets (192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24).
	if (ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 2) ||
		(ip4[0] == 198 && ip4[1] == 51 && ip4[2] == 100) ||
		(ip4[0] == 203 && ip4[1] == 0 && ip4[2] == 113) {
		return true
	}
	return false
}

func candidateHostname(raw string) string {
	host := strings.TrimRight(strings.TrimSpace(raw), ".")
	if host == "" || net.ParseIP(host) != nil {
		return ""
	}
	if strings.HasSuffix(strings.ToLower(host), ".local") {
		if validDNSName(host) {
			return host
		}
		return ""
	}
	if !strings.Contains(host, ".") {
		candidate := host + ".local"
		if validDNSName(candidate) {
			return candidate
		}
		return ""
	}
	if validDNSName(host) {
		return host
	}
	return ""
}

type candidateInterface struct {
	flags net.Flags
	addrs []string
}

func collectCandidateHosts(interfaces []candidateInterface, hostname string) []string {
	var tailscale, privateLAN, global []string
	for _, iface := range interfaces {
		if iface.flags&net.FlagUp == 0 || iface.flags&net.FlagLoopback != 0 {
			continue
		}
		for _, rawAddr := range iface.addrs {
			ip, _, err := net.ParseCIDR(rawAddr)
			if err != nil {
				ip = net.ParseIP(rawAddr)
			}
			ip4 := ip.To4()
			if isFilteredCandidateIPv4(ip4) {
				continue
			}
			switch {
			case isTailscaleIP(ip4):
				tailscale = append(tailscale, ip4.String())
			case ip4.IsPrivate():
				privateLAN = append(privateLAN, ip4.String())
			default:
				global = append(global, ip4.String())
			}
		}
	}
	slices.Sort(tailscale)
	slices.Sort(privateLAN)
	slices.Sort(global)
	ordered := make([]string, 0, len(tailscale)+len(privateLAN)+len(global)+1)
	seen := make(map[string]bool)
	appendUnique := func(values ...string) {
		for _, value := range values {
			if value != "" && !seen[value] {
				seen[value] = true
				ordered = append(ordered, value)
			}
		}
	}
	appendUnique(tailscale...)
	appendUnique(privateLAN...)
	appendUnique(global...)
	appendUnique(candidateHostname(hostname))
	if len(ordered) == 0 {
		ordered = append(ordered, "127.0.0.1")
	}
	return ordered
}

func (h *hub) candidateHosts() []string {
	h.mu.RLock()
	configured := slices.Clone(h.cfg.CandidateHosts)
	h.mu.RUnlock()
	if len(configured) > 0 {
		var hosts []string
		seen := make(map[string]bool)
		for _, host := range configured {
			host = strings.TrimSpace(host)
			if parsedHost, _, err := net.SplitHostPort(host); err == nil {
				host = parsedHost
			}
			trimmed := strings.Trim(host, "[]")
			if (net.ParseIP(trimmed) != nil || validDNSName(host)) && !seen[trimmed] {
				seen[trimmed] = true
				hosts = append(hosts, trimmed)
			}
		}
		return hosts
	}
	rawIfaces, _ := net.Interfaces()
	ifaces := make([]candidateInterface, 0, len(rawIfaces))
	for _, iface := range rawIfaces {
		addresses, _ := iface.Addrs()
		cidrs := make([]string, 0, len(addresses))
		for _, address := range addresses {
			cidrs = append(cidrs, address.String())
		}
		ifaces = append(ifaces, candidateInterface{flags: iface.Flags, addrs: cidrs})
	}
	hostname, _ := os.Hostname()
	return collectCandidateHosts(ifaces, hostname)
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
