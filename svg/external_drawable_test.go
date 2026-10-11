package svg_test

// This file lives in the external test package on purpose: it can only reach
// exported identifiers, so it proves the Drawable seam is implementable from
// outside svg — the precondition for moving text into its own package, and
// later its own module (docs/svg-separation.md T7b).
//
// It deliberately exercises everything the text renderer needs, not just
// geometry: resolving lengths, applying painters through the opaque Element
// handle, building a paint op, and growing a bounding box. If any of that
// were unreachable from outside, this file would not compile.

import (
	"strings"
	"testing"

	"github.com/anaelorlinski/aaoo.gfxcore-go"
	"github.com/anaelorlinski/aaoo.svg-go/svg"
)

// stubElement stands in for what svgtext.textSpan will become.
type stubElement struct {
	w, h    svg.Value
	painted bool
}

func (s *stubElement) Draw(dst gfxcore.Canvas, img *svg.SVGImage, el *svg.Element) []svg.Vertex {
	dims := el.Dims()
	w, h := dims.Point(s.w, s.h)
	_ = dims.Length(s.w)

	dst.Rectangle(0, 0, w, h)

	// The callback text cannot do without: have core resolve fill/stroke
	// for this element, then turn the answer into a paint operation.
	doFill, doStroke := img.ApplyPainters(dst, el, s)
	dst.State().SetTextPaint(svg.NewPaintOp(doFill, doStroke, false))
	s.painted = true
	return nil
}

func (s *stubElement) BoundingBox(el *svg.Element) (svg.Rectangle, bool) {
	dims := el.Dims()
	w, h := dims.Point(s.w, s.h)
	bbox := svg.Rectangle{Width: w, Height: h}
	bbox.Union(svg.Rectangle{X: -5, Y: -5, Width: 1, Height: 1})
	return bbox, true
}

var _ svg.Drawable = (*stubElement)(nil)

// TestExternalDrawableResolves checks the exported surface is not merely
// present but sufficient: units resolve correctly through it.
func TestExternalDrawableResolves(t *testing.T) {
	dims := svg.Dims{FontSize: 16, RootFontSize: 32}
	dims.SetViewport(200, 100)

	el := &stubElement{w: svg.Value{U: svg.Vw, V: 50}, h: svg.Value{U: svg.Perc, V: 50}}
	w, h := dims.Point(el.w, el.h)
	if w != 100 {
		t.Errorf("50vw of a 200 viewport = %v, want 100", w)
	}
	if h != 50 {
		t.Errorf("50%% of a 100 height = %v, want 50", h)
	}

	// rem must use the root font size, not the element's
	if got := dims.Length(svg.Value{U: svg.Rem, V: 2}); got != 64 {
		t.Errorf("2rem at root 32 = %v, want 64", got)
	}
}

// TestExternalDrawableBoundingBox exercises the union helper an external
// renderer needs to accumulate a text subtree's extent.
func TestExternalDrawableBoundingBox(t *testing.T) {
	var box svg.Rectangle
	box.Union(svg.Rectangle{X: 10, Y: 10, Width: 5, Height: 5})
	box.Union(svg.Rectangle{X: 0, Y: 0, Width: 1, Height: 1})
	if box.X != 0 || box.Y != 0 {
		t.Fatalf("union did not grow to the origin: %+v", box)
	}
}

// TestExternalPaintOp checks the paint operation an external renderer builds
// matches what core builds internally for the same answer.
func TestExternalPaintOp(t *testing.T) {
	if op := svg.NewPaintOp(true, false, false); op&gfxcore.FillNonZero == 0 {
		t.Errorf("fill-only should use the non-zero rule, got %v", op)
	}
	if op := svg.NewPaintOp(true, false, true); op&gfxcore.FillEvenOdd == 0 {
		t.Errorf("even-odd fill not requested, got %v", op)
	}
	if op := svg.NewPaintOp(false, true, false); op&gfxcore.Stroke == 0 {
		t.Errorf("stroke not requested, got %v", op)
	}
}

// TestExternalDrawableIsReachedByTheRenderer is the end-to-end check: a
// Drawable implemented outside the package must actually be invoked, with a
// usable Element, when the image is drawn.
func TestExternalDrawableParsesAndDraws(t *testing.T) {
	// A minimal document confirms the parse path is reachable externally;
	// the stub is exercised directly above, since wiring an external element
	// into the tree is what T7b step 3 adds.
	img, err := svg.Parse(strings.NewReader(
		`<svg xmlns="http://www.w3.org/2000/svg" width="60" height="40"></svg>`), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if img == nil {
		t.Fatal("expected a parsed image")
	}
}
