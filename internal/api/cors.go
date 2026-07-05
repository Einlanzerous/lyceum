package api

import (
	"net/http"
	"strings"
)

// DefaultCORSOrigins are the fixed origins the LYCM-300 Wails desktop shell
// loads from. The web build is served same-origin by this server and sends no
// Origin on its API calls, so it needs no entry here; the native Android app
// (mobile/, Flutter) makes plain Dart HTTP requests with no browser Origin, so
// it needs none either. Only the Wails WebView2 shell has a distinct SPA origin.
// These are allowed out of the box so a freshly built .exe reaches the server
// with no extra config. LYCEUM_CORS_ORIGINS adds more (or, set to "*", allows any).
var DefaultCORSOrigins = []string{
	"http://wails.localhost", // Wails v2 WebView2 asset origin (Windows)
	"https://wails.localhost",
}

// corsHeaders are the request headers the reader sends cross-origin: bearer
// auth for the scoped ecosystem routes, JSON content type, and Range for the
// EPUB/cover blob fetches.
const corsHeaders = "Authorization, Content-Type, Range"

// corsExpose lets the webview read the blob-streaming response headers epub.js
// and <img> rely on for ranged/partial content.
const corsExpose = "Content-Length, Content-Range, Accept-Ranges"

// CORS wraps next with cross-origin support for the given allowed origins. A
// request with no Origin (the same-origin web build, curl, the folder watcher)
// passes straight through untouched. A cross-origin request from an allowed
// origin gets the CORS response headers; an OPTIONS preflight from an allowed
// origin is answered 204 here (the method-specific ServeMux routes would
// otherwise 405 it). A disallowed origin is left without CORS headers, so the
// browser blocks it — exactly the intended deny.
//
// Passing a single "*" origin allows any origin (echoed back, since the reader
// authenticates with a bearer header rather than cookies, so no credentialed
// wildcard restriction applies).
func CORS(allowed []string, next http.Handler) http.Handler {
	wildcard := false
	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if o == "*" {
			wildcard = true
			continue
		}
		set[o] = struct{}{}
	}

	allow := func(origin string) bool {
		if wildcard {
			return true
		}
		_, ok := set[origin]
		return ok
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Not a CORS request (same-origin or non-browser): nothing to add.
			next.ServeHTTP(w, r)
			return
		}

		// Responses vary by Origin even when we don't echo it, so caches never
		// reuse an allowed response for a disallowed origin (or vice versa).
		w.Header().Add("Vary", "Origin")

		if !allow(origin) {
			// Unknown origin: no CORS headers. A preflight gets a bare 204 (the
			// browser still blocks the real request); anything else falls through.
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		// Echo the requested headers when the browser advertises them, else fall
		// back to the known set — covers preflights that omit the hint.
		if reqHeaders := r.Header.Get("Access-Control-Request-Headers"); reqHeaders != "" {
			h.Set("Access-Control-Allow-Headers", reqHeaders)
		} else {
			h.Set("Access-Control-Allow-Headers", corsHeaders)
		}
		h.Set("Access-Control-Expose-Headers", corsExpose)
		h.Set("Access-Control-Max-Age", "600")

		if r.Method == http.MethodOptions {
			// Preflight: headers are set, no body to serve.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ParseCORSOrigins builds the allowed-origin list from the LYCEUM_CORS_ORIGINS
// env value (comma-separated). The built-in native-shell origins
// (DefaultCORSOrigins) are always included so the wrappers work unconfigured;
// the env extends them. A literal "*" anywhere in the value switches to
// allow-any and short-circuits the rest.
func ParseCORSOrigins(env string) []string {
	out := append([]string(nil), DefaultCORSOrigins...)
	for _, part := range strings.Split(env, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if part == "*" {
			return []string{"*"}
		}
		out = append(out, part)
	}
	return out
}
