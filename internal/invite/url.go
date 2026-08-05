// Package invite builds the links that carry a one-time invite to another
// device (LYCM-88).
package invite

import (
	"fmt"
	"net/url"
	"strings"
)

// NormalizeBase canonicalizes an origin for SignInURL — trimmed, no trailing
// slash — and rejects anything that is not an absolute http(s) URL with a host.
// An empty base is not an error: it means "not configured".
//
// Validating matters more here than the usual config-typo argument. A base that
// cannot be resolved is worse than no base at all: clients prefer the server's
// URL over the one they would have built, so a malformed value both produces a QR
// that scans to nothing *and* switches off the local fallback that would have
// worked (LYCM-102). Refusing it leaves that fallback in place.
func NormalizeBase(base string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(base), "/")
	if trimmed == "" {
		return "", nil
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%q is not a URL: %w", base, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%q needs an http:// or https:// scheme", base)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%q has no host", base)
	}
	return trimmed, nil
}

// SignInURL builds the scannable `<base>/sign-in?token=…` redemption link for an
// invite, or "" when no base URL is configured.
//
// This is the one encoder for that link, and it deliberately lives here rather
// than in any single caller: the API hands the URL to clients in the invite
// payload, `lyceum mint-token` renders it as a terminal QR, and Purser's
// connector (SERV-38) can reuse it. While each client built the link from
// whatever origin it happened to know, they disagreed — which is how the web app
// came to encode its own Cloudflare-gated origin into a QR meant for a phone that
// cannot get past that gate (LYCM-102).
func SignInURL(base, token string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return ""
	}
	return base + "/sign-in?token=" + url.QueryEscape(token)
}
