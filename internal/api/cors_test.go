package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// okHandler is a stand-in for the wrapped mux; it records whether it ran.
func okHandler(ran *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*ran = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestCORS_SameOriginRequestPassesThrough(t *testing.T) {
	var ran bool
	h := CORS(DefaultCORSOrigins, okHandler(&ran))

	// No Origin header → not a CORS request (the web build).
	req := httptest.NewRequest(http.MethodGet, "/library", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !ran {
		t.Fatal("inner handler should run for a same-origin request")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("no CORS headers expected without Origin, got %q", got)
	}
}

func TestCORS_AllowedOriginGetsHeaders(t *testing.T) {
	var ran bool
	h := CORS(DefaultCORSOrigins, okHandler(&ran))

	req := httptest.NewRequest(http.MethodGet, "/library", nil)
	req.Header.Set("Origin", "http://wails.localhost")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !ran {
		t.Fatal("inner handler should run for an allowed cross-origin GET")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://wails.localhost" {
		t.Fatalf("Allow-Origin = %q, want the echoed origin", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got != corsExpose {
		t.Fatalf("Expose-Headers = %q, want %q", got, corsExpose)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want Origin", got)
	}
}

func TestCORS_PreflightShortCircuits(t *testing.T) {
	var ran bool
	h := CORS(DefaultCORSOrigins, okHandler(&ran))

	req := httptest.NewRequest(http.MethodOptions, "/sync", nil)
	req.Header.Set("Origin", "http://wails.localhost")
	req.Header.Set("Access-Control-Request-Method", "PUT")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if ran {
		t.Fatal("preflight must not reach the inner handler (mux would 405 it)")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("preflight should advertise allowed methods")
	}
	// The advertised request headers are echoed back.
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "content-type" {
		t.Fatalf("Allow-Headers = %q, want echoed 'content-type'", got)
	}
}

func TestCORS_DisallowedOriginGetsNoHeaders(t *testing.T) {
	var ran bool
	h := CORS(DefaultCORSOrigins, okHandler(&ran))

	req := httptest.NewRequest(http.MethodGet, "/library", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// The request still reaches the handler (CORS is a browser-side guard), but
	// without Allow-Origin the browser discards the response.
	if !ran {
		t.Fatal("non-preflight should still reach the handler")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disallowed origin must not get Allow-Origin, got %q", got)
	}
}

func TestCORS_Wildcard(t *testing.T) {
	var ran bool
	h := CORS([]string{"*"}, okHandler(&ran))

	req := httptest.NewRequest(http.MethodGet, "/library", nil)
	req.Header.Set("Origin", "https://anything.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://anything.example" {
		t.Fatalf("wildcard should echo any origin, got %q", got)
	}
}

func TestParseCORSOrigins(t *testing.T) {
	// Empty env → just the built-in native origins.
	got := ParseCORSOrigins("")
	if len(got) != len(DefaultCORSOrigins) {
		t.Fatalf("empty env: got %d origins, want %d defaults", len(got), len(DefaultCORSOrigins))
	}

	// Extra origins are appended to the defaults.
	got = ParseCORSOrigins("https://reader.example.com, http://192.168.1.10:8080")
	if len(got) != len(DefaultCORSOrigins)+2 {
		t.Fatalf("got %d origins, want defaults+2", len(got))
	}

	// A wildcard anywhere collapses to allow-any.
	got = ParseCORSOrigins("https://a.example, *")
	if len(got) != 1 || got[0] != "*" {
		t.Fatalf("wildcard should collapse to [\"*\"], got %v", got)
	}
}
