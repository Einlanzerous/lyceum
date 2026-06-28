// Generates a small but multi-page EPUB3 for the reader smoke. The single
// chapter is long enough that epub.js's paginated flow splits it across many
// pages, so Next/Prev navigation is genuinely exercised (the repo's testdata
// EPUBs are single-page and can't).
//
//   node e2e/make-fixture.mjs <out.epub> [unique-tag]
//
// The optional tag is woven into the identifier and content so each tagged
// build produces distinct bytes — the backend content-addresses uploads, so a
// fresh tag avoids a 409 duplicate and yields a book with no stored position.
import { writeFileSync } from 'node:fs'
import JSZip from 'jszip'

const tag = process.argv[3] ?? 'fixed'

const paragraphs = Array.from({ length: 120 }, (_, i) =>
  `<p>Section ${i + 1}. ` +
  'Sing, O goddess, the anger of Achilles son of Peleus, that brought ' +
  'countless ills upon the Achaeans. Many a brave soul did it send hurrying ' +
  'down to Hades, and many a hero did it yield a prey to dogs and vultures. ' +
  '</p>',
).join('\n')

const chapter = `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>Chapter One</title></head>
<body><h1>Chapter One</h1><p>Build ${tag}.</p>${paragraphs}</body>
</html>`

const nav = `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>Contents</title></head>
<body><nav epub:type="toc" id="toc"><ol><li><a href="chapter1.xhtml">Chapter One</a></li></ol></nav></body>
</html>`

const opf = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="bookid">urn:uuid:lyceum-smoke-${tag}</dc:identifier>
    <dc:title>Lyceum Smoke Sample</dc:title>
    <dc:creator>Reader Test</dc:creator>
    <dc:language>en</dc:language>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="ch1"/>
  </spine>
</package>`

const container = `<?xml version="1.0" encoding="utf-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`

const zip = new JSZip()
// mimetype must be stored uncompressed.
zip.file('mimetype', 'application/epub+zip', { compression: 'STORE' })
zip.file('META-INF/container.xml', container)
zip.file('OEBPS/content.opf', opf)
zip.file('OEBPS/nav.xhtml', nav)
zip.file('OEBPS/chapter1.xhtml', chapter)

const buffer = await zip.generateAsync({ type: 'nodebuffer' })
const out = process.argv[2]
if (!out) throw new Error('usage: node make-fixture.mjs <out.epub>')
writeFileSync(out, buffer)
console.log(`wrote ${out} (${buffer.length} bytes)`)
