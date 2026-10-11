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
