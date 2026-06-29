package epub

import (
	"bufio"
	"encoding/xml"
	"io"
	"strings"
)

// blockElements are HTML elements that introduce a paragraph break in the
// extracted plain text: closing one flushes the current line. This preserves
// the paragraph boundaries TTS engines rely on for natural pacing.
var blockElements = map[string]bool{
	"p": true, "div": true, "br": true, "li": true, "tr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"section": true, "article": true, "header": true, "footer": true,
	"blockquote": true, "pre": true, "figcaption": true, "title": true,
}

// skipElements have their text content dropped entirely (scripts, styles, and
// embedded SVG/markup that would otherwise leak symbols into the narration).
var skipElements = map[string]bool{
	"script": true, "style": true, "head": true,
}

// ExtractText streams the plain-text content of an XHTML/HTML document to w,
// stripping markup while preserving paragraph boundaries (block elements become
// blank-line-separated paragraphs). Inline whitespace is collapsed. It parses
// leniently so real-world EPUB XHTML — HTML entities, void elements — does not
// abort extraction.
//
// Output is streamed paragraph by paragraph rather than buffered whole, so
// arbitrarily large chapters stay memory-light.
func ExtractText(w io.Writer, r io.Reader) error {
	bw := bufio.NewWriter(w)

	dec := xml.NewDecoder(r)
	// Tolerate HTML: map named entities (&nbsp; etc.), auto-close void elements,
	// and don't choke on minor non-strictness.
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity

	var para strings.Builder // accumulates the current paragraph's inline text
	skipDepth := 0           // >0 while inside a skipped subtree

	flush := func() error {
		text := collapseSpaces(para.String())
		para.Reset()
		if text == "" {
			return nil
		}
		if _, err := bw.WriteString(text); err != nil {
			return err
		}
		// Blank line between paragraphs for clear TTS sentence/paragraph breaks.
		_, err := bw.WriteString("\n\n")
		return err
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			if skipElements[name] {
				skipDepth++
				continue
			}
			// Flush on entering a block element too, so a wrapper's loose text
			// (e.g. "A" in <div>A<p>B</p></div>) doesn't merge with the nested
			// block's text. flush() is a no-op when the buffer is empty, so this
			// adds no spurious blank lines.
			if skipDepth == 0 && blockElements[name] {
				if err := flush(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			name := strings.ToLower(t.Name.Local)
			if skipElements[name] {
				if skipDepth > 0 {
					skipDepth--
				}
				continue
			}
			if skipDepth == 0 && blockElements[name] {
				if err := flush(); err != nil {
					return err
				}
			}
		case xml.CharData:
			if skipDepth == 0 {
				para.Write(t)
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return bw.Flush()
}

// collapseSpaces trims and collapses runs of whitespace (including newlines from
// source indentation) into single spaces, yielding a clean one-line paragraph.
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
