// Package invite builds the links that carry a one-time invite to another
// device (LYCM-88).
package invite

import (
	"net/url"
	"strings"
)

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
