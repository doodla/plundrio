package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestMux builds the dashboard's real route table (registerRoutes) over the
// test fakes, so route PRECEDENCE — specific /api/* > /api/ catch-all > "/" SPA
// — is exercised end-to-end, not just the individual handlers.
func newTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	d := newTestDashboard(newFakeService(), &fakeAccountClient{}, "")
	mux := http.NewServeMux()
	d.registerRoutes(mux)
	return mux
}

// TestUnknownAPIRouteReturnsJSON404 asserts an unknown /api/* path returns the
// contract JSON 404 (not the SPA HTML the "/" handler would otherwise serve).
func TestUnknownAPIRouteReturnsJSON404(t *testing.T) {
	mux := newTestMux(t)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/bogus", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/bogus status = %d, want 404 (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	resp := mustJSON(t, rec.Body.Bytes())
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("response missing error object: %s", rec.Body.String())
	}
	if errObj["code"] != codeNotFound {
		t.Errorf("error.code = %v, want %s", errObj["code"], codeNotFound)
	}
	if _, ok := errObj["message"].(string); !ok {
		t.Errorf("error.message missing or not a string: %v", errObj["message"])
	}
}

// TestNonAPIDeepLinkServesIndex asserts a client-side deep link (a non-/api path
// with no matching embedded file) still falls back to the SPA index.html, so
// browser deep links keep working.
func TestNonAPIDeepLinkServesIndex(t *testing.T) {
	mux := newTestMux(t)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/transfers/12345", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("deep-link status = %d, want 200 (SPA fallback)", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("deep-link Content-Type = %q, want text/html (index.html)", ct)
	}
	// The placeholder index.html (and the real Vite build) is an HTML document.
	if !strings.Contains(strings.ToLower(rec.Body.String()), "<html") {
		t.Errorf("deep-link body is not the SPA index.html: %s", rec.Body.String())
	}
}

// TestKnownAPIRouteNotShadowedByCatchAll guards the precedence: a registered
// /api/* route still reaches its handler rather than the /api/ 404 catch-all.
func TestKnownAPIRouteNotShadowedByCatchAll(t *testing.T) {
	mux := newTestMux(t)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/transfers", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/transfers status = %d, want 200 (catch-all must not shadow it)", rec.Code)
	}
}
