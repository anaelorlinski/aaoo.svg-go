package oksvg_test

import (
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anaelorlinski/aaoo.svg-go/svgrender/oksvg"
	"github.com/anaelorlinski/aaoo.svg-go/svgrender/rasterx"
)

var (
	red   = color.RGBA{255, 0, 0, 255}
	white = color.RGBA{255, 255, 255, 255}
)

const icon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 12"><rect width="24" height="12" fill="red"/></svg>`

func at(img image.Image, x, y int) color.RGBA {
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
}

// The sequence of calls of a program using oksvg, unchanged.
func TestDropIn(t *testing.T) {
	const w, h = 48, 24
	icon, err := oksvg.ReadIconStream(strings.NewReader(icon), oksvg.WarnErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	if icon.ViewBox.W != 24 || icon.ViewBox.H != 12 {
		t.Fatalf("viewBox %g x %g, want 24 x 12", icon.ViewBox.W, icon.ViewBox.H)
	}
	icon.SetTarget(0, 0, float64(w), float64(h))

	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	icon.Draw(rasterx.NewDasher(w, h, rasterx.NewScannerGV(w, h, rgba, rgba.Bounds())), 1)

	for _, p := range []image.Point{{1, 1}, {46, 1}, {1, 22}, {46, 22}, {24, 12}} {
		if got := at(rgba, p.X, p.Y); got != red {
			t.Errorf("pixel %v: %v, want red", p, got)
		}
	}
}

// Without SetTarget the icon is drawn at the size of its viewBox; a document
// without a viewBox has the size it says.
func TestDefaultTarget(t *testing.T) {
	sized := `<svg xmlns="http://www.w3.org/2000/svg" width="6" height="4"><rect width="6" height="4" fill="red"/></svg>`
	icon, err := oksvg.ReadIconStream(strings.NewReader(sized))
	if err != nil {
		t.Fatal(err)
	}
	if icon.ViewBox.W != 6 || icon.ViewBox.H != 4 {
		t.Fatalf("size %g x %g, want 6 x 4", icon.ViewBox.W, icon.ViewBox.H)
	}
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	draw.Draw(img, img.Bounds(), image.NewUniform(white), image.Point{}, draw.Src)
	icon.Draw(rasterx.NewDasher(10, 10, rasterx.NewScannerGV(10, 10, img, img.Bounds())), 1)
	if got := at(img, 3, 2); got != red {
		t.Errorf("inside: %v, want red", got)
	}
	if got := at(img, 8, 2); got != white {
		t.Errorf("right of the icon: %v, want white", got)
	}
	if got := at(img, 3, 7); got != white {
		t.Errorf("below the icon: %v, want white", got)
	}
}

// Only the part of the image given to the scanner is drawn on.
func TestClip(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	draw.Draw(img, img.Bounds(), image.NewUniform(white), image.Point{}, draw.Src)
	icon, err := oksvg.ReadIconStream(strings.NewReader(icon))
	if err != nil {
		t.Fatal(err)
	}
	icon.SetTarget(0, 0, 20, 20)
	left := image.Rect(0, 0, 10, 20)
	icon.Draw(rasterx.NewDasher(20, 20, rasterx.NewScannerGV(20, 20, img, left)), 1)
	if got := at(img, 5, 10); got != red {
		t.Errorf("inside the part: %v, want red", got)
	}
	if got := at(img, 15, 10); got != white {
		t.Errorf("outside the part: %v, want white", got)
	}
}

func TestCurrentColor(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 4 4"><rect width="4" height="4" fill="currentColor"/></svg>`
	icon, err := oksvg.ReadReplacingCurrentColor(strings.NewReader(src), "#ff0000")
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	icon.Draw(rasterx.NewDasher(4, 4, rasterx.NewScannerGV(4, 4, img, img.Bounds())), 1)
	if got := at(img, 2, 2); got != red {
		t.Errorf("%v, want red", got)
	}
}

func TestReadIcon(t *testing.T) {
	file := filepath.Join(t.TempDir(), "icon.svg")
	if err := os.WriteFile(file, []byte(icon), 0o600); err != nil {
		t.Fatal(err)
	}
	read, err := oksvg.ReadIcon(file, oksvg.IgnoreErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	if read.ViewBox.W != 24 {
		t.Errorf("viewBox width %g, want 24", read.ViewBox.W)
	}
	if _, err := oksvg.ReadIcon(filepath.Join(t.TempDir(), "missing.svg")); err == nil {
		t.Error("a missing file was read")
	}
}

func TestStrictErrorMode(t *testing.T) {
	if _, err := oksvg.ReadIconStream(strings.NewReader(icon), oksvg.StrictErrorMode); err != nil {
		t.Errorf("a clean document was refused: %v", err)
	}
	// a path that cannot be read is a warning, and an error when strict
	broken := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 4 4"><path d="M 0 0 L 1"/></svg>`
	if _, err := oksvg.ReadIconStream(strings.NewReader(broken), oksvg.StrictErrorMode); err == nil {
		t.Error("a document with an invalid path was accepted")
	}
	if _, err := oksvg.ReadIconStream(strings.NewReader(broken)); err != nil {
		t.Errorf("the default mode refused the document: %v", err)
	}
}
