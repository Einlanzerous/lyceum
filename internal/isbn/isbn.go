// Package isbn provides pure, dependency-free ISBN handling: normalizing raw
// input (hyphens, spaces, "urn:isbn:" prefixes), validating ISBN-10/13
// checksums, converting ISBN-10 to ISBN-13, and pulling an ISBN out of an EPUB
// dc:identifier. It is the backend counterpart to the frontend scan validator
// (LYCM-602) and underpins the inventory keying in LYCM-601.
package isbn

import (
	"errors"
	"strings"
)

// ErrInvalid is returned when input is not a valid ISBN-10 or ISBN-13.
var ErrInvalid = errors.New("isbn: not a valid ISBN-10 or ISBN-13")

// Normalize cleans raw input down to its ISBN digits, validates the checksum,
// converts a valid ISBN-10 to ISBN-13, and returns the canonical 13-digit
// string (digits only, no hyphens). It tolerates surrounding text such as
// "ISBN", "urn:isbn:", hyphens and spaces. It returns ErrInvalid when the
// cleaned value is not a valid ISBN-10 or ISBN-13.
func Normalize(raw string) (string, error) {
	s := clean(raw)
	switch len(s) {
	case 10:
		if valid10(s) {
			return to13(s), nil
		}
	case 13:
		if valid13(s) {
			return s, nil
		}
	}
	return "", ErrInvalid
}

// Valid reports whether raw normalizes to a valid ISBN.
func Valid(raw string) bool {
	_, err := Normalize(raw)
	return err == nil
}

// FromIdentifier extracts a canonical ISBN-13 from an EPUB dc:identifier value
// such as "urn:isbn:9780140449334", "isbn:0-14-044913-9", or a bare ISBN. It
// returns ok=false for identifiers that are not ISBNs (e.g. "urn:uuid:..."),
// which is the common case, so callers should treat a false result as "this
// book has no usable ISBN" rather than an error.
func FromIdentifier(id string) (code string, ok bool) {
	code, err := Normalize(id)
	if err != nil {
		return "", false
	}
	return code, true
}

// FirstFrom returns the first identifier in ids that parses as a valid ISBN,
// normalized to ISBN-13. EPUBs commonly carry several dc:identifier values (a
// UUID alongside one or more ISBNs) in no guaranteed order, so callers pass the
// whole set and let this pick the usable ISBN. ok is false when none qualify.
func FirstFrom(ids []string) (code string, ok bool) {
	for _, id := range ids {
		if code, ok := FromIdentifier(id); ok {
			return code, true
		}
	}
	return "", false
}

// AllFrom returns every identifier in ids that parses as a valid ISBN,
// normalized to ISBN-13 and de-duplicated, preserving first-seen order. Useful
// when a caller wants to try each ISBN (e.g. against a cover source) rather than
// just the first.
func AllFrom(ids []string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, id := range ids {
		code, ok := FromIdentifier(id)
		if !ok {
			continue
		}
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

// clean keeps only ISBN-significant characters: digits, and an 'X'/'x' check
// digit (uppercased). Everything else — hyphens, spaces, "urn:isbn:" — is
// dropped. A non-ISBN identifier rarely cleans to a 10/13-length string that
// also passes the checksum, so the length+checksum gate in Normalize rejects it.
func clean(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == 'X' || r == 'x':
			b.WriteByte('X')
		}
	}
	return b.String()
}

// valid10 reports whether s (10 chars, digits with an optional trailing 'X') is
// a valid ISBN-10: sum(d_i * (10-i)) ≡ 0 (mod 11), where the last digit may be
// 'X' meaning 10.
func valid10(s string) bool {
	sum := 0
	for i := 0; i < 10; i++ {
		c := s[i]
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c == 'X' && i == 9: // X is only legal as the final check digit
			d = 10
		default:
			return false
		}
		sum += d * (10 - i)
	}
	return sum%11 == 0
}

// valid13 reports whether s (13 digits) is a valid ISBN-13/EAN-13:
// sum(d_i * w_i) ≡ 0 (mod 10) with weights alternating 1,3.
func valid13(s string) bool {
	sum := 0
	for i := 0; i < 13; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
		d := int(c - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return sum%10 == 0
}

// to13 converts a valid ISBN-10 to its ISBN-13 form by prefixing "978" to the
// first 9 digits and recomputing the EAN-13 check digit.
func to13(s10 string) string {
	body := "978" + s10[:9]
	sum := 0
	for i := 0; i < 12; i++ {
		d := int(body[i] - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	check := (10 - sum%10) % 10
	return body + string(rune('0'+check))
}
