package svgrender

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parse(t *testing.T, src string, opts Options) *Image {
	t.Helper()
	im, err := Parse(strings.NewReader(src), opts)
	if err != nil {
		t.Fatal(err)
	}
	return im
}

func at(img image.Image, x, y int) color.RGBA {
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
}

func expect(t *testing.T, img image.Image, x, y int, want color.RGBA) {
	t.Helper()
	if got := at(img, x, y); got != want {
		t.Errorf("pixel (%d, %d): %v, want %v", x, y, got, want)
	}
}

var (
	red   = color.RGBA{255, 0, 0, 255}
	blue  = color.RGBA{0, 0, 255, 255}
	white = color.RGBA{255, 255, 255, 255}
	none  = color.RGBA{}
)

const redSquare = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10" fill="red"/></svg>`

func TestRender(t *testing.T) {
	// a document twice as wide as high
	src := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 10">
		<rect width="10" height="10" fill="red"/><rect x="10" width="10" height="10" fill="blue"/></svg>`
	img, err := Render(strings.NewReader(src), 40, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 40 || b.Dy() != 20 {
		t.Fatalf("size %dx%d, want 40x20", b.Dx(), b.Dy())
	}
	expect(t, img, 5, 10, red)
	expect(t, img, 35, 10, blue)

	if _, err := Render(strings.NewReader(src), 0, Options{}); err == nil {
		t.Error("a width of 0 was accepted")
	}
	if _, err := Render(strings.NewReader("not an svg"), 10, Options{}); err == nil {
		t.Error("a document that is not SVG was accepted")
	}
}

func TestSize(t *testing.T) {
	im := parse(t, `<svg xmlns="http://www.w3.org/2000/svg" width="30" height="10" viewBox="1 2 3 4"/>`, Options{})
	if w, h, ratio := im.IntrinsicSize(); w != 30 || h != 10 || ratio != 3 {
		t.Errorf("size %g x %g, ratio %g: want 30 x 10, 3", w, h, ratio)
	}
	if x, y, w, h, ok := im.ViewBox(); !ok || x != 1 || y != 2 || w != 3 || h != 4 {
		t.Errorf("viewBox %g %g %g %g (%v): want 1 2 3 4", x, y, w, h, ok)
	}
	if got := im.HeightFor(90); got != 30 {
		t.Errorf("height for a width of 90: %d, want 30", got)
	}

	// no size: the ratio of the viewBox, and no viewBox: a square
	im = parse(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 4 1"/>`, Options{})
	if w, h, ratio := im.IntrinsicSize(); w != 0 || h != 0 || ratio != 4 {
		t.Errorf("size %g x %g, ratio %g: want unknown, 4", w, h, ratio)
	}
	im = parse(t, `<svg xmlns="http://www.w3.org/2000/svg"/>`, Options{})
	if _, _, _, _, ok := im.ViewBox(); ok {
		t.Error("a viewBox was found")
	}
	if got := im.HeightFor(50); got != 50 {
		t.Errorf("height with no ratio: %d, want 50", got)
	}
}

// A document is drawn in its rectangle, over what the image holds.
func TestDrawTarget(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	draw.Draw(img, img.Bounds(), image.NewUniform(white), image.Point{}, draw.Src)
	im := parse(t, redSquare, Options{})
	if err := im.Draw(img, 20, 10, 10, 10, 1); err != nil {
		t.Fatal(err)
	}
	expect(t, img, 25, 15, red)
	expect(t, img, 5, 5, white)
	expect(t, img, 25, 25, white)
	expect(t, img, 35, 15, white)

	// drawn again, somewhere else: the document is not used up
	if err := im.Draw(img, 0, 30, 10, 10, 1); err != nil {
		t.Fatal(err)
	}
	expect(t, img, 5, 35, red)
	expect(t, img, 25, 15, red)

	// an image whose origin is not (0, 0)
	shifted := image.NewRGBA(image.Rect(100, 100, 120, 120))
	if err := im.Draw(shifted, 5, 5, 10, 10, 1); err != nil {
		t.Fatal(err)
	}
	expect(t, shifted, 110, 110, red)
	expect(t, shifted, 102, 102, none)

	if err := im.Draw(img, 0, 0, 0, 10, 1); err == nil {
		t.Error("an empty target was accepted")
	}
}

func TestDrawOpacity(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	draw.Draw(img, img.Bounds(), image.NewUniform(white), image.Point{}, draw.Src)
	if err := parse(t, redSquare, Options{}).Draw(img, 0, 0, 10, 10, 0.5); err != nil {
		t.Fatal(err)
	}
	// half red over white
	got := at(img, 5, 5)
	if got.R != 255 || got.A != 255 || got.G < 120 || got.G > 135 || got.B != got.G {
		t.Errorf("half transparent red over white: %v", got)
	}
}

func TestCurrentColor(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10" fill="currentColor"/></svg>`
	img, err := Render(strings.NewReader(src), 10, Options{CurrentColor: "blue"})
	if err != nil {
		t.Fatal(err)
	}
	expect(t, img, 5, 5, blue)
}

// bluePNG returns a 2x2 blue image, encoded.
func bluePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	draw.Draw(img, img.Bounds(), image.NewUniform(blue), image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func imageDoc(href string) string {
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><image href="` + href + `" width="10" height="10"/></svg>`
}

func TestImages(t *testing.T) {
	encoded := bluePNG(t)
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded)

	// an image in a data URL is drawn with the default fetcher
	img, err := Render(strings.NewReader(imageDoc(dataURL)), 10, Options{})
	if err != nil {
		t.Fatal(err)
	}
	expect(t, img, 5, 5, blue)

	// a file is not read by the default fetcher
	dir := t.TempDir()
	file := filepath.Join(dir, "blue.png")
	if err := os.WriteFile(file, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	img, err = Render(strings.NewReader(imageDoc("blue.png")), 10, Options{BaseURL: "file://" + filepath.ToSlash(dir) + "/doc.svg"})
	if err != nil {
		t.Fatal(err)
	}
	expect(t, img, 5, 5, none)

	// it is with the fetcher of files
	img, err = Render(strings.NewReader(imageDoc("blue.png")), 10, Options{
		BaseURL: "file://" + filepath.ToSlash(dir) + "/doc.svg",
		Fetcher: FileFetcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	expect(t, img, 5, 5, blue)

	// an SVG document as an image
	nested := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(redSquare))
	img, err = Render(strings.NewReader(imageDoc(nested)), 10, Options{})
	if err != nil {
		t.Fatal(err)
	}
	expect(t, img, 5, 5, red)
}

func TestFetchers(t *testing.T) {
	r, err := DataFetcher("data:text/plain;charset=utf-8,a%20b")
	if err != nil {
		t.Fatal(err)
	}
	content := make([]byte, 3)
	r.Content.Read(content)
	if string(content) != "a b" || r.MimeType != "text/plain" || r.ProtocolEncoding != "utf-8" {
		t.Errorf("data URL: %q, %q, %q", content, r.MimeType, r.ProtocolEncoding)
	}
	// base64, on several lines and without padding
	r, err = DataFetcher("DATA:;base64,YW\n Jj")
	if err != nil {
		t.Fatal(err)
	}
	r.Content.Read(content)
	if string(content) != "abc" {
		t.Errorf("base64 data URL: %q", content)
	}

	for _, refused := range []string{"/etc/hosts", "file:///etc/hosts", "https://example.com/a.png", "data:nothing"} {
		if _, err := DataFetcher(refused); err == nil {
			t.Errorf("DataFetcher accepted %q", refused)
		}
	}
	if _, err := FileFetcher("https://example.com/a.png"); err == nil {
		t.Error("FileFetcher accepted a network URL")
	}
}

// Without a text renderer, text is skipped and the rest is drawn.
func TestNoText(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">
		<rect width="10" height="10" fill="red"/><text x="1" y="8" font-size="8">a</text></svg>`
	img, err := Render(strings.NewReader(src), 10, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			if got := at(img, x, y); got != red {
				t.Fatalf("pixel (%d, %d): %v, want the red of the background", x, y, got)
			}
		}
	}
}
