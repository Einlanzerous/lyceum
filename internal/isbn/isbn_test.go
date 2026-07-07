package isbn

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		// Real-world pair: The Dinosaur Lords (ISBN-10 converts to ISBN-13).
		{"real isbn13", "978-0765382115", "9780765382115", true},
		{"real isbn10", "0765382113", "9780765382115", true},
		{"isbn13 plain", "9780140449334", "9780140449334", true},
		{"isbn13 hyphenated", "978-0-14-044933-4", "9780140449334", true},
		{"isbn13 spaced", "978 0 14 044933 4", "9780140449334", true},
		{"isbn13 urn", "urn:isbn:9780140449334", "9780140449334", true},
		{"isbn13 labeled", "ISBN 978-0-14-044933-4", "9780140449334", true},
		{"isbn10 to 13", "0140449337", "9780140449334", true},
		{"isbn10 hyphenated", "0-14-044933-7", "9780140449334", true},
		{"isbn10 trailing X", "080442957X", "9780804429573", true},
		{"isbn10 lower x", "080442957x", "9780804429573", true},
		{"bad isbn13 checksum", "9780140449335", "", false},
		{"bad isbn10 checksum", "0140449338", "", false},
		{"uuid identifier", "urn:uuid:0a1b2c3d-4e5f-6789-abcd-ef0123456789", "", false},
		{"empty", "", "", false},
		{"too short", "12345", "", false},
		{"x not last", "X140449337", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Normalize(c.in)
			if c.ok {
				if err != nil {
					t.Fatalf("Normalize(%q) error: %v", c.in, err)
				}
				if got != c.want {
					t.Fatalf("Normalize(%q) = %q, want %q", c.in, got, c.want)
				}
			} else if err == nil {
				t.Fatalf("Normalize(%q) = %q, want error", c.in, got)
			}
		})
	}
}

func TestValid(t *testing.T) {
	if !Valid("978-0-14-044933-4") {
		t.Error("expected valid ISBN-13 to report Valid")
	}
	if Valid("nope") {
		t.Error("expected junk to report invalid")
	}
}

func TestFromIdentifier(t *testing.T) {
	if got, ok := FromIdentifier("urn:isbn:9780140449334"); !ok || got != "9780140449334" {
		t.Fatalf("FromIdentifier(urn:isbn) = %q,%v; want 9780140449334,true", got, ok)
	}
	if got, ok := FromIdentifier("0140449337"); !ok || got != "9780140449334" {
		t.Fatalf("FromIdentifier(isbn10) = %q,%v; want 9780140449334,true", got, ok)
	}
	if _, ok := FromIdentifier("urn:uuid:test-0001"); ok {
		t.Fatal("FromIdentifier(uuid) reported ok; want false")
	}
}

func TestFirstFrom(t *testing.T) {
	// ISBN not first: a UUID precedes it (the Eisenhorn case).
	if got, ok := FirstFrom([]string{"urn:uuid:6F024F91", "9781800269187"}); !ok || got != "9781800269187" {
		t.Fatalf("FirstFrom(uuid,isbn) = %q,%v; want 9781800269187,true", got, ok)
	}
	// ISBN-10 anywhere in the list normalizes to ISBN-13.
	if got, ok := FirstFrom([]string{"0140449337"}); !ok || got != "9780140449334" {
		t.Fatalf("FirstFrom(isbn10) = %q,%v; want 9780140449334,true", got, ok)
	}
	// No ISBN at all (UUID-only EPUB, e.g. Spring Dawning / On a Knife Edge).
	if _, ok := FirstFrom([]string{"urn:uuid:a", "d87e3b24-2a62-4783-b562-f5ef38d4f3a3"}); ok {
		t.Fatal("FirstFrom(uuids) reported ok; want false")
	}
	// Empty input.
	if _, ok := FirstFrom(nil); ok {
		t.Fatal("FirstFrom(nil) reported ok; want false")
	}
}
