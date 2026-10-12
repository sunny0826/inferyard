package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"
)

type Server struct {
	token   string
	assets  fs.FS
	mu      sync.Mutex
	count   uint64
	clients map[chan uint64]struct{}
}

func New(ctx context.Context, assets fs.FS) (*Server, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, err
	}
	s := &Server{token: hex.EncodeToString(secret[:]), assets: assets, clients: make(map[chan uint64]struct{})}
	go s.tick(ctx)
	return s, nil
}

func (s *Server) Token() string { return s.token }

func (s *Server) tick(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			s.count++
			for ch := range s.clients {
				select {
				case ch <- s.count:
				default:
				}
			}
			s.mu.Unlock()
		}
	}
}

func (s *Server) authorized(token string) bool {
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r.Header.Get("Authorization")) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("GET /api/events", s.events)
	files := http.FileServer(http.FS(s.assets))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" && !isAsset(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		if !isAsset(r.URL.Path) && !s.authorized(r.URL.Query().Get("token")) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(w, r)
	})
	return mux
}

func isAsset(path string) bool {
	return len(path) > len("/assets/") && path[:len("/assets/")] == "/assets/"
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r.URL.Query().Get("token")) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	ch := make(chan uint64, 1)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.clients, ch); s.mu.Unlock() }()
	log.Printf("SSE connected user_agent=%q", r.UserAgent())
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case count := <-ch:
			if _, err := fmt.Fprintf(w, "event: count\nid: %d\ndata: %d\n\n", count, count); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
