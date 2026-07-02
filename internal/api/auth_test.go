package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseTokens(t *testing.T) {
	a, err := ParseTokens(" tok_read = eidolon:read , tok_both=eidolon:read|delivery:send ,")
	if err != nil {
		t.Fatalf("ParseTokens: %v", err)
	}
	if !a.allows("tok_read", ScopeEidolonRead) {
		t.Fatal("tok_read should allow eidolon:read")
	}
	if a.allows("tok_read", ScopeDeliverySend) {
		t.Fatal("tok_read should not allow delivery:send")
	}
	if !a.allows("tok_both", ScopeEidolonRead) || !a.allows("tok_both", ScopeDeliverySend) {
		t.Fatal("tok_both should allow both scopes")
	}
	if a.allows("nope", ScopeEidolonRead) {
		t.Fatal("unknown token should allow nothing")
	}
}

func TestParseTokensErrors(t *testing.T) {
	for _, spec := range []string{
		"no-equals-sign",
		"=eidolon:read",          // empty token
		"tok=",                   // no scopes
		"tok=eidolon:read|bogus", // unknown scope
	} {
		if _, err := ParseTokens(spec); err == nil {
			t.Errorf("ParseTokens(%q) = nil error, want error", spec)
		}
	}
}

func TestParseTokensEmpty(t *testing.T) {
	a, err := ParseTokens("")
	if err != nil {
		t.Fatalf("ParseTokens(\"\"): %v", err)
	}
	if a.allows("anything", ScopeEidolonRead) {
		t.Fatal("empty table should allow nothing")
	}
}

func TestRequireScope(t *testing.T) {
	auth, err := ParseTokens("good=eidolon:read,sender=delivery:send")
	if err != nil {
		t.Fatalf("ParseTokens: %v", err)
	}
	a := New(nil, "", WithAuth(auth))
	guarded := a.requireScope(ScopeEidolonRead, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"no header", "", http.StatusUnauthorized},
		{"garbage scheme", "Basic abc", http.StatusUnauthorized},
		{"unknown token", "Bearer nope", http.StatusUnauthorized},
		{"wrong scope", "Bearer sender", http.StatusForbidden},
		{"valid", "Bearer good", http.StatusNoContent},
		{"case-insensitive scheme", "bearer good", http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/eidolon/x", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			guarded(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

// TestRequireScopeNoAuthConfigured verifies the fail-safe: with no token table
// installed, a protected route rejects every request.
func TestRequireScopeNoAuthConfigured(t *testing.T) {
	a := New(nil, "")
	guarded := a.requireScope(ScopeEidolonRead, func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler should not run without configured auth")
	})
	req := httptest.NewRequest(http.MethodGet, "/eidolon/x", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()
	guarded(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
