package inferyard

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"
)

type DiscoveredEndpoint struct {
	URL               string   `json:"url"`
	Health            *string  `json:"health"`
	Models            []string `json:"models"`
	Reason            *string  `json:"reason"`
	AmbiguousListener bool     `json:"ambiguous_listener"`
}

type DiscoveredService struct {
	PID               int64                `json:"pid"`
	ProcessName       string               `json:"process_name"`
	ProcessStartTicks int64                `json:"process_start_ticks"`
	Endpoints         []DiscoveredEndpoint `json:"endpoints"`
}

type ServiceDiscovery struct {
	Services    []DiscoveredService
	Limitations []string
	Status      string
}

var discoveryReasons = []string{
	"models_timeout", "models_unreachable", "models_http_1xx", "models_http_3xx",
	"models_http_4xx", "models_http_5xx", "models_invalid_response", "models_empty",
	"models_response_too_large", "health_timeout", "health_unreachable",
}

var discoveryLimitations = []string{
	"listener_table_incomplete", "listener_ownership_incomplete", "process_identity_unavailable",
	"listener_enumeration_failed", "service_discovery_unsupported_platform", "service_discovery_error",
}

// ParseServiceDiscovery validates the CLI wire contract, without inferring bind eligibility.
func ParseServiceDiscovery(raw []byte, exitCode int) (ServiceDiscovery, error) {
	var result ServiceDiscovery
	invalid := errors.New("invalid service discovery result")
	if !utf8.Valid(raw) {
		return result, invalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueJSON(d); err != nil {
		return result, invalid
	}
	if _, err := d.Token(); err != io.EOF {
		return result, invalid
	}
	r, err := Parse(raw)
	if err != nil || r.Command != "service discover" || r.RunID != nil || r.EvidenceDir != nil || r.Limitations == nil {
		return result, invalid
	}
	if exitCode == 2 || exitCode == 130 {
		status := "blocked"
		if exitCode == 130 {
			status = "interrupted"
		}
		if r.Status != status || r.Completeness != "incomplete" || len(r.Limitations) == 0 ||
			!discoveryFields(raw, "command", "status", "completeness", "run_id", "evidence_dir", "limitations", "details") {
			return result, invalid
		}
		if exitCode == 130 && (len(r.Limitations) != 1 || r.Limitations[0] != "cancelled") {
			return result, invalid
		}
		return ServiceDiscovery{Services: []DiscoveredService{}, Limitations: r.Limitations, Status: status}, nil
	}
	status, complete := "", "incomplete"
	switch exitCode {
	case 0:
		status, complete = "discovered", "complete"
	case 3:
		status = "partial"
	case 4:
		status = "error"
	default:
		return result, invalid
	}
	if r.Status != status || r.Completeness != complete || (exitCode == 0) != (len(r.Limitations) == 0) {
		return result, invalid
	}
	for _, code := range r.Limitations {
		if !slices.Contains(discoveryLimitations, code) {
			return result, invalid
		}
	}
	if !discoveryFields(raw, "command", "status", "completeness", "run_id", "evidence_dir", "limitations", "details") ||
		!discoveryFields(r.Details, "services", "model_requests_sent") {
		return result, invalid
	}
	var details struct {
		Services []json.RawMessage `json:"services"`
		Requests *int              `json:"model_requests_sent"`
	}
	if json.Unmarshal(r.Details, &details) != nil || details.Services == nil || details.Requests == nil || *details.Requests != 0 {
		return result, invalid
	}
	result = ServiceDiscovery{Services: []DiscoveredService{}, Limitations: r.Limitations, Status: status}
	for _, rawService := range details.Services {
		var service DiscoveredService
		if !discoveryFields(rawService, "pid", "process_name", "process_start_ticks", "endpoints") ||
			json.Unmarshal(rawService, &service) != nil || service.PID <= 0 || service.ProcessStartTicks <= 0 ||
			strings.TrimSpace(service.ProcessName) == "" || len(service.Endpoints) == 0 {
			return ServiceDiscovery{}, invalid
		}
		var wire struct {
			Endpoints []json.RawMessage `json:"endpoints"`
		}
		_ = json.Unmarshal(rawService, &wire)
		for i, endpoint := range service.Endpoints {
			if !discoveryFields(wire.Endpoints[i], "url", "health", "models", "reason", "ambiguous_listener") || !validDiscoveryEndpoint(endpoint) {
				return ServiceDiscovery{}, invalid
			}
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(wire.Endpoints[i], &fields)
			if bytes.Equal(bytes.TrimSpace(fields["ambiguous_listener"]), []byte("null")) {
				return ServiceDiscovery{}, invalid
			}
		}
		result.Services = append(result.Services, service)
	}
	return result, nil
}

func discoveryFields(raw []byte, names ...string) bool {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || len(obj) != len(names) {
		return false
	}
	for _, name := range names {
		if _, ok := obj[name]; !ok {
			return false
		}
	}
	return true
}

func validDiscoveryEndpoint(e DiscoveredEndpoint) bool {
	u, err := url.Parse(e.URL)
	if err != nil || u.Scheme != "http" || !net.ParseIP(u.Hostname()).IsLoopback() ||
		u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return false
	}
	if e.Health != nil && !slices.Contains([]string{"1xx", "2xx", "3xx", "4xx", "5xx"}, *e.Health) {
		return false
	}
	if e.Reason != nil && !slices.Contains(discoveryReasons, *e.Reason) {
		return false
	}
	if e.Models == nil {
		return e.Reason != nil && strings.HasPrefix(*e.Reason, "models_")
	}
	if len(e.Models) == 0 || (e.Reason != nil && !strings.HasPrefix(*e.Reason, "health_")) {
		return false
	}
	seen := map[string]bool{}
	for _, name := range e.Models {
		if strings.TrimSpace(name) == "" || seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}
