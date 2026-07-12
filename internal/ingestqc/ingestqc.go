// Package ingestqc detects quality problems in a freshly-ingested book so the
// worst ones can be held for human review instead of silently going live
// (LYCM-58). It is the "kick it to me to check" safety net over the automated
// cover fetch (LYCM-56) and normalization (LYCM-65): those clean the cosmetic
// side, but a book with no ISBN, a poor source cover, or mangled MOBI metadata
// still wants eyes on it.
//
// Detect is deliberately conservative and cheap — a false positive only costs a
// one-click approve, while a missed problem lands a bad book on the shelf.
package ingestqc

import (
	"strings"

	"github.com/magos/lyceum/internal/coverimg"
	"github.com/magos/lyceum/internal/epub"
	"github.com/magos/lyceum/internal/isbn"
)

// Issue codes stored on a pending book's review_flags and shown in the review
// UI. Stable strings — the web surface maps them to labels.
const (
	FlagNoISBN          = "no_isbn"
	FlagNoCover         = "no_cover"
	FlagLowQualityCover = "low_quality_cover"
	FlagSuspiciousTitle = "suspicious_title"
)

// Cover-quality thresholds for FlagLowQualityCover, measured on the source cover
// before normalization.
const (
	minCoverDim        = 200  // px; below this the source is too small to be good
	maxBorderFraction  = 0.15 // heavily framed (white/black border) covers
	minCoverAspect     = 0.5  // portrait book covers cluster around 0.61 (366:600)
	maxCoverAspect     = 0.75 // outside [min,max] is an odd, non-book-cover shape
	suspiciousSegments = 2    // count of " - " segments that reads as mangled
)

// Detect returns the QC issue codes a freshly-parsed EPUB trips, in a stable
// order. An empty slice means the book is clean and can publish straight to the
// shelf. cover is the chosen source cover bytes (embedded or fetched) before
// normalization — empty when the book has none.
func Detect(md *epub.Metadata, cover []byte) []string {
	var flags []string
	if _, ok := isbn.FirstFrom(md.Identifiers); !ok {
		flags = append(flags, FlagNoISBN)
	}
	switch {
	case len(cover) == 0:
		flags = append(flags, FlagNoCover)
	case lowQualityCover(cover):
		flags = append(flags, FlagLowQualityCover)
	}
	if suspiciousTitle(md.Title, md.Author) {
		flags = append(flags, FlagSuspiciousTitle)
	}
	return flags
}

// lowQualityCover reports whether a present cover looks like a poor source: not
// decodable, too small, heavily framed, or an odd (non-book-cover) aspect.
func lowQualityCover(cover []byte) bool {
	r := coverimg.Inspect(cover)
	if !r.Decodable {
		return true
	}
	if r.Width < minCoverDim || r.Height < minCoverDim {
		return true
	}
	if r.BorderFraction >= maxBorderFraction {
		return true
	}
	return r.Aspect < minCoverAspect || r.Aspect > maxCoverAspect
}

// suspiciousTitle flags mangled metadata typical of converted MOBIs, e.g. title
// "D&D - Dragonlance - Chronicles 03" with author "Dragons Of Spring Dawning #
// 3". It catches: a title split into several " - " segments, a missing author,
// an author carrying a "#" volume marker, and an author ending in a bare number.
func suspiciousTitle(title, author string) bool {
	t := strings.TrimSpace(title)
	a := strings.TrimSpace(author)
	switch {
	case strings.Count(t, " - ") >= suspiciousSegments:
		return true
	case a == "":
		return true
	case strings.Contains(a, "#"):
		return true
	case endsWithBareNumber(a):
		return true
	default:
		return false
	}
}

// endsWithBareNumber reports whether s's last whitespace-separated token is a
// short run of digits — an author field that trails into a volume number.
func endsWithBareNumber(s string) bool {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return false
	}
	last := fields[len(fields)-1]
	if len(last) == 0 || len(last) > 3 {
		return false
	}
	for _, r := range last {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
