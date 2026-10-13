package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/core"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	token  string
	assets fs.FS
	Core   *core.Service
	ctx    context.Context
}

func New(ctx context.Context, assets fs.FS) (*Server, error) {
	c, e := core.New(ctx, core.Options{})
	if e != nil {
		return nil, e
	}
	return NewWithCore(ctx, assets, c)
}
func NewWithCore(ctx context.Context, assets fs.FS, c *core.Service) (*Server, error) {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		return nil, e
	}
	return &Server{hex.EncodeToString(b[:]), assets, c, ctx}, nil
}
func (s *Server) Token() string { return s.token }
func Listen(address string) (net.Listener, error) {
	host, _, e := net.SplitHostPort(address)
	if e != nil || host != "127.0.0.1" {
		return nil, fmt.Errorf("only 127.0.0.1 is allowed")
	}
	return net.Listen("tcp4", address)
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, e *core.ApiError) {
	respond(w, e.HTTPStatus(), map[string]any{"error": e})
}
func decode(r *http.Request, v any) bool {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		return false
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	if e != nil || len(raw) > 1<<20 || len(strings.TrimSpace(string(raw))) == 0 || strings.TrimSpace(string(raw))[0] != '{' {
		return false
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return false
	}
	return d.Decode(new(any)) == io.EOF
}
func (s *Server) authorized(r *http.Request) bool {
	token := ""
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		token = strings.TrimPrefix(h, "Bearer ")
	}
	p := r.URL.Path
	if token == "" && (p == "/api/events" || p == "/" || p == "/index.html" || strings.HasPrefix(p, "/reports/")) {
		token = r.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1
}
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serve) }
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	p := r.URL.Path
	if !strings.HasPrefix(p, "/assets/") && !s.authorized(r) {
		failure(w, core.Err("unauthorized"))
		return
	}
	if r.Method == "GET" && (p == "/" || p == "/index.html" || strings.HasPrefix(p, "/assets/")) {
		name := strings.TrimPrefix(p, "/")
		if name == "" {
			name = "index.html"
		}
		data, e := fs.ReadFile(s.assets, name)
		if e != nil {
			failure(w, core.Err("not_found"))
			return
		}
		serveStatic(w, r, name, time.Time{}, strings.NewReader(string(data)))
		return
	}
	if r.Method == "GET" && strings.HasPrefix(p, "/reports/") {
		path, e := s.Core.ReportFile(strings.TrimPrefix(p, "/reports/"))
		if e != nil {
			failure(w, e)
			return
		}
		f, err := os.Open(path)
		if err != nil {
			failure(w, core.Err("not_found"))
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			failure(w, core.Err("internal_error"))
			return
		}
		serveStatic(w, r, st.Name(), st.ModTime(), f)
		return
	}
	var value any
	status := 200
	var e *core.ApiError
	switch r.Method + " " + p {
	case "GET /api/status":
		value = s.Core.Status()
	case "GET /api/readiness":
		value = s.Core.Readiness()
	case "POST /api/settings/provision":
		value, e = s.Core.Provision()
	case "GET /api/progress":
		value, e = s.Core.Progress(r.Context())
	case "GET /api/onboarding/startup":
		value, e = s.Core.Startup(r.URL.Query().Get("candidate"))
	case "GET /api/onboarding/processes":
		value, e = s.Core.Processes(r.Context())
	case "POST /api/onboarding/execute":
		var q core.WizardRequest
		if !decode(r, &q) {
			e = core.Err("invalid_request")
		} else {
			value, e = s.Core.WizardExecute(r.Context(), q)
		}
	case "GET /api/settings":
		value = s.Core.Settings()
	case "PUT /api/settings":
		var q map[string]json.RawMessage
		if !decode(r, &q) {
			e = core.Err("invalid_request")
		} else {
			value, e = s.Core.UpdateSettings(q)
		}
	case "GET /api/device":
		value = s.Core.Device()
	case "POST /api/device/refresh":
		e = s.Core.RefreshDevice()
		status = 202
		value = map[string]bool{"refreshing": true}
	case "GET /api/runs":
		value, e = s.Core.History()
	case "POST /api/runs":
		var q core.StartRunsRequest
		if !decode(r, &q) {
			e = core.Err("invalid_request")
		} else {
			var tasks []core.RunTask
			tasks, e = s.Core.StartRuns(q)
			value = map[string]any{"tasks": tasks}
			status = 202
		}
	case "POST /api/runs/resume":
		var q core.ResumeRunRequest
		if !decode(r, &q) {
			e = core.Err("invalid_request")
		} else {
			var task core.RunTask
			task, e = s.Core.Resume(q)
			value = map[string]any{"task": task}
			status = 202
		}
	case "GET /api/run-tasks":
		value = map[string]any{"tasks": s.Core.RunTasks()}
	case "GET /api/reports":
		var root *string
		var reports []core.ReportSummary
		root, reports, e = s.Core.Reports()
		value = map[string]any{"reports_root": root, "reports": reports}
	case "POST /api/reports":
		var q core.CreateReportRequest
		if !decode(r, &q) {
			e = core.Err("invalid_request")
		} else {
			var report core.ReportSummary
			report, e = s.Core.CreateReport(q)
			value = map[string]any{"report": report}
			status = 201
		}
	case "GET /api/downloads":
		value = map[string]any{"tasks": s.Core.Downloads()}
	case "POST /api/downloads":
		var q core.CreateDownloadRequest
		if !decode(r, &q) {
			e = core.Err("invalid_request")
		} else {
			var task core.DownloadTask
			task, e = s.Core.CreateDownload(q)
			value = map[string]any{"task": task}
			status = 202
		}
	case "GET /api/events":
		s.events(w, r)
		return
	default:
		parts := strings.Split(strings.Trim(p, "/"), "/")
		if len(parts) == 3 && parts[0] == "api" && parts[1] == "run-tasks" && r.Method == "GET" {
			value, e = s.Core.RunDetail(parts[2])
		} else if len(parts) == 4 && parts[0] == "api" && parts[3] == "cancel" && r.Method == "POST" {
			status = 202
			switch parts[1] {
			case "run-tasks":
				var t core.RunTask
				t, e = s.Core.CancelRun(parts[2])
				value = map[string]any{"task": t}
			case "downloads":
				var t core.DownloadTask
				t, e = s.Core.CancelDownload(parts[2])
				value = map[string]any{"task": t}
			default:
				e = core.Err("not_found")
			}
		} else {
			e = core.Err("not_found")
		}
	}
	if e != nil {
		failure(w, e)
		return
	}
	respond(w, status, value)
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		failure(w, core.Err("internal_error"))
		return
	}
	wake, unsubscribe, last := s.Core.Bus.Subscribe()
	defer unsubscribe()
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		v, e := strconv.ParseUint(raw, 10, 64)
		if e != nil {
			failure(w, core.Err("invalid_request"))
			return
		}
		last = v
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, ": connected\n\n")
	f.Flush()
	timer := time.NewTimer(core.HeartbeatInterval)
	defer timer.Stop()
	first := r.Header.Get("Last-Event-ID") != ""
	send := func() bool {
		var events []core.Event
		var gap bool
		var current uint64
		if first {
			events, gap, current = s.Core.Bus.Since(last)
			first = false
		} else {
			events, gap, current = s.Core.Bus.After(last)
		}
		controller := http.NewResponseController(w)
		_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
		defer controller.SetWriteDeadline(time.Time{})
		if gap {
			if _, e := fmt.Fprintf(w, "id: %d\nevent: sync.reset\ndata: {\"reason\":\"buffer_gap\"}\n\n", current); e != nil {
				return false
			}
			last = current
		}
		for _, event := range events {
			if _, e := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Type, event.Data); e != nil {
				return false
			}
			last = event.ID
		}
		f.Flush()
		if len(events) > 0 || gap {
			timer.Reset(core.HeartbeatInterval)
		}
		return true
	}
	if !send() {
		return
	}
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-r.Context().Done():
			return
		case <-wake:
			if !send() {
				return
			}
		case <-timer.C:
			if _, e := fmt.Fprint(w, ": hb\n\n"); e != nil {
				return
			}
			f.Flush()
			timer.Reset(core.HeartbeatInterval)
		}
	}
}

// Keep standard-library static error responses inside the API error contract.
type staticWriter struct {
	http.ResponseWriter
	failed bool
}

func (w *staticWriter) WriteHeader(status int) {
	if status >= 400 {
		w.failed = true
		w.Header().Del("Content-Length")
		w.Header().Del("Content-Range")
		code := "invalid_request"
		if status == 404 {
			code = "not_found"
		}
		failure(w.ResponseWriter, core.Err(code))
		return
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *staticWriter) Write(p []byte) (int, error) {
	if w.failed {
		return len(p), nil
	}
	return w.ResponseWriter.Write(p)
}
func serveStatic(w http.ResponseWriter, r *http.Request, name string, modified time.Time, reader io.ReadSeeker) {
	copy := r.Clone(r.Context())
	for _, h := range []string{"If-Modified-Since", "If-Unmodified-Since", "If-None-Match", "If-Match", "If-Range"} {
		copy.Header.Del(h)
	}
	http.ServeContent(&staticWriter{ResponseWriter: w}, copy, name, modified, reader)
}
