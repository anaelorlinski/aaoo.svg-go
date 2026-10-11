package withtext

import (
	"image"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/anaelorlinski/aaoo.svg-go/svgrender"
	"github.com/anaelorlinski/aaoo.textengine-go/face"
)

// The font used is the one of the text engine, checked out beside this
// module.
const testFont = "../../../aaoo.textengine-go/sfnt/resources/DejaVuSerif.ttf"

const textDoc = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 60 30">
	<text x="5" y="22" font-family="test" font-size="20" fill="black">Hi</text></svg>`

func inked(img *image.RGBA) (n int) {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			n++
		}
	}
	return n
}

// Importing the package is enough for text to be drawn.
func TestTextIsDrawn(t *testing.T) {
	content, err := os.ReadFile(testFont)
	if err != nil {
		t.Skip("no test font:", err)
	}
	if err := LoadFont("test", content, face.Style{Weight: 400, Width: face.WidthNormal}); err != nil {
		t.Fatal(err)
	}

	img, err := svgrender.Render(strings.NewReader(textDoc), 120, svgrender.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if n := inked(img); n < 200 {
		t.Fatalf("%d pixels drawn: the text is missing", n)
	}

	// from several goroutines
	var wg sync.WaitGroup
	counts := make([]int, 8)
	for i := range counts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			img, err := svgrender.Render(strings.NewReader(textDoc), 120, svgrender.Options{})
			if err == nil {
				counts[i] = inked(img)
			}
		}()
	}
	wg.Wait()
	for i, n := range counts {
		if n != inked(img) {
			t.Errorf("goroutine %d drew %d pixels, want %d", i, n, inked(img))
		}
	}
}
