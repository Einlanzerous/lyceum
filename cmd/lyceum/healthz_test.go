package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
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

// The boot log exists to make a mis-stamped image obvious in `docker logs`, so
// it has to obey the same blank-is-not-unset rule as the wire. A build stamped
// with an empty ARG must announce "dev" — printing an empty version would make
// the one line an operator relies on read as though the field were missing.
func TestLogBuildIdentityAppliesTheDevFallback(t *testing.T) {
	origVersion, origCommit := version.Version, version.Commit
	origFlags, origPrefix := log.Flags(), log.Prefix()
	t.Cleanup(func() {
		version.Version, version.Commit = origVersion, origCommit
		log.SetOutput(os.Stderr)
		log.SetFlags(origFlags)
		log.SetPrefix(origPrefix)
	})
	log.SetFlags(0)

	for _, tc := range []struct {
		name            string
		version, commit string
		want            string
	}{
		{
			name:    "stamped release",
			version: "1.12.0", commit: "ce25438d348b78399e44f4cad937ff17b951c1e2",
			want: "lyceum build: version=1.12.0 commit=ce25438d348b78399e44f4cad937ff17b951c1e2\n",
		},
		{
			// Exactly what an image built with no --build-arg produces.
			name:    "blank ARG",
			version: "", commit: "",
			want: "lyceum build: version=dev commit=no commit recorded\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			log.SetOutput(&buf)
			version.Version, version.Commit = tc.version, tc.commit

			logBuildIdentity()

			if got := buf.String(); got != tc.want {
				t.Errorf("boot log = %q, want %q", got, tc.want)
			}
		})
	}
}
