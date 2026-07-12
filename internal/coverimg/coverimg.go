// Package coverimg normalizes book cover images at ingest so the library grid
// reads as one consistent shelf. Many covers Lyceum stores are embedded EPUB art
// (used when no Apple Books cover is available): some carry uniform white/black
// frames, odd aspect ratios, or oversized scans, which make the grid ragged.
//
// Normalize trims those frames, pads the image to the shelf's aspect ratio on a
// sampled background (never cropping real art), downscales to a sane maximum,
// and re-encodes JPEG. It is a cosmetic pass over whatever cover bytes the
// caller already chose — it is not a cover source and it makes no network calls.
// It is deliberately best-effort: undecodable input is returned unchanged so a
// weird payload never fails ingest.
//
// Pure Go (no CGO): the production binary builds with CGO_ENABLED=0, so this
// leans on image/* stdlib decoders plus the pure-Go github.com/disintegration/
// imaging for scaling and compositing.
package coverimg

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // register GIF decoder
	"image/jpeg"
	_ "image/png" // register PNG decoder
	"math"

	"github.com/disintegration/imaging"
)

// A border pixel qualifies as trimmable only when it is near-white or near-black
// — a publisher frame — so colored art edges are never peeled. whiteFloor and
// blackCeil are the per-channel cutoffs (all three channels must clear them).
const (
	whiteFloor = 235
	blackCeil  = 20
)

// Options tunes Normalize. The zero value is not usable directly; callers pass
// DefaultOptions() (or a partial Options that withDefaults fills in).
type Options struct {
	// TargetW, TargetH are the aspect ratio the image is padded to (only the
	// ratio matters, not the absolute numbers). The shelf card is 366×600.
	TargetW, TargetH int
	// MaxHeight caps the output height; taller images are downscaled. Smaller
	// images are never upscaled.
	MaxHeight int
	// BorderTolerance is the per-channel spread allowed across a border line for
	// it to count as uniform.
	BorderTolerance uint8
	// MaxTrimFraction caps how much of each side border-trimming may remove, so a
	// mostly-solid cover can never be gutted.
	MaxTrimFraction float64
	// JPEGQuality is the re-encode quality (1–100).
	JPEGQuality int
}

// DefaultOptions returns the tuned defaults used at ingest.
func DefaultOptions() Options {
	return Options{
		TargetW:         366,
		TargetH:         600,
		MaxHeight:       900,
		BorderTolerance: 12,
		MaxTrimFraction: 0.25,
		JPEGQuality:     88,
	}
}

// withDefaults fills any zero field from DefaultOptions so a partial Options is
// usable.
func (o Options) withDefaults() Options {
	d := DefaultOptions()
	if o.TargetW <= 0 {
		o.TargetW = d.TargetW
	}
	if o.TargetH <= 0 {
		o.TargetH = d.TargetH
	}
	if o.MaxHeight <= 0 {
		o.MaxHeight = d.MaxHeight
	}
	if o.BorderTolerance == 0 {
		o.BorderTolerance = d.BorderTolerance
	}
	if o.MaxTrimFraction <= 0 {
		o.MaxTrimFraction = d.MaxTrimFraction
	}
	if o.JPEGQuality <= 0 {
		o.JPEGQuality = d.JPEGQuality
	}
	return o
}

// Normalize returns a cleaned, shelf-consistent cover encoded as JPEG. It is
// best-effort: empty or undecodable input is returned unchanged with a nil error
// (the caller stores the original). A genuine encode failure returns the
// original bytes alongside the error so a caller that ignores the error is still
// safe.
func Normalize(data []byte, opts Options) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	opts = opts.withDefaults()

	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, nil // not a decodable image; store as-is
	}

	// Clone to an NRGBA at origin (0,0) for direct pixel access and predictable
	// bounds through the imaging pipeline.
	img := imaging.Clone(decoded)
	img = trim(img, opts)
	img = fitAspect(img, opts)
	img = downscale(img, opts)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: opts.JPEGQuality}); err != nil {
		return data, fmt.Errorf("coverimg: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// trim crops away uniform near-white/near-black borders, bounded by
// MaxTrimFraction per side.
func trim(img *image.NRGBA, opts Options) *image.NRGBA {
	rect := trimBounds(img, opts)
	if rect == img.Bounds() {
		return img
	}
	return imaging.Crop(img, rect)
}

// trimBounds computes the crop rectangle after peeling uniform neutral borders.
// Left/right columns are tested only over the already-trimmed vertical span so a
// frame's corners don't block detection.
func trimBounds(src *image.NRGBA, opts Options) image.Rectangle {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	maxX := int(float64(w) * opts.MaxTrimFraction)
	maxY := int(float64(h) * opts.MaxTrimFraction)
	tol := opts.BorderTolerance

	top := 0
	for top < maxY && rowUniformNeutral(src, top, 0, w, tol) {
		top++
	}
	bottom := h
	for bottom > h-maxY && rowUniformNeutral(src, bottom-1, 0, w, tol) {
		bottom--
	}
	left := 0
	for left < maxX && colUniformNeutral(src, left, top, bottom, tol) {
		left++
	}
	right := w
	for right > w-maxX && colUniformNeutral(src, right-1, top, bottom, tol) {
		right--
	}
	if right-left < 1 || bottom-top < 1 {
		return b // degenerate; trim nothing
	}
	return image.Rect(left, top, right, bottom)
}

// fitAspect pads the image to Options' target ratio on a background sampled from
// its border, so nothing is cropped. An image already at (near) the target ratio
// is returned unchanged.
func fitAspect(img *image.NRGBA, opts Options) *image.NRGBA {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w == 0 || h == 0 {
		return img
	}
	target := float64(opts.TargetW) / float64(opts.TargetH)
	cur := float64(w) / float64(h)
	const eps = 0.01
	if math.Abs(cur-target) <= eps {
		return img
	}

	var newW, newH int
	if cur > target {
		newW, newH = w, int(math.Round(float64(w)/target)) // too wide: pad height
	} else {
		newW, newH = int(math.Round(float64(h)*target)), h // too tall: pad width
	}
	canvas := imaging.New(newW, newH, borderColor(img))
	return imaging.PasteCenter(canvas, img)
}

// downscale shrinks the image to MaxHeight (preserving aspect) when it is taller;
// smaller images are left as-is (never upscaled).
func downscale(img *image.NRGBA, opts Options) *image.NRGBA {
	if img.Bounds().Dy() <= opts.MaxHeight {
		return img
	}
	return imaging.Resize(img, 0, opts.MaxHeight, imaging.Lanczos)
}

// borderColor averages the outermost ring of pixels — the fill used when padding
// to the target aspect so the pad blends with the cover's edge.
func borderColor(src *image.NRGBA) color.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	var rs, gs, bs, n int64
	acc := func(x, y int) {
		r, g, bl := pixAt(src, x, y)
		rs, gs, bs, n = rs+int64(r), gs+int64(g), bs+int64(bl), n+1
	}
	for x := 0; x < w; x++ {
		acc(x, 0)
		acc(x, h-1)
	}
	for y := 0; y < h; y++ {
		acc(0, y)
		acc(w-1, y)
	}
	if n == 0 {
		return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	return color.NRGBA{R: uint8(rs / n), G: uint8(gs / n), B: uint8(bs / n), A: 255}
}

// rowUniformNeutral reports whether row y over [x0,x1) is a near-white or
// near-black line uniform within tol.
func rowUniformNeutral(src *image.NRGBA, y, x0, x1 int, tol uint8) bool {
	r0, g0, b0 := pixAt(src, x0, y)
	if !neutral(r0, g0, b0) {
		return false
	}
	for x := x0 + 1; x < x1; x++ {
		r, g, b := pixAt(src, x, y)
		if diff(r, r0) > tol || diff(g, g0) > tol || diff(b, b0) > tol {
			return false
		}
	}
	return true
}

// colUniformNeutral is rowUniformNeutral for a vertical line at x over [y0,y1).
func colUniformNeutral(src *image.NRGBA, x, y0, y1 int, tol uint8) bool {
	r0, g0, b0 := pixAt(src, x, y0)
	if !neutral(r0, g0, b0) {
		return false
	}
	for y := y0 + 1; y < y1; y++ {
		r, g, b := pixAt(src, x, y)
		if diff(r, r0) > tol || diff(g, g0) > tol || diff(b, b0) > tol {
			return false
		}
	}
	return true
}

// neutral reports whether a color is near-white or near-black on every channel —
// the only borders trim will peel.
func neutral(r, g, b uint8) bool {
	white := r >= whiteFloor && g >= whiteFloor && b >= whiteFloor
	black := r <= blackCeil && g <= blackCeil && b <= blackCeil
	return white || black
}

// pixAt returns the RGB of the pixel at (x,y); alpha is ignored (covers are
// opaque).
func pixAt(src *image.NRGBA, x, y int) (r, g, b uint8) {
	i := src.PixOffset(x, y)
	return src.Pix[i], src.Pix[i+1], src.Pix[i+2]
}

// diff is the absolute difference of two channel values.
func diff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}
