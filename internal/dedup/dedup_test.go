package dedup

import "testing"

func TestNormalizeTitle(t *testing.T) {
	cases := []struct {
		name    string
		a, b    string
		wantSam bool
	}{
		{"case and punctuation", "The Hobbit", "the hobbit!", true},
		{"leading article", "The Hobbit", "Hobbit", true},
		{"inverted article is left alone", "Hobbit, The", "The Hobbit", true},
		{"precomposed vs decomposed accent", "Les Misérables", "Les Misérables", true},
		{"accent vs none", "Les Misérables", "Les Miserables", true},
		{"ampersand spelled out", "Pride & Prejudice", "Pride and Prejudice", true},
		{"collapsed whitespace", "Moby  Dick", "Moby Dick", true},
		{"different books stay different", "Dune", "Dune Messiah", false},
		// The subtitle trap: stripping everything after the colon would collapse
		// these two volumes onto "dune", which is why NormalizeTitle keeps it.
		{"subtitles distinguish volumes", "Dune: Book One", "Dune: Book Two", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, b := NormalizeTitle(tc.a), NormalizeTitle(tc.b)
			if (a == b) != tc.wantSam {
				t.Errorf("NormalizeTitle(%q)=%q, NormalizeTitle(%q)=%q; equal=%v want %v",
					tc.a, a, tc.b, b, a == b, tc.wantSam)
			}
		})
	}
}

func TestNormalizeAuthor(t *testing.T) {
	cases := []struct {
		name    string
		a, b    string
		wantSam bool
	}{
		{"inverted name", "Le Guin, Ursula K.", "Ursula K. Le Guin", true},
		{"case and dots", "URSULA K. LE GUIN", "ursula k le guin", true},
		{"ampersand", "Weis & Hickman", "Weis and Hickman", true},
		{"different authors", "Ursula K. Le Guin", "Ursula Le Guin Jr.", false},
		// Two commas is a list of authors, not an inversion; flipping it would
		// fabricate a name, so it is left to compare literally.
		{"multi-comma is not an inversion", "Weis, Margaret, Hickman, Tracy", "Margaret Weis Tracy Hickman", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, b := NormalizeAuthor(tc.a), NormalizeAuthor(tc.b)
			if (a == b) != tc.wantSam {
				t.Errorf("NormalizeAuthor(%q)=%q, NormalizeAuthor(%q)=%q; equal=%v want %v",
					tc.a, a, tc.b, b, a == b, tc.wantSam)
			}
		})
	}
}

// TestFindSeriesVolumesAreNotDuplicates is the false-positive case that matters
// most: a series arrives in bulk, every volume shares an author, and several
// share a title stem. Flagging each new volume as a copy of the last would make
// the review queue useless exactly when it is busiest.
func TestFindSeriesVolumesAreNotDuplicates(t *testing.T) {
	shelf := []Candidate{
		{ID: 1, Title: "The Eye of the World", Author: "Robert Jordan", Series: "The Wheel of Time", SeriesIndex: 1},
		{ID: 2, Title: "The Great Hunt", Author: "Robert Jordan", Series: "The Wheel of Time", SeriesIndex: 2},
	}
	incoming := Candidate{Title: "The Dragon Reborn", Author: "Robert Jordan", Series: "The Wheel of Time", SeriesIndex: 3}
	if m, ok := Find(incoming, shelf); ok {
		t.Errorf("volume 3 flagged as a copy of book %d (%s)", m.BookID, m.Reason)
	}

	// Same title as an existing volume but a different declared index: still a
	// different volume, whatever the titles do.
	sameTitle := Candidate{Title: "The Great Hunt", Author: "Robert Jordan", Series: "The Wheel of Time", SeriesIndex: 7}
	if m, ok := Find(sameTitle, shelf); ok {
		t.Errorf("volume 7 flagged as a copy of book %d despite a different series index", m.BookID)
	}

	// But a genuine second copy of volume 2 — same index — is a duplicate.
	reissue := Candidate{Title: "The Great Hunt", Author: "Robert Jordan", Series: "The Wheel of Time", SeriesIndex: 2}
	m, ok := Find(reissue, shelf)
	if !ok || m.BookID != 2 {
		t.Errorf("second copy of volume 2 = %+v (ok=%v), want book 2", m, ok)
	}
}

// TestFindMissingIndexStillMatches: SeriesIndex is 0 when the position is
// unknown, and plenty of EPUBs omit it. Treating a missing index as "different
// volume" would disable title matching for exactly the books most likely to be
// re-downloaded.
func TestFindMissingIndexStillMatches(t *testing.T) {
	shelf := []Candidate{{ID: 9, Title: "The Left Hand of Darkness", Author: "Ursula K. Le Guin"}}
	incoming := Candidate{Title: "Left Hand of Darkness", Author: "Le Guin, Ursula K."}

	m, ok := Find(incoming, shelf)
	if !ok || m.BookID != 9 || m.Reason != ReasonTitleAuthor {
		t.Fatalf("Find = %+v (ok=%v), want book 9 by %s", m, ok, ReasonTitleAuthor)
	}
}

// TestFindWorkIDWins: a resolver-confirmed work key beats a title lookalike that
// appears earlier in the list, and matches across editions whose titles differ.
func TestFindWorkIDWins(t *testing.T) {
	shelf := []Candidate{
		{ID: 1, Title: "Something Else Entirely", Author: "Anonymous"},
		{ID: 2, Title: "Hamlet, Prince of Denmark", Author: "William Shakespeare", WorkID: "/works/OL1W"},
	}
	incoming := Candidate{Title: "Hamlet", Author: "Shakespeare", WorkID: "/works/OL1W"}

	m, ok := Find(incoming, shelf)
	if !ok || m.BookID != 2 || m.Reason != ReasonWorkID {
		t.Fatalf("Find = %+v (ok=%v), want book 2 by %s", m, ok, ReasonWorkID)
	}
}

// TestFindEmptyFieldsNeverMatch: a book missing a title or author would
// otherwise match every other book missing the same field, turning one bad
// ingest into a flag on the next.
func TestFindEmptyFieldsNeverMatch(t *testing.T) {
	shelf := []Candidate{
		{ID: 1, Title: "Untitled", Author: ""},
		{ID: 2, Title: "", Author: "Anonymous"},
	}
	for _, incoming := range []Candidate{
		{Title: "Untitled", Author: ""},
		{Title: "", Author: "Anonymous"},
		{Title: "  ", Author: "  "},
	} {
		if m, ok := Find(incoming, shelf); ok {
			t.Errorf("Find(%+v) matched book %d; an empty field must not match", incoming, m.BookID)
		}
	}
}

// TestFindSkipsItself matters for the report command, which runs every existing
// book against the whole shelf — including its own row.
func TestFindSkipsItself(t *testing.T) {
	shelf := []Candidate{{ID: 5, Title: "Piranesi", Author: "Susanna Clarke", WorkID: "/works/OL5W"}}
	self := Candidate{ID: 5, Title: "Piranesi", Author: "Susanna Clarke", WorkID: "/works/OL5W"}

	if m, ok := Find(self, shelf); ok {
		t.Errorf("book matched itself: %+v", m)
	}
}

// TestFindNoMatch: the ordinary case, a genuinely new book on a populated shelf.
func TestFindNoMatch(t *testing.T) {
	shelf := []Candidate{
		{ID: 1, Title: "Piranesi", Author: "Susanna Clarke"},
		{ID: 2, Title: "Dune", Author: "Frank Herbert"},
	}
	if m, ok := Find(Candidate{Title: "Annihilation", Author: "Jeff VanderMeer"}, shelf); ok {
		t.Errorf("unrelated book matched %+v", m)
	}
}
