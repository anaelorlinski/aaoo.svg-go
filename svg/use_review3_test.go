package svg

import (
	"fmt"
	"strings"
	"testing"

	"github.com/anaelorlinski/aaoo.cssengine-go/parser"
	"github.com/anaelorlinski/aaoo.gfxcore-go"
)

// strokeCanvas records the stroke and fill colours set while drawing.
type strokeCanvas struct {
	outputPage
	strokes, fills *[]parser.RGBA
}

func (c strokeCanvas) State() gfxcore.GraphicState {
	return strokeState{c.outputPage.State(), c.strokes, c.fills}
}

func (c strokeCanvas) OnNewStack(f func()) { f() }

type strokeState struct {
	gfxcore.GraphicState
	strokes, fills *[]parser.RGBA
}

func (c strokeState) SetColorRgba(color parser.RGBA, stroke bool) {
	if stroke {
		*c.strokes = append(*c.strokes, color)
	} else {
		*c.fills = append(*c.fills, color)
	}
}

func lastFill(t *testing.T, src string) parser.RGBA {
	t.Helper()
	colors := fills(t, `<svg xmlns="http://www.w3.org/2000/svg">`+src+`</svg>`, "", nil)
	if len(colors) == 0 {
		t.Fatalf("no fill: %s", src)
	}
	return colors[len(colors)-1]
}

// Third review, SV3-3: a <use> instance inherits from the <use>, not from
// the ancestors of its definition.
func TestUseInheritsFromUse(t *testing.T) {
	for _, src := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg" fill="none"><symbol id="s"><rect width="1" height="1"/></symbol><use href="#s" fill="lime"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><g fill="red"><symbol id="s"><rect width="1" height="1"/></symbol></g><use href="#s" fill="lime"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><defs fill="red"><rect id="r" width="1" height="1"/></defs><use href="#r" fill="lime"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><style>svg { fill: red }</style><symbol id="s"><rect width="1" height="1"/></symbol><use href="#s" fill="lime"/></svg>`,
	} {
		colors := fills(t, src, "", nil)
		if len(colors) == 0 || colors[len(colors)-1] != green {
			t.Errorf("%s\n fills %v, want lime last", src, colors)
		}
	}
}

// Third review, SV3-4: currentColor in a symbol is the color of the <use>.
func TestUseCurrentColor(t *testing.T) {
	for _, src := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg"><symbol id="i" fill="none" stroke="currentColor"><path d="M0 0 L1 1"/></symbol><use href="#i" color="lime"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><symbol id="i" fill="none" stroke="currentColor"><path d="M0 0 L1 1"/></symbol><g color="lime"><use href="#i"/></g></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><symbol id="i" fill="none" stroke="currentColor"><path d="M0 0 L1 1"/></symbol><style>use { color: lime }</style><use href="#i"/></svg>`,
	} {
		img, err := Parse(strings.NewReader(src), "", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		var strokes, fills []parser.RGBA
		img.Draw(strokeCanvas{strokes: &strokes, fills: &fills}, 100, 100, nil)
		if !has(strokes, green) {
			t.Errorf("%s\n strokes %v, want lime", src, strokes)
		}
	}
}

// Third review, SV3-6: currentColor with color: inherit, unset, initial on
// the element itself, and inherited as a keyword.
func TestCurrentColorWithKeywordColor(t *testing.T) {
	black := parser.RGBA{A: 1}
	for src, want := range map[string]parser.RGBA{
		`<g color="lime" fill="red"><rect width="1" height="1" color="inherit" fill="currentColor"/></g>`:                       green,
		`<g color="lime" fill="red"><rect width="1" height="1" color="unset" fill="currentColor"/></g>`:                         green,
		`<g color="lime" fill="red"><rect width="1" height="1" color="initial" fill="currentColor"/></g>`:                       black,
		`<style>rect { color: inherit }</style><g color="lime" fill="red"><rect width="1" height="1" fill="currentColor"/></g>`: green,
		`<g color="lime"><g color="currentColor"><rect width="1" height="1" fill="currentColor"/></g></g>`:                      green,
		// inherited as currentColor: the color of the rect
		`<g color="red" fill="currentColor"><rect width="1" height="1" color="lime"/></g>`: green,
	} {
		if got := lastFill(t, src); got != want {
			t.Errorf("%s\n got %v, want %v", src, got, want)
		}
	}
}

// Third review, SV3-5: a <symbol viewBox> used without a size fills the
// viewport (100%), the size of the <use> winning.
func TestUseSymbolSize(t *testing.T) {
	for src, want := range map[string]string{
		`<symbol id="s" viewBox="0 0 10 10"><rect width="10" height="10"/></symbol><use href="#s"/>`:                      "100% 100%",
		`<symbol id="s" viewBox="0 0 10 10"><rect width="10" height="10"/></symbol><use href="#s" width="50"/>`:           "50 100%",
		`<symbol id="s" viewBox="0 0 10 10" width="20"><rect width="10" height="10"/></symbol><use href="#s"/>`:           "20 100%",
		`<symbol id="s" viewBox="0 0 10 10" width="20"><rect width="10" height="10"/></symbol><use href="#s" width="5"/>`: "5 100%",
	} {
		img, err := Parse(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg">`+src+`</svg>`), "", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		u := img.root.children[len(img.root.children)-1].graphicContent.(use)
		str := func(v Value) string {
			if v.U == Perc {
				return fmt.Sprintf("%g%%", v.V)
			}
			return fmt.Sprintf("%g", v.V)
		}
		if got := str(u.target.width) + " " + str(u.target.height); got != want {
			t.Errorf("%s\n size %s, want %s", src, got, want)
		}
	}
}

// countingCanvas counts the graphic stacks opened while drawing.
type countingCanvas struct {
	outputPage
	stacks *int
}

func (c countingCanvas) OnNewStack(f func()) { *c.stacks++; f() }

// Third review, SV3-2: markers stop with the draw budget (vertices ×
// marker children is quadratic in the document).
func TestMarkersStopWithBudget(t *testing.T) {
	const n = 4000
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg"><defs><marker id="m">`)
	for range n {
		b.WriteString(`<rect/>`)
	}
	b.WriteString(`</marker></defs><path marker-mid="url(#m)" d="M0 0`)
	for range n {
		b.WriteString(` l1 1`)
	}
	b.WriteString(`"/></svg>`)
	img, err := Parse(strings.NewReader(b.String()), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var stacks int
	img.Draw(countingCanvas{stacks: &stacks}, 100, 100, nil)
	if budget := max(1_000_000, img.elements*100); stacks > 2*budget {
		t.Errorf("%d stacks for a budget of %d draws", stacks, budget)
	}
}

// Third review, SV3-7: the type of <style> is compared as a media type.
func TestStyleType(t *testing.T) {
	for _, typ := range []string{`type="text/css"`, `type="TEXT/CSS"`, `type="text/css; charset=utf-8"`, `type=""`} {
		if got := lastFill(t, `<style `+typ+`>rect { fill: lime }</style><rect width="1" height="1"/>`); got != green {
			t.Errorf("<style %s>: %v", typ, got)
		}
	}
	if got := lastFill(t, `<style type="text/foo">rect { fill: lime }</style><rect width="1" height="1"/>`); got == green {
		t.Error("<style type=text/foo> applied")
	}
}

// Geometry attributes belong to their element: they are not inherited,
// also by the instances of a <use>.
func TestGeometryNotInherited(t *testing.T) {
	tree, err := newSVGContextReader(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg">
		<g cx="50" r="9" d="M0 0" class="a" fill="lime"><circle r="5"/></g></svg>`), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	circle := tree.root.children[0].children[0]
	for _, key := range []string{"cx", "d", "class"} {
		if v, has := circle.attrs[key]; has {
			t.Errorf("%s inherited: %q", key, v)
		}
	}
	if circle.attrs["r"] != "5" || circle.attrs["fill"] != "lime" {
		t.Errorf("attrs %v", circle.attrs)
	}
}
