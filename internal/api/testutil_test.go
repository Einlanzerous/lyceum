package api

import (
	"archive/zip"
	"bytes"
	"testing"
)

// syntheticEPUB synthesizes a small but structurally valid EPUB with two spine
// chapters and a nav doc. It is shared by the delivery (LYCM-402) and Eidolon
// (LYCM-403/404) tests, which rely on a known spine order and chapter text.
// (The sibling sampleEPUB helper in upload_test.go instead loads the real
// fixtures under epub/testdata, whose spine we don't control.)
//
// Spine order is [chapter1.xhtml, chapter2.xhtml] (the nav is not in the
// spine), so a CFI spine step of /6/2 resolves to chapter 1 and /6/4 to
// chapter 2 (step/2 - 1 = 0-based spine index).
func syntheticEPUB(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, contents string) {
		t.Helper()
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(contents)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}

	write("mimetype", "application/epub+zip")
	write("META-INF/container.xml", `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`)

	write("OEBPS/content.opf", `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>The Test Book</dc:title>
    <dc:creator>A. Tester</dc:creator>
    <dc:language>en</dc:language>
    <dc:identifier id="bookid">urn:uuid:test-0001</dc:identifier>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="c1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="chapter2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="c1"/>
    <itemref idref="c2"/>
  </spine>
</package>`)

	write("OEBPS/nav.xhtml", `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><body><nav epub:type="toc"><ol>
  <li><a href="chapter1.xhtml">One</a></li>
  <li><a href="chapter2.xhtml">Two</a></li>
</ol></nav></body></html>`)

	write("OEBPS/chapter1.xhtml", `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>One</title></head>
<body>
  <h1>Chapter One</h1>
  <p>Call me Ishmael. Some years ago&#8212;never mind how long precisely.</p>
  <p>Having little or no money in my purse.</p>
</body></html>`)

	write("OEBPS/chapter2.xhtml", `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Two</title></head>
<body>
  <h1>Chapter Two</h1>
  <p>I stuffed a shirt or two into my old carpet-bag.</p>
</body></html>`)

	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}
