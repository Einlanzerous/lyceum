package epub

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// SpineItem is one entry in an EPUB's reading order, resolved from a spine
// itemref to its content document. Href is the document's full path within the
// zip (the OPF directory joined with the manifest href), which is also the
// identifier the Eidolon chapter endpoint accepts.
type SpineItem struct {
	IDRef     string
	Href      string
	MediaType string
}

// Reader is an opened EPUB positioned for spine traversal and content-document
// reads. It keeps the underlying archive open, so callers must Close it.
type Reader struct {
	zr     *zip.Reader
	closer io.Closer // underlying *os.File when opened from disk; nil for in-memory
	opfDir string
	spine  []SpineItem
}

// OpenFile opens the EPUB at path for reading. The returned Reader holds the
// file open until Close is called.
func OpenFile(p string) (*Reader, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("epub: open %s: %w", p, err)
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("epub: stat %s: %w", p, err)
	}
	rd, err := newReader(f, fi.Size())
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	rd.closer = f
	return rd, nil
}

// OpenReader opens an EPUB from a random-access reader of the given size. The
// caller retains ownership of r; Close on the returned Reader is a no-op for
// the data source.
func OpenReader(r io.ReaderAt, size int64) (*Reader, error) {
	return newReader(r, size)
}

func newReader(r io.ReaderAt, size int64) (*Reader, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("epub: open zip: %w", err)
	}
	opfPath, err := findOPFPath(zr)
	if err != nil {
		return nil, err
	}
	pkg, err := readOPF(zr, opfPath)
	if err != nil {
		return nil, err
	}

	rd := &Reader{zr: zr, opfDir: path.Dir(opfPath)}
	rd.spine = buildSpine(pkg, rd.opfDir)
	return rd, nil
}

// Close releases the underlying archive file, if any.
func (rd *Reader) Close() error {
	if rd.closer != nil {
		return rd.closer.Close()
	}
	return nil
}

// Spine returns the reading order, in document order.
func (rd *Reader) Spine() []SpineItem { return rd.spine }

// SpineItemAt returns the spine entry at the given 0-based index.
func (rd *Reader) SpineItemAt(index int) (SpineItem, bool) {
	if index < 0 || index >= len(rd.spine) {
		return SpineItem{}, false
	}
	return rd.spine[index], true
}

// FindSpineItem locates a spine entry by href. It matches either the full zip
// path (e.g. "OEBPS/chapter1.xhtml") or the bare manifest href / basename
// (e.g. "chapter1.xhtml"), so callers can pass whichever form they hold.
func (rd *Reader) FindSpineItem(href string) (SpineItem, int, bool) {
	href = strings.TrimSpace(href)
	if href == "" {
		return SpineItem{}, -1, false
	}
	clean := path.Clean(href)
	for i, it := range rd.spine {
		if it.Href == clean || path.Base(it.Href) == clean {
			return it, i, true
		}
	}
	return SpineItem{}, -1, false
}

// ReadContent returns the raw bytes of the content document at the given full
// zip path (as carried by SpineItem.Href).
func (rd *Reader) ReadContent(href string) ([]byte, error) {
	return readZipFile(rd.zr, href)
}

// buildSpine resolves each spine itemref to its manifest item, producing the
// ordered list of content documents with zip-absolute hrefs. Itemrefs whose
// idref has no manifest entry are skipped.
func buildSpine(pkg *opfPackage, opfDir string) []SpineItem {
	byID := make(map[string]manifestItem, len(pkg.Manifest.Items))
	for _, it := range pkg.Manifest.Items {
		byID[it.ID] = it
	}

	var spine []SpineItem
	for _, ref := range pkg.Spine.Items {
		it, ok := byID[ref.IDRef]
		if !ok || it.Href == "" {
			continue
		}
		spine = append(spine, SpineItem{
			IDRef:     ref.IDRef,
			Href:      path.Join(opfDir, it.Href),
			MediaType: it.MediaType,
		})
	}
	return spine
}
