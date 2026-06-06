package dashboard

import (
	"io/fs"
	"net/http"
	"strings"
)

// spaFallback serves index.html for client-side-routed deep links. A request
// path that does not resolve to a real embedded file (and isn't an API route)
// gets index.html so the SPA router can handle it. API routes are registered on
// the mux ahead of the static handler, so they never reach here.
type spaFallback struct {
	index []byte
	ok    bool
}

func newSPAFallback(sub fs.FS) *spaFallback {
	data, err := fs.ReadFile(sub, "index.html")
	return &spaFallback{index: data, ok: err == nil}
}

// shouldFallback reports whether the request path should be served index.html
// rather than a static file: true when the path has no extension (a route, not
// an asset) or the named file doesn't exist in the embedded tree. The root path
// "/" also falls back so the index is served there.
func (s *spaFallback) shouldFallback(urlPath string, sub fs.FS) bool {
	clean := strings.TrimPrefix(urlPath, "/")
	if clean == "" {
		return true
	}
	if _, err := fs.Stat(sub, clean); err == nil {
		return false // a real embedded file: let the file server handle it
	}
	return true
}

func (s *spaFallback) serve(w http.ResponseWriter, _ *http.Request) {
	if !s.ok {
		http.Error(w, "dashboard index unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// SPA shell is not cacheable: it must reflect the current build's asset
	// hashes on every load.
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(s.index)
}
