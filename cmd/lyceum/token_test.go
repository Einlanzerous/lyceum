package main

import "testing"

// Which origin the terminal QR carries (LYCM-102).
//
// `mint-token` and the first-boot owner invite draw a QR precisely so it can be
// scanned by a phone, which makes the mobile origin the right one whenever the
// two differ. This path has no client-side fallback to rescue a wrong answer —
// a terminal cannot pick another origin the way the apps can — so the precedence
// is worth pinning.
func TestInviteQRBase(t *testing.T) {
	cases := []struct {
		name   string
		cfg    config
		want   string
		reason string
	}{
		{
			name:   "prefers the mobile origin over the browser one",
			cfg:    config{publicURL: "https://gated.example.test", mobileBaseURL: "https://direct.example.test"},
			want:   "https://direct.example.test",
			reason: "the browser origin is the one behind the SSO wall a scan cannot pass",
		},
		{
			name:   "falls back to the public URL when no mobile origin is set",
			cfg:    config{publicURL: "http://192.168.1.9:8080"},
			want:   "http://192.168.1.9:8080",
			reason: "hosts that only ever set LYCEUM_PUBLIC_URL must keep their QR",
		},
		{
			name:   "uses the mobile origin when it is the only one set",
			cfg:    config{mobileBaseURL: "https://direct.example.test"},
			want:   "https://direct.example.test",
			reason: "setting the mobile origin alone should be enough to get a QR",
		},
		{
			name:   "stays empty when neither is set",
			cfg:    config{},
			want:   "",
			reason: "no origin means no QR, rather than one pointing nowhere",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inviteQRBase(tc.cfg); got != tc.want {
				t.Errorf("inviteQRBase() = %q, want %q — %s", got, tc.want, tc.reason)
			}
		})
	}
}
