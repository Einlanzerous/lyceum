package api

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestIPRateLimiterAllowsUpToBurstThenBlocks(t *testing.T) {
	l := newIPRateLimiter(time.Minute, 3)
	for i := 0; i < 3; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("attempt %d should be allowed within the burst", i+1)
		}
	}
	if l.allow("1.2.3.4") {
		t.Fatal("the 4th attempt in the window should be blocked")
	}
}

func TestIPRateLimiterIsPerKey(t *testing.T) {
	l := newIPRateLimiter(time.Minute, 1)
	if !l.allow("1.1.1.1") {
		t.Fatal("first key's first attempt should be allowed")
	}
	if !l.allow("2.2.2.2") {
		t.Fatal("a different key has its own budget")
	}
	if l.allow("1.1.1.1") {
		t.Fatal("first key is now over its limit")
	}
}

func TestIPRateLimiterResetsAfterWindow(t *testing.T) {
	now := time.Unix(0, 0)
	l := newIPRateLimiter(time.Minute, 1)
	l.now = func() time.Time { return now }

	if !l.allow("1.2.3.4") {
		t.Fatal("first attempt allowed")
	}
	if l.allow("1.2.3.4") {
		t.Fatal("second attempt in the same window blocked")
	}
	now = now.Add(61 * time.Second)
	if !l.allow("1.2.3.4") {
		t.Fatal("a new window should allow again")
	}
}

func TestClientIPStripsPort(t *testing.T) {
	r := httptest.NewRequest("POST", "/auth/session", nil)
	r.RemoteAddr = "203.0.113.7:54321"
	if got := clientIP(r); got != "203.0.113.7" {
		t.Fatalf("clientIP = %q, want 203.0.113.7", got)
	}
}
