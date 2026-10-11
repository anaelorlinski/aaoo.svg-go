package svgtests

import (
	"image"
	"image/color"
	"testing"
)

func solid(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func TestBucketThresholds(t *testing.T) {
	opaque := color.NRGBA{10, 20, 30, 255}
	ref := solid(10, 10, opaque)

	// identical -> perfect
	if got := Compare(ref, solid(10, 10, opaque)).Bucket(); got != Perfect {
		t.Fatalf("identical images: got %s, want perfect", got)
	}

	// a delta of exactly 1 is below both thresholds -> still perfect
	off1 := solid(10, 10, color.NRGBA{11, 20, 30, 255})
	if got := Compare(ref, off1).Bucket(); got != Perfect {
		t.Fatalf("delta 1: got %s, want perfect", got)
	}

	// every pixel far off -> bad
	if got := Compare(ref, solid(10, 10, color.NRGBA{200, 20, 30, 255})).Bucket(); got != Bad {
		t.Fatalf("all pixels differ: got %s, want bad", got)
	}

	// size mismatch is bad regardless of content
	if got := Compare(ref, solid(10, 11, opaque)).Bucket(); got != Bad {
		t.Fatalf("size mismatch: got %s, want bad", got)
	}
}

// Two fully transparent pixels compare equal whatever their RGB says.
func TestInvisiblePixelsAreEqual(t *testing.T) {
	a := solid(4, 4, color.NRGBA{0, 255, 0, 1})
	b := solid(4, 4, color.NRGBA{255, 0, 255, 0})
	d := Compare(a, b)
	if d.DiffT1 != 0 || d.Bucket() != Perfect {
		t.Fatalf("invisible pixels should compare equal, got diffT1=%d bucket=%s", d.DiffT1, d.Bucket())
	}
}

// One differing pixel in 10000 is 0.01% -> good, not perfect.
func TestSingleFarPixelIsGood(t *testing.T) {
	ref := solid(100, 100, color.NRGBA{0, 0, 0, 255})
	out := solid(100, 100, color.NRGBA{0, 0, 0, 255})
	out.SetNRGBA(50, 50, color.NRGBA{255, 255, 255, 255})

	d := Compare(ref, out)
	if d.DiffT1 != 1 || d.DiffT16 != 1 {
		t.Fatalf("expected exactly one differing pixel, got t1=%d t16=%d", d.DiffT1, d.DiffT16)
	}
	if got := d.Bucket(); got != Good {
		t.Fatalf("1 pixel of 10000 (0.01%%): got %s, want good", got)
	}
	if d.MaxDelta != 255 {
		t.Fatalf("max delta: got %d, want 255", d.MaxDelta)
	}
}

// column returns a 20x20 image, black from x = from on, with the given alpha
// in the column before it: an edge and its coverage.
func column(from int, coverage uint8) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 20, 20))
	for y := range 20 {
		for x := range 20 {
			switch {
			case x >= from:
				img.SetNRGBA(x, y, color.NRGBA{A: 255})
			case x == from-1:
				img.SetNRGBA(x, y, color.NRGBA{A: coverage})
			}
		}
	}
	return img
}

// An edge covered another way, or a pixel apart, is not a difference of
// shape; a shape two pixels apart is.
func TestCompareShape(t *testing.T) {
	ref := column(10, 192)

	// the same edge, its coverage rounded another way
	d := Compare(ref, column(10, 128))
	if d.DiffT16 == 0 || d.DiffShape != 0 {
		t.Errorf("another coverage: t16=%d shape=%d, want differences but none of shape", d.DiffT16, d.DiffShape)
	}
	if d.Bucket() != Good {
		t.Errorf("another coverage: bucket %s, want good", d.Bucket())
	}

	// the edge one pixel apart
	if d := Compare(ref, column(11, 192)); d.DiffShape != 0 {
		t.Errorf("one pixel apart: shape=%d, want 0", d.DiffShape)
	}

	// two pixels apart: a column of pixels is black in one and empty in the other
	if d := Compare(ref, column(12, 192)); d.DiffShape == 0 {
		t.Error("two pixels apart: no difference of shape")
	}

	// a shape that is missing
	if d := Compare(ref, image.NewNRGBA(image.Rect(0, 0, 20, 20))); d.DiffShape < 150 || d.Bucket() != Bad {
		t.Errorf("missing shape: shape=%d bucket=%s", d.DiffShape, d.Bucket())
	}

	// another colour
	red := image.NewNRGBA(image.Rect(0, 0, 20, 20))
	for y := range 20 {
		for x := 10; x < 20; x++ {
			red.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	if d := Compare(column(10, 0), red); d.DiffShape != 200 {
		t.Errorf("another colour: shape=%d, want 200", d.DiffShape)
	}
}
