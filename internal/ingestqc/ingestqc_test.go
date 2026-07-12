package ingestqc

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"slices"
	"testing"

	"github.com/magos/lyceum/internal/epub"
)

// pngOf builds a solid w×h PNG cover for detection tests.
func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	fill := color.NRGBA{R: 120, G: 90, B: 60, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func hasFlag(flags []string, f string) bool { return slices.Contains(flags, f) }

func TestDetectCleanBook(t *testing.T) {
	md := &epub.Metadata{
		Title:       "The Dinosaur Lords",
		Author:      "Victor Milán",
		Identifiers: []string{"urn:uuid:abc", "9780765382115"},
	}
	cover := pngOf(t, 366, 600) // good size + aspect
	if flags := Detect(md, cover); len(flags) != 0 {
		t.Fatalf("clean book flagged %v, want none", flags)
	}
}

func TestDetectNoISBN(t *testing.T) {
	md := &epub.Metadata{Title: "A Good Title", Author: "Jane Doe", Identifiers: []string{"urn:uuid:only"}}
	flags := Detect(md, pngOf(t, 366, 600))
	if !hasFlag(flags, FlagNoISBN) {
		t.Fatalf("flags = %v, want %s", flags, FlagNoISBN)
	}
}

func TestDetectNoCover(t *testing.T) {
	md := &epub.Metadata{Title: "A Good Title", Author: "Jane Doe", Identifiers: []string{"9780765382115"}}
	flags := Detect(md, nil)
	if !hasFlag(flags, FlagNoCover) {
		t.Fatalf("flags = %v, want %s", flags, FlagNoCover)
	}
	if hasFlag(flags, FlagLowQualityCover) {
		t.Fatalf("flags = %v, no_cover and low_quality_cover are mutually exclusive", flags)
	}
}

func TestDetectLowQualityCover(t *testing.T) {
	md := &epub.Metadata{Title: "A Good Title", Author: "Jane Doe", Identifiers: []string{"9780765382115"}}
	t.Run("too small", func(t *testing.T) {
		if flags := Detect(md, pngOf(t, 120, 180)); !hasFlag(flags, FlagLowQualityCover) {
			t.Fatalf("flags = %v, want %s for a tiny cover", flags, FlagLowQualityCover)
		}
	})
	t.Run("odd aspect", func(t *testing.T) {
		if flags := Detect(md, pngOf(t, 600, 600)); !hasFlag(flags, FlagLowQualityCover) {
			t.Fatalf("flags = %v, want %s for a square cover", flags, FlagLowQualityCover)
		}
	})
	t.Run("undecodable", func(t *testing.T) {
		if flags := Detect(md, []byte("not an image at all")); !hasFlag(flags, FlagLowQualityCover) {
			t.Fatalf("flags = %v, want %s for junk bytes", flags, FlagLowQualityCover)
		}
	})
}

func TestDetectSuspiciousTitle(t *testing.T) {
	good := pngOf(t, 366, 600)
	cases := []struct {
		name, title, author string
		want                bool
	}{
		{"segmented title", "D&D - Dragonlance - Chronicles 03", "Weis & Hickman", true},
		{"hash in author", "Dragons of Spring Dawning", "Dragons Of Spring Dawning # 3", true},
		{"author trailing number", "Some Book", "Margaret Weis 3", true},
		{"missing author", "A Real Title", "", true},
		{"clean", "Prince of Thorns", "Mark Lawrence", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			md := &epub.Metadata{Title: tc.title, Author: tc.author, Identifiers: []string{"9780765382115"}}
			got := hasFlag(Detect(md, good), FlagSuspiciousTitle)
			if got != tc.want {
				t.Fatalf("suspicious_title=%v, want %v (title=%q author=%q)", got, tc.want, tc.title, tc.author)
			}
		})
	}
}

func TestDetectAccumulatesFlags(t *testing.T) {
	// No ISBN, no cover, mangled title all at once.
	md := &epub.Metadata{
		Title:       "D&D - Dragonlance - Chronicles 03",
		Author:      "",
		Identifiers: []string{"urn:uuid:x"},
	}
	flags := Detect(md, nil)
	for _, want := range []string{FlagNoISBN, FlagNoCover, FlagSuspiciousTitle} {
		if !hasFlag(flags, want) {
			t.Fatalf("flags = %v, missing %s", flags, want)
		}
	}
}
