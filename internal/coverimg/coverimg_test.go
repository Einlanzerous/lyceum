package coverimg

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// solid fills a new image with c.
func solid(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// framed returns a `w`×`h` image filled with frame, holding a centered inner
// rectangle of fill inset by `border` px on every side.
func framed(w, h, border int, frame, fill color.NRGBA) *image.NRGBA {
	img := solid(w, h, frame)
	for y := border; y < h-border; y++ {
		for x := border; x < w-border; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// decode reads normalized JPEG output back into an image for assertions.
func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	return img
}

func aspect(img image.Image) float64 {
	b := img.Bounds()
	return float64(b.Dx()) / float64(b.Dy())
}

// nearGray reports whether c is within tol of a mid-tone (used to check the
// inner art survived, since JPEG shifts exact values).
func near(a, b uint8, tol uint8) bool {
	if a > b {
		return a-b <= tol
	}
	return b-a <= tol
}

var (
	white = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	black = color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	art   = color.NRGBA{R: 120, G: 90, B: 60, A: 255} // an opaque "cover" tone
	red   = color.NRGBA{R: 200, G: 40, B: 40, A: 255} // a colored (non-neutral) edge
)

func TestNormalizeTrimsWhiteFrame(t *testing.T) {
	// A 200×200 art block inside a 60px white frame (aspect starts square).
	src := framed(320, 320, 60, white, art)
	out, err := Normalize(encodePNG(t, src), DefaultOptions())
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	got := decode(t, out)

	// After trimming the frame the art is ~200×200; padding to 366:600 must land
	// near the target ratio.
	if a := aspect(got); a < 0.55 || a > 0.67 {
		t.Fatalf("aspect = %.3f, want ~0.61 (366:600)", a)
	}
	// The center pixel must be the art tone, not white frame — i.e. the frame was
	// removed and the art preserved (JPEG tolerance).
	b := got.Bounds()
	r, g, bl, _ := got.At(b.Dx()/2, b.Dy()/2).RGBA()
	cr, cg, cb := uint8(r>>8), uint8(g>>8), uint8(bl>>8)
	if !near(cr, art.R, 24) || !near(cg, art.G, 24) || !near(cb, art.B, 24) {
		t.Fatalf("center = (%d,%d,%d), want art tone (%d,%d,%d)", cr, cg, cb, art.R, art.G, art.B)
	}
}

func TestNormalizeTrimsBlackFrame(t *testing.T) {
	src := framed(300, 300, 40, black, art)
	out, err := Normalize(encodePNG(t, src), DefaultOptions())
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	got := decode(t, out)
	b := got.Bounds()
	r, g, bl, _ := got.At(b.Dx()/2, b.Dy()/2).RGBA()
	cr, cg, cb := uint8(r>>8), uint8(g>>8), uint8(bl>>8)
	if !near(cr, art.R, 24) || !near(cg, art.G, 24) || !near(cb, art.B, 24) {
		t.Fatalf("center = (%d,%d,%d), want art tone after black-frame trim", cr, cg, cb)
	}
}

func TestNormalizePadsSquareToAspect(t *testing.T) {
	// A full-bleed square with no neutral border: nothing trims, so it must be
	// padded (not cropped) to the target aspect.
	src := solid(400, 400, art)
	out, err := Normalize(encodePNG(t, src), DefaultOptions())
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	got := decode(t, out)
	if a := aspect(got); a < 0.58 || a > 0.64 {
		t.Fatalf("aspect = %.3f, want ~0.61", a)
	}
	// Padding taller keeps full width, so width must be preserved (400), height
	// grown — never cropped below the original 400 art width.
	if got.Bounds().Dx() < 400 {
		t.Fatalf("width shrank to %d; padding must not crop the art", got.Bounds().Dx())
	}
}

func TestNormalizeDownscalesTall(t *testing.T) {
	// Already at target ratio but far taller than MaxHeight → downscaled.
	src := solid(1220, 2000, art)
	out, err := Normalize(encodePNG(t, src), DefaultOptions())
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	got := decode(t, out)
	if h := got.Bounds().Dy(); h != 900 {
		t.Fatalf("height = %d, want 900 (MaxHeight)", h)
	}
}

func TestNormalizeNoUpscaleSmall(t *testing.T) {
	// A small cover already at the target ratio must not be enlarged.
	src := solid(122, 200, art)
	out, err := Normalize(encodePNG(t, src), DefaultOptions())
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	got := decode(t, out)
	if got.Bounds().Dy() > 200 {
		t.Fatalf("height = %d, small cover was upscaled", got.Bounds().Dy())
	}
}

func TestNormalizeKeepsColoredEdge(t *testing.T) {
	// A red (non-neutral) frame is NOT a publisher white/black border, so trim
	// must leave it — the colored edge pixels survive.
	src := framed(300, 300, 30, red, art)
	out, err := Normalize(encodePNG(t, src), DefaultOptions())
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	got := decode(t, out)
	// Sample a pixel near the left edge, vertically centered: still red-ish.
	b := got.Bounds()
	r, g, bl, _ := got.At(b.Min.X+3, b.Dy()/2).RGBA()
	cr, cg, cb := uint8(r>>8), uint8(g>>8), uint8(bl>>8)
	if cr < 150 || cg > 100 || cb > 100 {
		t.Fatalf("left edge = (%d,%d,%d), want the red frame preserved", cr, cg, cb)
	}
}

func TestNormalizeCleanCoverAspectPreserved(t *testing.T) {
	// A clean, correctly-sized 366:600 cover: no trim, no pad, no downscale — the
	// aspect must be preserved (we don't degrade good Apple covers).
	src := solid(366, 600, art)
	out, err := Normalize(encodePNG(t, src), DefaultOptions())
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	got := decode(t, out)
	if a := aspect(got); a < 0.60 || a > 0.62 {
		t.Fatalf("aspect = %.3f, want 366:600 preserved", a)
	}
	if got.Bounds().Dy() > 600 {
		t.Fatalf("clean cover height grew to %d", got.Bounds().Dy())
	}
}

func TestNormalizeNonImagePassthrough(t *testing.T) {
	junk := []byte("this is not an image")
	out, err := Normalize(junk, DefaultOptions())
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if !bytes.Equal(out, junk) {
		t.Fatalf("non-image input was altered; want passthrough")
	}
}

func TestNormalizeEmptyPassthrough(t *testing.T) {
	out, err := Normalize(nil, DefaultOptions())
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if out != nil {
		t.Fatalf("nil input returned %v, want nil passthrough", out)
	}
}

func TestInspect(t *testing.T) {
	t.Run("clean cover", func(t *testing.T) {
		r := Inspect(encodePNG(t, solid(366, 600, art)))
		if !r.Decodable || r.Width != 366 || r.Height != 600 {
			t.Fatalf("report = %+v, want decodable 366x600", r)
		}
		if r.Aspect < 0.60 || r.Aspect > 0.62 {
			t.Fatalf("aspect = %.3f, want ~0.61", r.Aspect)
		}
		if r.BorderFraction > 0.02 {
			t.Fatalf("border fraction = %.3f, want ~0 for a full-bleed cover", r.BorderFraction)
		}
	})

	t.Run("heavily framed cover", func(t *testing.T) {
		// 60px white frame on a 320x320 image: ~2*60/320 = 37.5% each axis, so a
		// large border fraction.
		r := Inspect(encodePNG(t, framed(320, 320, 60, white, art)))
		if !r.Decodable {
			t.Fatalf("framed cover did not decode")
		}
		if r.BorderFraction < 0.3 {
			t.Fatalf("border fraction = %.3f, want a large frame detected", r.BorderFraction)
		}
	})

	t.Run("non-image", func(t *testing.T) {
		if r := Inspect([]byte("nope")); r.Decodable {
			t.Fatalf("non-image reported decodable: %+v", r)
		}
	})
}

func TestNormalizeOutputIsJPEG(t *testing.T) {
	src := solid(366, 600, art)
	out, err := Normalize(encodePNG(t, src), DefaultOptions())
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("output is not decodable JPEG: %v", err)
	}
}
