package epub

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// twoChapterEPUB builds an EPUB whose spine is [chapter1.xhtml, chapter2.xhtml]
// (OPF under OEBPS/), for exercising spine resolution and text extraction.
func twoChapterEPUB(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, contents string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(contents)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("META-INF/container.xml", `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`)
	write("OEBPS/content.opf", `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>T</dc:title><dc:identifier id="bookid">x</dc:identifier></metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="c1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="text/chapter2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="c1"/>
    <itemref idref="c2"/>
  </spine>
</package>`)
	write("OEBPS/chapter1.xhtml", `<?xml version="1.0"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>One</title>
<style>.x{color:red}</style></head>
<body>
  <h1>Chapter One</h1>
  <p>Call me Ishmael&#8212;some years ago.</p>
  <p>Having    little   money.</p>
  <script>var x = 1;</script>
</body></html>`)
	write("OEBPS/text/chapter2.xhtml", `<?xml version="1.0"?>
<html xmlns="http://www.w3.org/1999/xhtml"><body><p>Second chapter only.</p></body></html>`)
	if err := zw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return buf.Bytes()
}

func TestReaderSpine(t *testing.T) {
	rd, err := OpenReader(bytes.NewReader(twoChapterEPUB(t)), int64(len(twoChapterEPUB(t))))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer rd.Close()

	spine := rd.Spine()
	if len(spine) != 2 {
		t.Fatalf("spine len = %d, want 2", len(spine))
	}
	if spine[0].Href != "OEBPS/chapter1.xhtml" {
		t.Errorf("spine[0].Href = %q", spine[0].Href)
	}
	if spine[1].Href != "OEBPS/text/chapter2.xhtml" {
		t.Errorf("spine[1].Href = %q", spine[1].Href)
	}

	// Lookup by full path and by basename.
	if _, idx, ok := rd.FindSpineItem("OEBPS/chapter1.xhtml"); !ok || idx != 0 {
		t.Errorf("FindSpineItem(full) = idx %d ok %v", idx, ok)
	}
	if _, idx, ok := rd.FindSpineItem("chapter2.xhtml"); !ok || idx != 1 {
		t.Errorf("FindSpineItem(basename) = idx %d ok %v", idx, ok)
	}
	if _, _, ok := rd.FindSpineItem("nope.xhtml"); ok {
		t.Error("FindSpineItem(missing) should fail")
	}
}

func TestReaderReadContent(t *testing.T) {
	data := twoChapterEPUB(t)
	rd, err := OpenReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer rd.Close()

	b, err := rd.ReadContent("OEBPS/text/chapter2.xhtml")
	if err != nil {
		t.Fatalf("ReadContent: %v", err)
	}
	if !strings.Contains(string(b), "Second chapter only.") {
		t.Errorf("content = %q", b)
	}
}

func TestExtractText(t *testing.T) {
	data := twoChapterEPUB(t)
	rd, err := OpenReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer rd.Close()

	content, err := rd.ReadContent("OEBPS/chapter1.xhtml")
	if err != nil {
		t.Fatalf("ReadContent: %v", err)
	}
	var out bytes.Buffer
	if err := ExtractText(&out, bytes.NewReader(content)); err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	text := out.String()

	// HTML entity decoded to an em dash; tags stripped; whitespace collapsed.
	if !strings.Contains(text, "Call me Ishmael—some years ago.") {
		t.Errorf("missing/!decoded first paragraph: %q", text)
	}
	if !strings.Contains(text, "Having little money.") {
		t.Errorf("whitespace not collapsed: %q", text)
	}
	// script/style content must be dropped.
	if strings.Contains(text, "var x") || strings.Contains(text, "color:red") {
		t.Errorf("skipped element content leaked: %q", text)
	}
	// Paragraphs separated by a blank line.
	if !strings.Contains(text, "ago.\n\n") {
		t.Errorf("paragraph boundary not preserved: %q", text)
	}
}

func TestExtractTextMixedContent(t *testing.T) {
	// A wrapper's loose text must not merge with a nested block's text.
	in := `<html><body><div>Wrapper lead.<p>Nested para.</p>Wrapper tail.</div></body></html>`
	var out bytes.Buffer
	if err := ExtractText(&out, strings.NewReader(in)); err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Wrapper lead.", "Nested para.", "Wrapper tail."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	// The three pieces must be on separate paragraphs, not run together.
	if strings.Contains(got, "Wrapper lead. Nested") || strings.Contains(got, "Nested para. Wrapper") {
		t.Errorf("paragraphs merged: %q", got)
	}
}

func TestSpineIndex(t *testing.T) {
	cases := []struct {
		cfi  string
		want int
		ok   bool
	}{
		{"epubcfi(/6/2[c1]!/4/2/1:0)", 0, true},  // itemref /2 -> spine 0
		{"epubcfi(/6/4[c2]!/4/2/1:0)", 1, true},  // itemref /4 -> spine 1
		{"epubcfi(/6/14[chap05]!/4/2)", 6, true}, // itemref /14 -> spine 6
		{"epubcfi(/6/4/2:7)", 1, true},           // no indirection: fall back to 2nd step
		{"epubcfi(/4)", 0, false},                // too short
		{"epubcfi(/6/3!/4)", 0, false},           // odd itemref step is invalid
	}
	for _, tc := range cases {
		c, err := ParseCFI(tc.cfi)
		if err != nil {
			t.Fatalf("ParseCFI(%q): %v", tc.cfi, err)
		}
		idx, ok := c.SpineIndex()
		if ok != tc.ok || (ok && idx != tc.want) {
			t.Errorf("SpineIndex(%q) = (%d,%v), want (%d,%v)", tc.cfi, idx, ok, tc.want, tc.ok)
		}
	}
}
