// Package svgtests runs the resvg regression suite against this repo's SVG
// renderer and reports per-test pixel differences.
//
// It lives in the svgrender module, beside the renderer it measures: the SVG
// library itself must not depend on a rasterising backend. Only the package
// canvasrenderer knows the renderer.
//
// The bucket thresholds and the pixel comparison deliberately match the
// pre-existing Rust harness (AO/webrender-svg-test/testbed) so the numbers
// stay comparable with the oksvg baseline.
package svgtests

import "image"

// Bucket classifies one test's visual agreement with the reference.
type Bucket string

const (
	// Perfect: no pixel differs by more than 1 per channel.
	Perfect Bucket = "perfect"
	// Good: at most 0.1% of pixels differ by more than 16 per channel.
	Good Bucket = "good"
	// Close: at most 1% do.
	Close Bucket = "close"
	// Bad: more than 1% do, or the output size does not match.
	Bad Bucket = "bad"
)

// Diff is the pixel-level comparison of one render against its reference.
type Diff struct {
	RefW, RefH int
	OutW, OutH int
	TotalPx    int64
	// DiffT1 counts pixels differing by more than 1 in any channel,
	// DiffT16 by more than 16.
	DiffT1, DiffT16 int64
	MaxDelta        int
}

func (d Diff) PctT1() float64  { return pct(d.DiffT1, d.TotalPx) }
func (d Diff) PctT16() float64 { return pct(d.DiffT16, d.TotalPx) }

func pct(n, total int64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(n) / float64(total)
}

// Bucket classifies the diff. Size mismatch is always Bad, however few
// pixels happen to differ.
func (d Diff) Bucket() Bucket {
	if d.RefW != d.OutW || d.RefH != d.OutH {
		return Bad
	}
	switch {
	case d.DiffT1 == 0:
		return Perfect
	case d.PctT16() <= 0.1:
		return Good
	case d.PctT16() <= 1.0:
		return Close
	default:
		return Bad
	}
}

// Compare diffs two images over the union of their bounds.
func Compare(ref, out image.Image) Diff {
	rb, ob := ref.Bounds(), out.Bounds()
	d := Diff{RefW: rb.Dx(), RefH: rb.Dy(), OutW: ob.Dx(), OutH: ob.Dy()}

	w, h := max(rb.Dx(), ob.Dx()), max(rb.Dy(), ob.Dy())
	d.TotalPx = int64(w) * int64(h)

	for y := range h {
		for x := range w {
			a, aOK := rgba8(ref, rb, x, y)
			b, bOK := rgba8(out, ob, x, y)
			if !aOK || !bOK {
				// Present in only one image: a size mismatch.
				d.DiffT1++
				d.DiffT16++
				d.MaxDelta = 255
				continue
			}
			// Two invisible pixels are equal whatever their RGB claims:
			// below alpha 1/255 no colour reaches the eye, and
			// unpremultiplying an alpha-1 coverage sliver saturates the
			// channels — premultiplied (0,1,0,1) decodes as (0,255,0,1).
			if a[3] <= 1 && b[3] <= 1 {
				continue
			}
			delta := 0
			for i := range 4 {
				delta = max(delta, absDiff(a[i], b[i]))
			}
			d.MaxDelta = max(d.MaxDelta, delta)
			if delta > 1 {
				d.DiffT1++
			}
			if delta > 16 {
				d.DiffT16++
			}
		}
	}
	return d
}

// rgba8 returns the 8-bit RGBA at (x, y) relative to bounds b.
func rgba8(img image.Image, b image.Rectangle, x, y int) ([4]int, bool) {
	if x >= b.Dx() || y >= b.Dy() {
		return [4]int{}, false
	}
	r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
	return [4]int{int(r >> 8), int(g >> 8), int(bl >> 8), int(a >> 8)}, true
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}
