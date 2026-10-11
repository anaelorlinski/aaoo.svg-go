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
	// Good: at most 0.1% of pixels differ in shape, see [Diff.DiffShape]:
	// by more than 16 per channel, and not as an edge drawn another way.
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
	// DiffShape counts the pixels differing by more than 16 that the
	// drawing of an edge does not explain, see [Compare].
	DiffShape int64
}

func (d Diff) PctShape() float64 { return pct(d.DiffShape, d.TotalPx) }

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
	case d.PctShape() <= 0.1:
		return Good
	case d.PctShape() <= 1.0:
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

	sameSize := rb.Dx() == ob.Dx() && rb.Dy() == ob.Dy()
	var refPx, outPx [][4]int
	if sameSize {
		refPx, outPx = pixels(ref), pixels(out)
	}

	for y := range h {
		for x := range w {
			a, aOK := rgba8(ref, rb, x, y)
			b, bOK := rgba8(out, ob, x, y)
			if !aOK || !bOK {
				// Present in only one image: a size mismatch.
				d.DiffT1++
				d.DiffT16++
				d.DiffShape++
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
				if !sameSize || !(within(b, refPx, w, h, x, y) && within(a, outPx, w, h, x, y)) {
					d.DiffShape++
				}
			}
		}
	}
	return d
}

// edgeTolerance is how far a pixel may be from the values around it in the
// other image and still be the same edge drawn another way.
const edgeTolerance = 16

// pixels returns the pixels of an image, row by row.
func pixels(img image.Image) [][4]int {
	b := img.Bounds()
	out := make([][4]int, 0, b.Dx()*b.Dy())
	for y := range b.Dy() {
		for x := range b.Dx() {
			px, _ := rgba8(img, b, x, y)
			out = append(out, px)
		}
	}
	return out
}

// within reports whether a pixel is between the lowest and the highest
// values of the pixels around (x, y) in the other image, channel by channel.
//
// Two renderers do not cover the pixels of an edge the same way: one rounds
// the coverage, the other does not, and the edge may sit a fraction of a
// pixel apart. Each pixel of such an edge is then between its neighbours in
// the other image. A shape that is missing, misplaced by more than a pixel
// or of another colour is not.
func within(px [4]int, other [][4]int, w, h, x, y int) bool {
	lo := [4]int{255, 255, 255, 255}
	hi := [4]int{}
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			nx, ny := x+dx, y+dy
			if nx < 0 || ny < 0 || nx >= w || ny >= h {
				continue
			}
			o := other[ny*w+nx]
			for i := range 4 {
				lo[i], hi[i] = min(lo[i], o[i]), max(hi[i], o[i])
			}
		}
	}
	for i := range 4 {
		if px[i] < lo[i]-edgeTolerance || px[i] > hi[i]+edgeTolerance {
			return false
		}
	}
	return true
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
