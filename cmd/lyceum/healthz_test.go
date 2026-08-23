package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/magos/lyceum/internal/version"
)

// What the delivery reconciler actually parses. Kept as a raw map rather than
// reusing healthzResponse so the test asserts the JSON *wire* shape — reusing
// the struct would make a renamed json tag invisible, which is precisely the
// break that would silently stop observations.
func decodeHealthz(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body is not JSON: %v (body=%s)", err, body)
	}
	return got
}

func TestHandleHealthzReportsBuildIdentity(t *testing.T) {
	origVersion, origCommit := version.Version, version.Commit
	t.Cleanup(func() { version.Version, version.Commit = origVersion, origCommit })

	const sha = "08679beff57e82e4749793b73bd7337bfeb796e8"
	version.Version, version.Commit = "1.10.0", sha

	rec := httptest.NewRecorder()
	handleHealthz(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		// The reconciler falls back to sniffing a leading "<" when the content
		// type is absent, and files a markup body as `unreachable`. Getting
		// this header right is what keeps a healthy service off the red list.
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	got := decodeHealthz(t, rec.Body.Bytes())
	if got["version"] != "1.10.0" {
		t.Errorf("version = %v, want 1.10.0", got["version"])
	}
	if got["sha"] != sha {
		t.Errorf("sha = %v, want the full 40-char commit %s", got["sha"], sha)
	}
	// The pre-existing fields keep working — uptime-kuma and the compose
	// HEALTHCHECK both read this endpoint.
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	if got["service"] != "lyceum" {
		t.Errorf("service = %v, want lyceum", got["service"])
	}
}

// An unstamped build must say so, and `sha` must be JSON null rather than "".
// The reconciler treats a blank version as "reports no version" and an absent
// sha as null; an empty STRING for either is a third state nothing expects.
func TestHandleHealthzUnstampedBuild(t *testing.T) {
	origVersion, origCommit := version.Version, version.Commit
	t.Cleanup(func() { version.Version, version.Commit = origVersion, origCommit })

	// Exactly what an image built with no --build-arg produces.
	version.Version, version.Commit = "", ""

	rec := httptest.NewRecorder()
	handleHealthz(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	got := decodeHealthz(t, rec.Body.Bytes())
	if got["version"] != "dev" {
		t.Errorf("version = %v, want dev — a blank ARG must not report an empty version", got["version"])
	}
	if _, present := got["sha"]; !present {
		t.Error("sha key is missing; the contract wants it present and null")
	}
	if got["sha"] != nil {
		t.Errorf("sha = %v, want null", got["sha"])
	}
}
