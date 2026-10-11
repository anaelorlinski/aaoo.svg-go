package svg

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/anaelorlinski/aaoo.cssengine-go/parser"
	"github.com/anaelorlinski/aaoo.gfxcore-go"
	"github.com/anaelorlinski/aaoo.svg-go/internal/limits"
	"golang.org/x/net/html"
)

// The @media rules of an SVG image are evaluated against its viewport, at
// the size it is drawn at; those of an inline SVG against the host device.

// colorCanvas records the fill colours set while drawing.
type colorCanvas struct {
	outputPage
	colors *[]parser.RGBA
}

func (c colorCanvas) State() gfxcore.GraphicState { return colorState{c.outputPage.State(), c.colors} }

type colorState struct {
	gfxcore.GraphicState
	colors *[]parser.RGBA
}

func (c colorState) SetColorRgba(color parser.RGBA, stroke bool) {
	if !stroke {
		*c.colors = append(*c.colors, color)
	}
}

func drawnFill(t *testing.T, img *SVGImage, width, height Fl) parser.RGBA {
	t.Helper()
	var colors []parser.RGBA
	img.Draw(colorCanvas{colors: &colors}, width, height, nil)
	// the <svg> root, then the <rect>
	if len(colors) != 2 {
		t.Fatalf("drawn at %gx%g: fills %v", width, height, colors)
	}
	return colors[1]
}

var (
	red  = parser.RGBA{R: 1, A: 1}
	blue = parser.RGBA{B: 1, A: 1}
)

const narrowBlue = `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100" viewBox="0 0 10 10">
	<style>rect { fill: red } @media (max-width: 150px) { rect { fill: blue } }</style>
	<rect width="10" height="10"/></svg>`

func TestSVGImageMediaDrawnSize(t *testing.T) {
	img, err := Parse(strings.NewReader(narrowBlue), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if img.resize == nil {
		t.Fatal("size-dependent styles not detected")
	}
	// intrinsic size 200x100: red
	if got := drawnFill(t, img, 200, 100); got != red {
		t.Errorf("intrinsic size: %v", got)
	}
	// drawn narrower: blue, and back
	if got := drawnFill(t, img, 100, 50); got != blue {
		t.Errorf("drawn at 100px: %v", got)
	}
	if got := drawnFill(t, img, 400, 200); got != red {
		t.Errorf("drawn at 400px: %v", got)
	}
	// the variants are cached, by the result of the media queries: every
	// size up to 150px wide shares one
	if a, b := img.resize.variant(100, 50), img.resize.variant(120.0001, 50); a == nil || a != b {
		t.Error("variant not cached")
	}
	if img.resize.variant(200, 100) != nil || img.resize.variant(1000, 1) != nil {
		t.Error("sizes over 150px are the image itself")
	}
	if len(img.resize.variants) != 1 {
		t.Errorf("%d variants, want 1", len(img.resize.variants))
	}
	// non-finite sizes are not styled again
	nan := Fl(math.NaN())
	if img.resize.variant(nan, 50) != nil || img.resize.variant(Fl(math.Inf(1)), 50) != nil {
		t.Error("non-finite sizes")
	}
}

func TestSVGImageMediaNotSizeDependent(t *testing.T) {
	for _, style := range []string{
		`rect { fill: blue }`,
		`@media print { rect { fill: blue } }`,
		`@media (color) { rect { fill: blue } }`,
	} {
		img, err := Parse(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100"><style>`+
			style+`</style><rect width="10" height="10"/></svg>`), "", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if img.resize != nil {
			t.Errorf("%s: parsed again at draw time", style)
		}
		if got := drawnFill(t, img, 50, 50); got != blue {
			t.Errorf("%s: %v", style, got)
		}
	}
}

func TestSVGInlineMediaHostDevice(t *testing.T) {
	parse := func(host Device) *SVGImage {
		root, err := parseHTML(narrowBlue)
		if err != nil {
			t.Fatal(err)
		}
		img, err := ParseNode(root, "", nil, nil, "", &host)
		if err != nil {
			t.Fatal(err)
		}
		if img.resize != nil {
			t.Error("inline SVG styled again at draw time")
		}
		return img
	}
	// the host's width decides, not the drawn size
	if got := drawnFill(t, parse(Device{Width: 120, Height: 800}), 400, 200); got != blue {
		t.Errorf("narrow host: %v", got)
	}
	if got := drawnFill(t, parse(Device{Width: 800, Height: 800}), 100, 50); got != red {
		t.Errorf("wide host: %v", got)
	}
	// the zero device is A4 (794px wide)
	if got := drawnFill(t, parse(Device{}), 100, 50); got != red {
		t.Errorf("zero host: %v", got)
	}
}

func TestSVGIntrinsicDevice(t *testing.T) {
	for attrs, want := range map[string][2]Fl{
		`width="200" height="100"`:                   {200, 100},
		`width="2in" height="1in"`:                   {192, 96},
		`width="10em" height="5em"`:                  {160, 80},
		`width="200" viewBox="0 0 4 1"`:              {200, 50},
		`height="100" viewBox="0 0 4 1"`:             {400, 100},
		`viewBox="0 0 2 1"`:                          {300, 150},
		`viewBox="0 0 1 1"`:                          {300, 300},
		``:                                           {300, 150},
		`width="50%" height="100"`:                   {300, 100},
		`width="50%" height="100" viewBox="0 0 2 1"`: {200, 100},
	} {
		root, err := parseHTML(`<svg xmlns="http://www.w3.org/2000/svg" ` + attrs + `></svg>`)
		if err != nil {
			t.Fatal(err)
		}
		svgRoot, err := findSVGRoot(root)
		if err != nil {
			t.Fatal(err)
		}
		if d := intrinsicDevice(svgRoot); d.Width != want[0] || d.Height != want[1] {
			t.Errorf("%s: %gx%g, want %gx%g", attrs, d.Width, d.Height, want[0], want[1])
		}
	}
}

func parseHTML(src string) (*html.Node, error) { return html.Parse(strings.NewReader(src)) }

// Variants are bounded, and keyed exactly even when a size makes a nested
// query reachable.
func TestSVGImageMediaVariants(t *testing.T) {
	img, err := Parse(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100">
	<style>rect { fill: red }
	@media (max-width: 150px) { rect { fill: blue } @media (max-width: 50px) { rect { fill: lime } } }</style>
	<rect width="10" height="10"/></svg>`), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	lime := parser.RGBA{G: 1, A: 1}
	for _, tc := range []struct {
		width Fl
		want  parser.RGBA
	}{{100, blue}, {40, lime}, {120, blue}, {30, lime}, {300, red}} {
		if got := drawnFill(t, img, tc.width, 50); got != tc.want {
			t.Errorf("drawn at %gpx: %v, want %v", tc.width, got, tc.want)
		}
	}
	if len(img.resize.variants) != 2 {
		t.Errorf("%d variants, want 2 (blue, lime)", len(img.resize.variants))
	}
	// bounded: one query per width, all distinct
	var style strings.Builder
	for i := range 20 {
		fmt.Fprintf(&style, "@media (max-width: %dpx) { rect { stroke-width: %d } } ", 10*(i+1), i)
	}
	img, err = Parse(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg" width="1000" height="100"><style>`+
		style.String()+`</style><rect width="10" height="10"/></svg>`), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		img.resize.variant(Fl(10*i+5), 50)
	}
	if len(img.resize.variants) != limits.SVGVariants {
		t.Errorf("%d variants, want %d", len(img.resize.variants), limits.SVGVariants)
	}
}

// G2-7: the media attribute of <style> is evaluated against the device, and
// recorded when it tests the size.
func TestSVGStyleMediaAttribute(t *testing.T) {
	src := func(media string) string {
		return `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100"><style>rect { fill: lime }</style><style media="` + media + `">rect { fill: red }</style><rect width="1" height="1"/></svg>`
	}
	for media, want := range map[string]parser.RGBA{"screen": {G: 1, A: 1}, "print": red, "all": red, "(max-width: 150px)": {G: 1, A: 1}} {
		img, err := Parse(strings.NewReader(src(media)), "", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := drawnFill(t, img, 200, 100); got != want {
			t.Errorf("media=%q: %v, want %v", media, got, want)
		}
	}
	// a size-dependent media attribute styles the image again at the drawn size
	img, _ := Parse(strings.NewReader(src("(max-width: 150px)")), "", nil, nil)
	if got := drawnFill(t, img, 100, 50); got != red {
		t.Errorf("drawn at 100px: %v, want red", got)
	}
}

// currentColor inside a color function is the color of the element.
func TestSVGCurrentColorInFunction(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" color="red">
	<rect width="10" height="10" style="fill: color-mix(in srgb, CurrentColor, blue)"/></svg>`
	img, err := Parse(strings.NewReader(src), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := drawnFill(t, img, 10, 10), (parser.RGBA{R: 0.5, B: 0.5, A: 1}); math.Abs(float64(got.R-want.R)) > 1e-3 || math.Abs(float64(got.B-want.B)) > 1e-3 || got.A != 1 {
		t.Errorf("fill: got %v, want %v", got, want)
	}
}
