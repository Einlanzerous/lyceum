package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// countUsersFunc is a userCounter backed by a func, so a case can hand the guard
// an account count — or a failing count — without a database.
type countUsersFunc func(context.Context) (int, error)

func (f countUsersFunc) CountUsers(ctx context.Context) (int, error) { return f(ctx) }

func fixedCount(n int) countUsersFunc {
	return func(context.Context) (int, error) { return n, nil }
}

// The boot guard against a household silently collapsing into the owner
// (LYCM-116). Both directions matter and neither is observable at runtime: with
// LYCEUM_AUTH missing nothing errors, so a guard that failed to fire would look
// exactly like a healthy server until nine people's bookmarks had merged — and
// one that fired too eagerly would strand the single-user self-host that is
// supposed to start with no configuration at all.
func TestVerifyAuthMode(t *testing.T) {
	cases := []struct {
		name     string
		accounts int
		userAuth bool
		wantErr  bool
		reason   string
	}{
		{
			name:     "a household with auth off refuses to start",
			accounts: 9,
			wantErr:  true,
			reason:   "all nine would be served as the owner, sharing bookmarks and read marks",
		},
		{
			name:     "two accounts is already a household",
			accounts: 2,
			wantErr:  true,
			reason:   "one person beyond the owner is enough to merge two people's reading",
		},
		{
			name:     "a lone owner starts with no configuration",
			accounts: 1,
			reason:   "a fresh self-host holds only the seeded owner and must keep booting",
		},
		{
			name:     "an unseeded database starts",
			accounts: 0,
			reason:   "no accounts is not a collapsed household; nothing to protect",
		},
		{
			name:     "a household with auth on starts",
			accounts: 9,
			userAuth: true,
			reason:   "this is the configuration the guard exists to insist on",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyAuthMode(context.Background(), fixedCount(tc.accounts), tc.userAuth)
			if tc.wantErr && err == nil {
				t.Fatalf("verifyAuthMode(%d accounts, auth=%v) = nil, want a refusal — %s",
					tc.accounts, tc.userAuth, tc.reason)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("verifyAuthMode(%d accounts, auth=%v) = %v, want nil — %s",
					tc.accounts, tc.userAuth, err, tc.reason)
			}
			if err == nil {
				return
			}
			// The operator sees this line and nothing else, so it has to name the
			// flag they need to set.
			if !strings.Contains(err.Error(), "LYCEUM_AUTH") {
				t.Errorf("refusal = %q, want it to name LYCEUM_AUTH", err)
			}
		})
	}
}

// A database that can't be counted is not proof the deployment is safe, so the
// guard reports the failure rather than waving the boot through.
func TestVerifyAuthModeCountFailure(t *testing.T) {
	boom := errors.New("connection refused")
	err := verifyAuthMode(context.Background(),
		countUsersFunc(func(context.Context) (int, error) { return 0, boom }), false)
	if !errors.Is(err, boom) {
		t.Fatalf("verifyAuthMode with a failing count = %v, want it to wrap %v", err, boom)
	}
}

// With auth on there is nothing to decide, so the guard shouldn't spend a
// round-trip asking.
func TestVerifyAuthModeSkipsCountWhenAuthOn(t *testing.T) {
	counter := countUsersFunc(func(context.Context) (int, error) {
		t.Fatal("counted accounts with LYCEUM_AUTH on; the guard has nothing to check there")
		return 0, nil
	})
	if err := verifyAuthMode(context.Background(), counter, true); err != nil {
		t.Fatalf("verifyAuthMode with auth on = %v, want nil", err)
	}
}
