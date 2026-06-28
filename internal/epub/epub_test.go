package epub

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// sampleSpec describes an EPUB to synthesize for tests.
type sampleSpec struct {
	opfPath    string // path of the OPF inside the zip
	title      string
	creator    string
	language   string
	identifier string
	// coverStyle selects how the cover is declared: "meta" (EPUB2 legacy),
	// "property" (EPUB3 cover-image), or "fallback" (no declaration, first
	// image item is used).
	coverStyle string
	coverBytes []byte
}

// buildEPUB synthesizes a minimal but structurally valid EPUB zip in memory.
func buildEPUB(t *testing.T, s sampleSpec) []byte {
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
	writeBytes := func(name string, b []byte) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := w.Write(b); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// The mimetype entry is conventionally first and stored, but not required
	// for parsing; include it for realism.
	write("mimetype", "application/epub+zip")

	write("META-INF/container.xml", `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="`+s.opfPath+`" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`)

	dir := filepath.ToSlash(filepath.Dir(s.opfPath))
	coverHref := "images/cover.png"
	coverItem := `<item id="cover-img" href="` + coverHref + `" media-type="image/png"`
	switch s.coverStyle {
	case "property":
		coverItem += ` properties="cover-image"`
	}
	coverItem += `/>`

	metaCover := ""
	if s.coverStyle == "meta" {
		metaCover = `<meta name="cover" content="cover-img"/>`
	}

	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>` + s.title + `</dc:title>
    <dc:creator>` + s.creator + `</dc:creator>
    <dc:language>` + s.language + `</dc:language>
    <dc:identifier id="bookid">` + s.identifier + `</dc:identifier>
    ` + metaCover + `
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    ` + coverItem + `
  </manifest>
  <spine>
    <itemref idref="nav"/>
  </spine>
</package>`
	write(s.opfPath, opf)

	coverPath := coverHref
	if dir != "." && dir != "" {
		coverPath = dir + "/" + coverHref
	}
	writeBytes(coverPath, s.coverBytes)

	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// fakePNG is a small non-empty byte blob standing in for a cover image. The
// parser does not decode it, so any bytes suffice.
var fakePNG = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x42}, 64)...)

func TestParse_DublinCoreAndCover(t *testing.T) {
	cases := []struct {
		name string
		spec sampleSpec
	}{
		{
			name: "epub2 meta cover, OPF at root",
			spec: sampleSpec{
				opfPath:    "content.opf",
				title:      "The Iliad",
				creator:    "Homer",
				language:   "en",
				identifier: "urn:isbn:9780140275360",
				coverStyle: "meta",
				coverBytes: fakePNG,
			},
		},
		{
			name: "epub3 cover-image property, OPF in subdir",
			spec: sampleSpec{
				opfPath:    "OEBPS/package.opf",
				title:      "Meditations",
				creator:    "Marcus Aurelius",
				language:   "la",
				identifier: "urn:uuid:1234",
				coverStyle: "property",
				coverBytes: fakePNG,
			},
		},
		{
			name: "fallback to first image item",
			spec: sampleSpec{
				opfPath:    "OEBPS/content.opf",
				title:      "Republic",
				creator:    "Plato",
				language:   "grc",
				identifier: "urn:uuid:5678",
				coverStyle: "fallback",
				coverBytes: fakePNG,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := buildEPUB(t, tc.spec)
			md, err := ParseBytes(data)
			if err != nil {
				t.Fatalf("ParseBytes: %v", err)
			}
			if md.Title != tc.spec.title {
				t.Errorf("Title = %q, want %q", md.Title, tc.spec.title)
			}
			if md.Author != tc.spec.creator {
				t.Errorf("Author = %q, want %q", md.Author, tc.spec.creator)
			}
			if md.Language != tc.spec.language {
				t.Errorf("Language = %q, want %q", md.Language, tc.spec.language)
			}
			if md.Identifier != tc.spec.identifier {
				t.Errorf("Identifier = %q, want %q", md.Identifier, tc.spec.identifier)
			}
			if !md.HasCover() {
				t.Fatalf("expected a non-empty cover")
			}
			if !bytes.Equal(md.CoverData, tc.spec.coverBytes) {
				t.Errorf("cover bytes mismatch: got %d bytes", len(md.CoverData))
			}
			if md.CoverMediaType != "image/png" {
				t.Errorf("CoverMediaType = %q, want image/png", md.CoverMediaType)
			}
		})
	}
}

func TestParseFile_Fixtures(t *testing.T) {
	matches, err := filepath.Glob("testdata/*.epub")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Skip("no fixtures in testdata/")
	}
	for _, p := range matches {
		t.Run(filepath.Base(p), func(t *testing.T) {
			md, err := ParseFile(p)
			if err != nil {
				t.Fatalf("ParseFile(%s): %v", p, err)
			}
			if md.Title == "" {
				t.Errorf("%s: empty title", p)
			}
			if md.Author == "" {
				t.Errorf("%s: empty author", p)
			}
			if md.Language == "" {
				t.Errorf("%s: empty language", p)
			}
			if !md.HasCover() {
				t.Errorf("%s: expected a non-empty cover", p)
			}
		})
	}
}

func TestParse_NotAZip(t *testing.T) {
	if _, err := ParseBytes([]byte("not a zip")); err == nil {
		t.Fatal("expected error for non-zip input")
	}
}

func TestParseFile_Missing(t *testing.T) {
	if _, err := ParseFile(filepath.Join(t.TempDir(), "nope.epub")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// Ensure the synthesized sample round-trips through a real file too.
func TestParse_ReaderAt(t *testing.T) {
	data := buildEPUB(t, sampleSpec{
		opfPath:    "OEBPS/content.opf",
		title:      "Nicomachean Ethics",
		creator:    "Aristotle",
		language:   "grc",
		identifier: "urn:uuid:abcd",
		coverStyle: "property",
		coverBytes: fakePNG,
	})
	f := filepath.Join(t.TempDir(), "sample.epub")
	if err := os.WriteFile(f, data, 0o600); err != nil {
		t.Fatal(err)
	}
	md, err := ParseFile(f)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if md.Title != "Nicomachean Ethics" {
		t.Errorf("Title = %q", md.Title)
	}
}
