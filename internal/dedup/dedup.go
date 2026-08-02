// Package dedup decides whether a freshly-ingested book is another copy of one
// already on the shelf (LYCM-113).
//
// Book identity is per file: books.file_hash is unique and source_path is
// unique, so two EPUBs of one work satisfy both as long as the bytes differ. A
// re-download that isn't byte-identical, a different translation, an upload
// alongside a folder ingest — each is a distinct row today and the shelf shows
// the book twice. LYCM-66/68/109 taught ingest that the *same* file re-arriving
// under a respelled path is not a new book; this is about two genuinely
// different files.
//
// The matcher only ever suggests. A hit holds the new book in the ingest-QC
// queue carrying the id of what it looks like, and a person decides — two files
// of one work are often deliberate (a better scan, a different translation), and
// silently collapsing them loses one.
//
// It errs toward missing duplicates rather than inventing them, which is the
// opposite of the rest of ingestqc. A missed duplicate leaves the shelf as it is
// today and the next scan gets another chance; a false positive holds a book
// nobody asked about off the shelf, and the failure mode it courts —
// series volumes, which share an author and often a title stem — is exactly the
// case that arrives in bulk.
package dedup

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Candidate is the identity of a book already in the library, as much of it as
// matching needs.
type Candidate struct {
	ID          int64
	Title       string
	Author      string
	Series      string
	SeriesIndex float64
	// WorkID is the resolver work key (/works/OL…W) shared by every edition of a
	// title, via the book's inventory row (LYCM-35). Empty when the book has no
	// ISBN the resolver recognised, which is the common case.
	WorkID string
}

// Match is a suspected duplicate: the existing book, and why it matched.
type Match struct {
	BookID int64
	Reason string
}

// Reasons a match fired, stored on the flagged book so the review UI can say
// what it is claiming rather than just asserting a resemblance.
const (
	ReasonWorkID      = "work"         // same resolver work key: different editions of one title
	ReasonTitleAuthor = "title_author" // same normalized title and author
)

// Find returns the first existing book that looks like another copy of the
// incoming one, or ok=false. Candidates are scanned in order, so a caller that
// wants a stable answer should pass them in a stable one.
//
// Work-key equality is checked across all candidates before any title match, so
// a resolver-confirmed hit is never lost to an earlier lookalike. That is the
// cheap 80%: ingest already resolves the work key for inventory grouping, so for
// any book whose EPUB carries a usable ISBN "same work" is already computed.
func Find(incoming Candidate, existing []Candidate) (Match, bool) {
	if incoming.WorkID != "" {
		for _, c := range existing {
			if c.ID != incoming.ID && c.WorkID == incoming.WorkID {
				return Match{BookID: c.ID, Reason: ReasonWorkID}, true
			}
		}
	}

	title, author := NormalizeTitle(incoming.Title), NormalizeAuthor(incoming.Author)
	if title == "" || author == "" {
		// An empty side would match every book missing the same field. A book
		// with no author is already flagged by ingestqc's suspicious_title.
		return Match{}, false
	}
	for _, c := range existing {
		if c.ID == incoming.ID {
			continue
		}
		if differentVolumes(incoming, c) {
			continue
		}
		if NormalizeTitle(c.Title) == title && NormalizeAuthor(c.Author) == author {
			return Match{BookID: c.ID, Reason: ReasonTitleAuthor}, true
		}
	}
	return Match{}, false
}

// differentVolumes reports whether two books are known to be separate entries in
// one series. Series volumes share an author and often a title stem, and they
// arrive in bulk, so an explicit index disagreement settles it before any title
// comparison: The Wheel of Time #3 is not another copy of #4 however the two
// titles normalize.
//
// It takes an index disagreement to rule a pair out. A missing or zero index is
// no evidence either way — SeriesIndex is 0 when the position is unknown, so
// treating that as "different" would disable title matching for every series
// book whose EPUB omits the number.
func differentVolumes(a, b Candidate) bool {
	return a.SeriesIndex != 0 && b.SeriesIndex != 0 && a.SeriesIndex != b.SeriesIndex
}

// NormalizeTitle reduces a title to a comparable form: case-folded, punctuation
// and accents stripped, whitespace collapsed, and a leading article removed.
//
// It deliberately does NOT strip subtitles. "Dune: Book One" and "Dune: Book
// Two" both reduce to "dune" the moment everything after the colon goes, which
// is the series trap this whole package has to avoid — and the subtitle is
// usually the only thing distinguishing two volumes that share a stem.
func NormalizeTitle(s string) string {
	return stripLeadingArticle(fold(uninvertArticle(s)))
}

// uninvertArticle rewrites the cataloguing convention "Hobbit, The" as "The
// Hobbit", so it reaches stripLeadingArticle as the same title the uninverted
// spelling produces. It has to run before fold, which turns the comma that marks
// the inversion into a space and makes the two indistinguishable.
//
// Only an exact trailing article qualifies. A title that genuinely ends in ",
// The" is close to unheard of, while the inversion is a real convention some
// packagers use and others don't — which is precisely the kind of difference
// that puts one book on the shelf twice.
func uninvertArticle(s string) string {
	i := strings.LastIndex(s, ",")
	if i < 0 {
		return s
	}
	head, tail := strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	if head == "" {
		return s
	}
	switch strings.ToLower(tail) {
	case "the", "a", "an":
		return tail + " " + head
	}
	return s
}

// NormalizeAuthor reduces an author to a comparable form, additionally undoing
// the "Surname, Given" inversion that EPUB metadata uses inconsistently — the
// same person is "Le Guin, Ursula K." in one file and "Ursula K. Le Guin" in the
// next, and those must compare equal or the common case never matches.
func NormalizeAuthor(s string) string {
	// Split before folding: folding drops the comma that marks the inversion.
	if name, surname, ok := splitInverted(s); ok {
		s = name + " " + surname
	}
	return fold(s)
}

// splitInverted recognises "Surname, Given Names" and returns (given, surname).
// Only a single comma qualifies: a multi-comma field is a list of authors or
// something stranger, and guessing at it would fabricate a name.
func splitInverted(s string) (given, surname string, ok bool) {
	if strings.Count(s, ",") != 1 {
		return "", "", false
	}
	surname, given, _ = strings.Cut(s, ",")
	surname, given = strings.TrimSpace(surname), strings.TrimSpace(given)
	if surname == "" || given == "" {
		return "", "", false
	}
	return given, surname, true
}

// fold case-folds, strips accents and punctuation, and collapses whitespace, so
// that spelling differences carrying no meaning stop mattering. Ampersands
// become "and" first, since "Weis & Hickman" and "Weis and Hickman" are the same
// two people and dropping the "&" as punctuation would leave "weis hickman"
// against "weis and hickman".
//
// Accents go via NFD decomposition rather than a lookup table: titles come
// straight from EPUB metadata in whatever form the packager used, so the same
// book arrives as "Misérables" with U+00E9 from one source and "e"+U+0301 from
// the next. That is the NFC/NFD split that duplicated books by path in LYCM-109,
// reaching the shelf through a second door — and decomposing covers scripts a
// Latin-1 table would silently miss.
func fold(s string) string {
	s = strings.ReplaceAll(s, "&", " and ")
	s = norm.NFD.String(s)

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Mn, r):
			// A combining mark the decomposition split off. Dropping it is what
			// makes the two spellings of an accent compare equal.
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			// Punctuation and whitespace alike become a single space; the join
			// below collapses the runs. This is what makes "Dr. Jekyll" and "Dr
			// Jekyll" agree.
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// leadingArticles are dropped from the front of a title so "The Hobbit" and
// "Hobbit" agree — cataloguers and EPUB packagers disagree about whether to keep
// them at all. uninvertArticle has already turned "Hobbit, The" into the leading
// form by this point, so both spellings land here.
var leadingArticles = []string{"the ", "a ", "an "}

func stripLeadingArticle(s string) string {
	for _, a := range leadingArticles {
		if rest, ok := strings.CutPrefix(s, a); ok && rest != "" {
			return rest
		}
	}
	return s
}
