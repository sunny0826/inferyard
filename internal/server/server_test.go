package server

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestHTTPAndSharedSSE(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := New(ctx, fstest.MapFS{"index.html": {Data: []byte("embedded frontend")}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := New(ctx, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Token() == other.Token() || len(s.Token()) != 64 {
		t.Fatal("tokens must be random per session")
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	for _, tc := range []struct {
		path, header string
		status       int
	}{
		{"/", "", 401}, {"/?token=wrong", "", 401}, {"/?token=" + s.Token(), "", 200},
		{"/index.html", "", 401}, {"/api/events", "", 401},
		{"/api/status?token=" + s.Token(), "", 401}, {"/api/status", s.Token(), 200},
	} {
		req, _ := http.NewRequest("GET", ts.URL+tc.path, nil)
		req.Header.Set("Authorization", tc.header)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Fatalf("%s: got %d", tc.path, res.StatusCode)
		}
		if tc.path == "/?token="+s.Token() && string(body) != "embedded frontend" {
			t.Fatal(string(body))
		}
	}
	streamCtx, stop := context.WithTimeout(ctx, 6*time.Second)
	defer stop()
	readers := make([]*bufio.Reader, 2)
	for i := range readers {
		req, _ := http.NewRequestWithContext(streamCtx, "GET", ts.URL+"/api/events?token="+s.Token(), nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatal("wrong SSE content type")
		}
		readers[i] = bufio.NewReader(res.Body)
	}
	for _, want := range []string{"data: 1\n", "data: 2\n"} {
		for _, reader := range readers {
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(line, "data:") {
					if line != want {
						t.Fatalf("got %q want %q", line, want)
					}
					break
				}
			}
		}
	}
}
