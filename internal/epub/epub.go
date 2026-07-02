// Package epub is a small, storage-decoupled EPUB metadata reader. It opens an
// EPUB (a ZIP container), locates the OPF package document via
// META-INF/container.xml, and extracts the Dublin Core metadata plus the cover
// image. It deliberately knows nothing about internal/store: callers map the
// returned Metadata onto whatever persistence model they like.
package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// Metadata is the result of parsing an EPUB. The Dublin Core fields are taken
// verbatim from the OPF; Cover* hold the resolved cover image bytes and its
// declared media type (empty if no cover could be found).
type Metadata struct {
	Title      string
	Author     string // first dc:creator
	Language   string
	Identifier string

	CoverData      []byte
	CoverMediaType string
}

// HasCover reports whether a cover image was extracted.
func (m *Metadata) HasCover() bool { return len(m.CoverData) > 0 }

// container.xml model -------------------------------------------------------

type container struct {
	XMLName   xml.Name `xml:"container"`
	Rootfiles []struct {
		FullPath  string `xml:"full-path,attr"`
		MediaType string `xml:"media-type,attr"`
	} `xml:"rootfiles>rootfile"`
}

// OPF package model. The xml tags match on local element name, so the dc:
// prefix and any namespace declarations are handled transparently.

type opfPackage struct {
	XMLName  xml.Name `xml:"package"`
	Metadata struct {
		Titles      []string `xml:"title"`
		Creators    []string `xml:"creator"`
		Languages   []string `xml:"language"`
		Identifiers []string `xml:"identifier"`
		Metas       []struct {
			Name    string `xml:"name,attr"`
			Content string `xml:"content,attr"`
			// EPUB3 refinement form: <meta property="...">value</meta>.
			Property string `xml:"property,attr"`
			Value    string `xml:",chardata"`
		} `xml:"meta"`
	} `xml:"metadata"`
	Manifest struct {
		Items []manifestItem `xml:"item"`
	} `xml:"manifest"`
	Spine struct {
		Items []struct {
			IDRef string `xml:"idref,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
}

type manifestItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

// ParseFile reads and parses the EPUB at the given filesystem path.
func ParseFile(p string) (*Metadata, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("epub: open %s: %w", p, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("epub: stat %s: %w", p, err)
	}
	return Parse(f, fi.Size())
}

// ParseBytes parses an EPUB held entirely in memory.
func ParseBytes(b []byte) (*Metadata, error) {
	return Parse(bytes.NewReader(b), int64(len(b)))
}

// Parse parses an EPUB from a random-access reader of the given size.
func Parse(r io.ReaderAt, size int64) (*Metadata, error) {
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

	md := &Metadata{
		Title:      first(pkg.Metadata.Titles),
		Author:     first(pkg.Metadata.Creators),
		Language:   first(pkg.Metadata.Languages),
		Identifier: first(pkg.Metadata.Identifiers),
	}

	if href, mt := resolveCover(pkg); href != "" {
		// Cover hrefs are relative to the OPF's directory.
		coverPath := path.Join(path.Dir(opfPath), href)
		data, err := readZipFile(zr, coverPath)
		if err == nil {
			md.CoverData = data
			md.CoverMediaType = mt
		}
		// A missing/unreadable cover is non-fatal: metadata is still useful.
	}

	return md, nil
}

// findOPFPath reads META-INF/container.xml and returns the first OPF rootfile.
func findOPFPath(zr *zip.Reader) (string, error) {
	data, err := readZipFile(zr, "META-INF/container.xml")
	if err != nil {
		return "", fmt.Errorf("epub: read container.xml: %w", err)
	}
	var c container
	if err := xml.Unmarshal(data, &c); err != nil {
		return "", fmt.Errorf("epub: parse container.xml: %w", err)
	}
	for _, rf := range c.Rootfiles {
		if rf.FullPath != "" {
			return rf.FullPath, nil
		}
	}
	return "", fmt.Errorf("epub: no rootfile in container.xml")
}

func readOPF(zr *zip.Reader, opfPath string) (*opfPackage, error) {
	data, err := readZipFile(zr, opfPath)
	if err != nil {
		return nil, fmt.Errorf("epub: read opf %s: %w", opfPath, err)
	}
	var pkg opfPackage
	if err := xml.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("epub: parse opf %s: %w", opfPath, err)
	}
	return &pkg, nil
}

// resolveCover picks the cover image href and media type. It prefers an EPUB3
// manifest item flagged properties="cover-image", then the legacy
// <meta name="cover"> -> manifest item id, and finally the first image item.
func resolveCover(pkg *opfPackage) (href, mediaType string) {
	// EPUB3 cover-image property.
	for _, it := range pkg.Manifest.Items {
		if hasProperty(it.Properties, "cover-image") {
			return it.Href, it.MediaType
		}
	}

	// Legacy <meta name="cover" content="item-id">.
	var coverID string
	for _, m := range pkg.Metadata.Metas {
		if strings.EqualFold(m.Name, "cover") && m.Content != "" {
			coverID = m.Content
			break
		}
	}
	if coverID != "" {
		for _, it := range pkg.Manifest.Items {
			if it.ID == coverID {
				return it.Href, it.MediaType
			}
		}
	}

	// Fallback: first image manifest item.
	for _, it := range pkg.Manifest.Items {
		if strings.HasPrefix(it.MediaType, "image/") {
			return it.Href, it.MediaType
		}
	}
	return "", ""
}

func hasProperty(properties, want string) bool {
	for _, p := range strings.Fields(properties) {
		if p == want {
			return true
		}
	}
	return false
}

// readZipFile returns the full contents of the named entry. ZIP paths use
// forward slashes; matching is exact after cleaning.
func readZipFile(zr *zip.Reader, name string) ([]byte, error) {
	clean := path.Clean(name)
	for _, f := range zr.File {
		if path.Clean(f.Name) == clean {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("epub: entry not found: %s", name)
}

func first(ss []string) string {
	for _, s := range ss {
		if t := strings.TrimSpace(s); t != "" {
			return t
		}
	}
	return ""
}
