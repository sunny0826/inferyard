package observe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthIsExplicitReadOnlyAndThrottled(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/health" {
			t.Errorf("inference or wrong path: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(200)
	}))
	defer s.Close()
	p, err := NewHealthProbe(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h := p.Read(context.Background(), now)
	if h.State != "reachable" || h.PIDOwnershipVerified || h.Cached || h.CheckedUTC == nil {
		t.Fatal(h)
	}
	cached := p.Read(context.Background(), now.Add(time.Second))
	if !cached.Cached || *cached.CheckedUTC != *h.CheckedUTC || calls.Load() != 1 {
		t.Fatal(cached)
	}
	p.Read(context.Background(), now.Add(5*time.Second))
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
	quiet, _ := NewHealthProbe("")
	if h := quiet.Read(context.Background(), now); h.State != "not_observed" || calls.Load() != 2 {
		t.Fatal(h)
	}
}

func TestHealthRejectsRemoteCredentialsAndRedirects(t *testing.T) {
	for _, u := range []string{"https://127.0.0.1:80", "http://localhost:80", "http://10.0.0.1:80", "http://user:pass@127.0.0.1:80", "http://127.0.0.1:80?key=secret", "http://127.0.0.1:80#x", "http://127.0.0.1:80/v1/chat/completions", "http://127.0.0.1:0", "http://127.0.0.1:80?"} {
		if _, err := NewHealthProbe(u); err == nil {
			t.Fatal(u)
		}
	}
	var destinationCalls atomic.Int32
	d := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls.Add(1) }))
	defer d.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, d.URL, 302) }))
	defer s.Close()
	p, _ := NewHealthProbe(s.URL)
	if h := p.Read(context.Background(), time.Now()); h.State != "http_error" || *h.StatusCode != 302 || destinationCalls.Load() != 0 {
		t.Fatal(h)
	}
}
