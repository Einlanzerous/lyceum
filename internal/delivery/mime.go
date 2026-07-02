package delivery

import (
	"encoding/base64"
	"io"
)

// base64LineLen is the RFC 2045 recommended maximum encoded line length.
const base64LineLen = 76

// writeBase64Wrapped base64-encodes data to w, breaking the output into
// CRLF-terminated lines of at most base64LineLen characters. This keeps the
// attachment within SMTP's line-length limits for arbitrarily large EPUBs
// (an unwrapped encoding of a multi-MB file would be a single illegal line).
func writeBase64Wrapped(w io.Writer, data []byte) error {
	encoded := base64.StdEncoding.EncodeToString(data)
	for len(encoded) > 0 {
		n := base64LineLen
		if n > len(encoded) {
			n = len(encoded)
		}
		if _, err := io.WriteString(w, encoded[:n]); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\r\n"); err != nil {
			return err
		}
		encoded = encoded[n:]
	}
	return nil
}
