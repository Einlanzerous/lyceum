package epub

import "testing"

func TestValidateCFI_Valid(t *testing.T) {
	valid := []string{
		"epubcfi(/6/4[chap01ref]!/4[body01]/10/2/1:3)",
		"epubcfi(/6/4)",
		"epubcfi(/6/4[chap01ref])",
		"epubcfi(/6/4/2:0)",
		"epubcfi(/2/4/6/8)",
		// Range CFI: parent, start branch, end branch.
		"epubcfi(/6/4[chap01ref]!/4[body01]/10,/2/1:1,/3:4)",
		// Escaped characters inside an assertion.
		"epubcfi(/6/4[chap^[01^]]/2:5)",
		// Temporal/spatial terminal assertions on the final step.
		"epubcfi(/6/4/2~3.5@1:2)",
	}
	for _, s := range valid {
		if err := ValidateCFI(s); err != nil {
			t.Errorf("ValidateCFI(%q) = %v, want nil", s, err)
		}
	}
}

func TestValidateCFI_Malformed(t *testing.T) {
	bad := []string{
		"",                              // empty
		"/6/4/2",                        // no wrapper
		"epubcfi(/6/4",                  // missing close paren
		"epubcfi()",                     // empty path
		"epubcfi(6/4)",                  // does not start with '/'
		"epubcfi(/6//4)",                // empty step (missing number)
		"epubcfi(/6/4[])",               // empty assertion
		"epubcfi(/6/4[chap)",            // unterminated assertion
		"epubcfi(/6/4:)",                // colon with no offset
		"epubcfi(/6/x)",                 // non-numeric step
		"epubcfi(/6/4,/2)",              // 2 comma parts (not 1 or 3)
		"epubcfi(/6/4,,/2)",             // empty range branch
		"epubcfi(/6/4,/2,/3,/4)",        // 4 comma parts
		"notepubcfi(/6/4)",              // wrong prefix
	}
	for _, s := range bad {
		if err := ValidateCFI(s); err == nil {
			t.Errorf("ValidateCFI(%q) = nil, want error", s)
		}
	}
}

func TestParseCFI_Structure(t *testing.T) {
	c, err := ParseCFI("epubcfi(/6/4[chap01ref]/2:7)")
	if err != nil {
		t.Fatalf("ParseCFI error: %v", err)
	}
	if c.IsRange {
		t.Fatalf("expected non-range CFI")
	}
	want := []CFIStep{
		{Index: 6, Offset: -1},
		{Index: 4, Offset: -1, Assertion: "chap01ref"},
		{Index: 2, Offset: 7},
	}
	if len(c.Start) != len(want) {
		t.Fatalf("got %d steps, want %d", len(c.Start), len(want))
	}
	for i := range want {
		if c.Start[i] != want[i] {
			t.Errorf("step %d = %+v, want %+v", i, c.Start[i], want[i])
		}
	}
}

func TestParseCFI_Range(t *testing.T) {
	c, err := ParseCFI("epubcfi(/6/4[body]/10,/2/1:1,/3:4)")
	if err != nil {
		t.Fatalf("ParseCFI error: %v", err)
	}
	if !c.IsRange {
		t.Fatalf("expected range CFI")
	}
	// Start absolute path = parent + start branch.
	wantStart := []int{6, 4, 10, 2, 1}
	wantEnd := []int{6, 4, 10, 3}
	if got := indices(c.Start); !equalInts(got, wantStart) {
		t.Errorf("start indices = %v, want %v", got, wantStart)
	}
	if got := indices(c.End); !equalInts(got, wantEnd) {
		t.Errorf("end indices = %v, want %v", got, wantEnd)
	}
	// Range start must precede range end.
	if comparePath(c.Start, c.End) >= 0 {
		t.Errorf("range start should precede end")
	}
}

func TestCompareOrdering(t *testing.T) {
	cases := []struct {
		a, b string
		want int // -1 a<b, 0 equal, 1 a>b
	}{
		{"epubcfi(/6/4/2:1)", "epubcfi(/6/4/2:5)", -1}, // earlier offset
		{"epubcfi(/6/4/2)", "epubcfi(/6/8/2)", -1},     // earlier step
		{"epubcfi(/6/4)", "epubcfi(/6/4/2)", -1},       // shallower precedes deeper
		{"epubcfi(/6/8/2)", "epubcfi(/6/4/2)", 1},      // later step
		{"epubcfi(/6/4/2:3)", "epubcfi(/6/4/2:3)", 0},  // equal
		// Assertions must not affect ordering.
		{"epubcfi(/6/4[a]/2)", "epubcfi(/6/4[b]/2)", 0},
	}
	for _, tc := range cases {
		a, err := ParseCFI(tc.a)
		if err != nil {
			t.Fatalf("ParseCFI(%q): %v", tc.a, err)
		}
		b, err := ParseCFI(tc.b)
		if err != nil {
			t.Fatalf("ParseCFI(%q): %v", tc.b, err)
		}
		if got := sign(Compare(a, b)); got != tc.want {
			t.Errorf("Compare(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		// Less must be consistent with Compare.
		if wantLess := tc.want < 0; Less(a, b) != wantLess {
			t.Errorf("Less(%q,%q) = %v, want %v", tc.a, tc.b, Less(a, b), wantLess)
		}
	}
}

func indices(steps []CFIStep) []int {
	out := make([]int, len(steps))
	for i, s := range steps {
		out[i] = s.Index
	}
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
