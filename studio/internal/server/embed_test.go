package server

import (
	"inferyard/studio/web"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
)

// This test is also run from a standalone compiled test binary in an empty
// working directory. It reads only the embedded production frontend.
func TestEmbeddedArtifact(t *testing.T) {
	s, _ := setup(t, nil)
	assets, e := fs.Sub(web.Assets, "dist")
	if e != nil {
		t.Fatal(e)
	}
	s.assets = assets
	r := httptest.NewRequest(http.MethodGet, "/?token="+s.Token(), nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || w.Body.Len() < 100 {
		t.Fatal(w.Code, w.Body.String())
	}
	t.Logf("embedded frontend served: %d bytes", w.Body.Len())
}
