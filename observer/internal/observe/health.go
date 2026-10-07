package observe

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Health struct {
	State                string  `json:"state"`
	Reason               *string `json:"reason"`
	StatusCode           *int    `json:"status_code"`
	PIDOwnershipVerified bool    `json:"pid_ownership_verified"`
	CheckedUTC           *string `json:"checked_utc"`
	Cached               bool    `json:"cached"`
}

type HealthProbe struct {
	client *http.Client
	url    string
	next   time.Time
	result Health
}

func NewHealthProbe(endpoint string) (*HealthProbe, error) {
	missing := "not_requested"
	p := &HealthProbe{result: Health{State: "not_observed", Reason: &missing}}
	if endpoint == "" {
		return p, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") || u.Port() == "" || u.ForceQuery {
		return nil, errors.New("endpoint_must_be_plain_loopback_http")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("endpoint_must_be_literal_loopback")
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil || port == 0 {
		return nil, errors.New("endpoint_port_invalid")
	}
	u.Path = "/health"
	p.url = u.String()
	p.client = &http.Client{Timeout: 250 * time.Millisecond,
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxResponseHeaderBytes: 16384},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	return p, nil
}

func (p *HealthProbe) Read(ctx context.Context, now time.Time) Health {
	if p.url == "" {
		return p.result
	}
	if now.Before(p.next) {
		cached := p.result
		cached.Cached = true
		return cached
	}
	p.next = now.Add(5 * time.Second)
	checked := now.UTC().Format(time.RFC3339Nano)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err == nil {
		var response *http.Response
		response, err = p.client.Do(req)
		if err == nil {
			response.Body.Close() // Response bodies are neither logged nor read without bounds.
			status := response.StatusCode
			p.result = Health{State: "reachable", StatusCode: &status, CheckedUTC: &checked}
			if status < 200 || status >= 300 {
				reason := "http_non_success"
				p.result.State, p.result.Reason = "http_error", &reason
			}
			return p.result
		}
	}
	reason := "request_failed_or_timed_out"
	p.result = Health{State: "unavailable", Reason: &reason, CheckedUTC: &checked}
	return p.result
}
