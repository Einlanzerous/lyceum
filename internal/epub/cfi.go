package epub

// This file implements a small, self-contained parser and validator for EPUB
// Canonical Fragment Identifiers (CFIs). It deals purely with the string and
// structural form of a CFI: it validates the epubcfi(...) syntax, parses a CFI
// into a comparable structure, and provides ordering so two CFIs in the same
// book can be compared for best-effort reading-progress purposes.
//
// It intentionally does NOT resolve a CFI against an actual document tree, nor
// does it render anything. It also has no dependency on internal/store.
//
// Grammar handled (a practical subset of the EPUB CFI spec):
//
//	cfi        := "epubcfi(" path ")"
//	path       := location | location "," location "," location   (range)
//	location   := step+
//	step       := "/" number ( "[" assertion "]" )? ( ":" number )? ( "~" number )? ( "@" number ":" number )?
//	assertion  := id text, possibly "value" or "value,value" with character
//	              escaping via "^". Empty assertions ("[]") are rejected.
//
// A range CFI is "parent,start,end": the start and end paths are taken relative
// to the shared parent prefix. We model that by concatenating parent+start and
// parent+end into two absolute paths for comparison purposes.

import (
	"fmt"
	"strconv"
	"strings"
)

// CFIStep is a single "/N[id]:offset" component of a CFI path. Index is the
// step integer (the value after the slash). Offset is the trailing character
// offset introduced by ":N", or -1 when absent. Assertion is the bracketed id
// assertion text, with CFI "^" escaping already decoded; it is empty when no
// assertion was present.
type CFIStep struct {
	Index     int
	Offset    int // character offset from ":N"; -1 if none
	Assertion string
	// Indirection is true when this step immediately follows a "!" jump into a
	// referenced document (e.g. the spine itemref's content document). It marks
	// the boundary between the package-level path and the in-document path.
	Indirection bool
}

// CFI is a parsed EPUB CFI. For a non-range CFI, Start holds the steps and End
// is nil. For a range CFI, Start and End hold the two absolute paths (each the
// shared parent prefix followed by the respective branch) and IsRange is true.
type CFI struct {
	Raw     string
	Start   []CFIStep
	End     []CFIStep
	IsRange bool
}

// ParseCFI validates and parses an epubcfi(...) string. It returns an error for
// any malformed input.
func ParseCFI(s string) (*CFI, error) {
	inner, err := unwrap(s)
	if err != nil {
		return nil, err
	}

	parts := splitTopLevel(inner)
	switch len(parts) {
	case 1:
		steps, err := parsePath(parts[0])
		if err != nil {
			return nil, err
		}
		return &CFI{Raw: s, Start: steps}, nil
	case 3:
		parent, err := parsePath(parts[0])
		if err != nil {
			return nil, fmt.Errorf("cfi: range parent: %w", err)
		}
		start, err := parsePath(parts[1])
		if err != nil {
			return nil, fmt.Errorf("cfi: range start: %w", err)
		}
		end, err := parsePath(parts[2])
		if err != nil {
			return nil, fmt.Errorf("cfi: range end: %w", err)
		}
		// A range's parent must be a non-empty common prefix, and both branches
		// must be non-empty.
		if len(parent) == 0 || len(start) == 0 || len(end) == 0 {
			return nil, fmt.Errorf("cfi: range parts must be non-empty")
		}
		return &CFI{
			Raw:     s,
			IsRange: true,
			Start:   append(append([]CFIStep{}, parent...), start...),
			End:     append(append([]CFIStep{}, parent...), end...),
		}, nil
	default:
		return nil, fmt.Errorf("cfi: a CFI has 1 path or 3 comma-separated range paths, got %d", len(parts))
	}
}

// ValidateCFI reports whether s is a syntactically valid epubcfi(...) string.
func ValidateCFI(s string) error {
	_, err := ParseCFI(s)
	return err
}

// unwrap strips the "epubcfi(" prefix and ")" suffix, returning the inner path
// text. The inner text must be non-empty.
func unwrap(s string) (string, error) {
	const prefix = "epubcfi("
	if !strings.HasPrefix(s, prefix) {
		return "", fmt.Errorf("cfi: missing %q wrapper", prefix)
	}
	if !strings.HasSuffix(s, ")") {
		return "", fmt.Errorf("cfi: missing closing %q", ")")
	}
	inner := s[len(prefix) : len(s)-1]
	if inner == "" {
		return "", fmt.Errorf("cfi: empty CFI path")
	}
	return inner, nil
}

// splitTopLevel splits on commas that are not inside a "[...]" assertion or
// escaped with "^".
func splitTopLevel(s string) []string {
	var parts []string
	var cur strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '^' && i+1 < len(s):
			// Escaped char: keep both bytes verbatim, skip special handling.
			cur.WriteByte(c)
			cur.WriteByte(s[i+1])
			i++
		case c == '[':
			depth++
			cur.WriteByte(c)
		case c == ']':
			if depth > 0 {
				depth--
			}
			cur.WriteByte(c)
		case c == ',' && depth == 0:
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	parts = append(parts, cur.String())
	return parts
}

// parsePath parses a sequence of steps. A path must start with "/" and contain
// at least one step.
func parsePath(s string) ([]CFIStep, error) {
	if s == "" {
		return nil, fmt.Errorf("cfi: empty path component")
	}
	if s[0] != '/' {
		return nil, fmt.Errorf("cfi: path must begin with '/', got %q", s)
	}

	var steps []CFIStep
	i := 0
	indirect := false
	for i < len(s) {
		// An indirection step "!" (a jump into a referenced document, e.g. the
		// content document referenced by a spine itemref) precedes the next
		// "/N" step. We flag that next step so callers can locate the
		// package/in-document boundary (see CFI.SpineIndex).
		if s[i] == '!' {
			i++
			if i >= len(s) || s[i] != '/' {
				return nil, fmt.Errorf("cfi: '!' indirection must be followed by a step in %q", s)
			}
			indirect = true
		}
		if s[i] != '/' {
			return nil, fmt.Errorf("cfi: expected '/' at position %d in %q", i, s)
		}
		i++ // consume '/'

		// Read the step index digits.
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return nil, fmt.Errorf("cfi: missing step number in %q", s)
		}
		idx, err := strconv.Atoi(s[start:i])
		if err != nil {
			return nil, fmt.Errorf("cfi: bad step number in %q: %w", s, err)
		}

		step := CFIStep{Index: idx, Offset: -1, Indirection: indirect}
		indirect = false

		// Optional id assertion "[...]".
		if i < len(s) && s[i] == '[' {
			j := i + 1
			for j < len(s) && s[j] != ']' {
				if s[j] == '^' { // skip escaped char
					j++
				}
				j++
			}
			if j >= len(s) || s[j] != ']' {
				return nil, fmt.Errorf("cfi: unterminated assertion in %q", s)
			}
			raw := s[i+1 : j]
			if raw == "" {
				return nil, fmt.Errorf("cfi: empty assertion '[]' in %q", s)
			}
			step.Assertion = unescape(raw)
			i = j + 1
		}

		// Optional character offset ":N".
		if i < len(s) && s[i] == ':' {
			i++
			ns := i
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			if i == ns {
				return nil, fmt.Errorf("cfi: missing offset after ':' in %q", s)
			}
			off, err := strconv.Atoi(s[ns:i])
			if err != nil {
				return nil, fmt.Errorf("cfi: bad offset in %q: %w", s, err)
			}
			step.Offset = off
		}

		// Optional temporal "~N(.N)?" and spatial "@N:N" terminal assertions.
		// These only ever appear on the final step; we accept and skip them so
		// validation does not reject otherwise-legal CFIs, but we do not use
		// them for ordering.
		if i < len(s) && s[i] == '~' {
			i++
			i = skipNumber(s, i)
		}
		if i < len(s) && s[i] == '@' {
			i++
			i = skipNumber(s, i)
			if i < len(s) && s[i] == ':' {
				i++
				i = skipNumber(s, i)
			}
		}

		steps = append(steps, step)
	}

	if len(steps) == 0 {
		return nil, fmt.Errorf("cfi: no steps in %q", s)
	}
	return steps, nil
}

// skipNumber advances past a run of digits and an optional fractional part.
func skipNumber(s string, i int) int {
	for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == '.') {
		i++
	}
	return i
}

// unescape decodes CFI "^" escaping: "^x" becomes "x".
func unescape(s string) string {
	if !strings.ContainsRune(s, '^') {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '^' && i+1 < len(s) {
			i++
			b.WriteByte(s[i])
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Compare orders two parsed CFIs that refer to positions in the same book. It
// returns a negative value if a precedes b, zero if they denote the same
// position, and a positive value if a follows b. For range CFIs the comparison
// uses the range start path. The comparison is best-effort: it walks step
// indices then character offsets lexicographically; an id assertion does not
// affect ordering.
func Compare(a, b *CFI) int {
	return comparePath(a.Start, b.Start)
}

// Less reports whether a precedes b in reading order.
func Less(a, b *CFI) bool {
	return Compare(a, b) < 0
}

// SpineIndex returns the 0-based spine index the CFI points into, derived from
// the spine itemref step. In EPUB CFIs the package's spine element is the first
// step (conventionally /6) and the itemref is the next step, an even integer
// whose value is 2*(index+1); so itemref /2 maps to spine index 0, /4 to 1, and
// so on. The itemref is the step immediately before the first "!" indirection
// (the jump into the chapter's content document); absent an indirection it
// falls back to the second step. ok is false when the path is too short or the
// itemref step is not a valid even index.
func (c *CFI) SpineIndex() (index int, ok bool) {
	steps := c.Start
	boundary := -1
	for i, s := range steps {
		if s.Indirection {
			boundary = i
			break
		}
	}

	var itemref CFIStep
	switch {
	case boundary >= 1:
		itemref = steps[boundary-1]
	case len(steps) >= 2:
		itemref = steps[1]
	default:
		return 0, false
	}

	if itemref.Index < 2 || itemref.Index%2 != 0 {
		return 0, false
	}
	return itemref.Index/2 - 1, true
}

func comparePath(a, b []CFIStep) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if c := a[i].Index - b[i].Index; c != 0 {
			return sign(c)
		}
		// Same step: a trailing character offset only matters when both share
		// the same step depth, so compare offsets here.
		ao, bo := a[i].Offset, b[i].Offset
		if ao < 0 {
			ao = 0
		}
		if bo < 0 {
			bo = 0
		}
		if c := ao - bo; c != 0 {
			return sign(c)
		}
	}
	// Shared prefix is equal: the deeper path comes later.
	return sign(len(a) - len(b))
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}
