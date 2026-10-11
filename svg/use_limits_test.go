package svg

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anaelorlinski/aaoo.cssengine-go/parser"
	"github.com/anaelorlinski/aaoo.svg-go/internal/limits"
	"golang.org/x/net/html"
)

// fills parses and draws src, returning the fill colours used.
func fills(t *testing.T, src, baseURL string, fetcher Fetcher) []parser.RGBA {
	t.Helper()
	img, err := Parse(strings.NewReader(src), baseURL, nil, fetcher)
	if err != nil {
		t.Fatal(err)
	}
	var colors []parser.RGBA
	img.Draw(colorCanvas{colors: &colors}, 100, 100, nil)
	return colors
}

func has(colors []parser.RGBA, c parser.RGBA) bool {
	for _, got := range colors {
		if got == c {
			return true
		}
	}
	return false
}

var green = parser.RGBA{G: 1, A: 1}

// serve returns a fetcher serving documents, counting the fetches.
func serve(documents map[string]string, fetches *int) Fetcher {
	return func(url string) (Resource, error) {
		*fetches++
		doc, ok := documents[url]
		if !ok {
			return Resource{}, fmt.Errorf("not found: %s", url)
		}
		return Resource{Content: bytes.NewReader([]byte(doc))}, nil
	}
}

// SV1: nested <use> expanding exponentially ("billion laughs") is bounded.
func TestUseExpansionBounded(t *testing.T) {
	var src strings.Builder
	src.WriteString(`<svg xmlns="http://www.w3.org/2000/svg"><defs><rect id="l0" width="1" height="1"/>`)
	for level := 1; level <= 9; level++ {
		fmt.Fprintf(&src, `<g id="l%d">`, level)
		for range 10 {
			fmt.Fprintf(&src, `<use href="#l%d"/>`, level-1)
		}
		src.WriteString(`</g>`)
	}
	src.WriteString(`</defs><use href="#l9"/></svg>`) // 10^9 rectangles
	start := time.Now()
	if _, err := Parse(strings.NewReader(src.String()), "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 5*timeScale*time.Second {
		t.Errorf("parse took %s", d)
	}
}

// SV2: external <use> cycles and fan-out are bounded.
func TestUseExternalCycle(t *testing.T) {
	var fetches int
	docs := map[string]string{
		"asset:a": `<svg xmlns="http://www.w3.org/2000/svg"><use href="asset:b"/><rect width="1" height="1" fill="lime"/></svg>`,
		"asset:b": `<svg xmlns="http://www.w3.org/2000/svg"><use href="asset:a"/><use href="asset:b#x"/></svg>`,
	}
	colors := fills(t, `<svg xmlns="http://www.w3.org/2000/svg"><use href="asset:a"/></svg>`, "asset:main", serve(docs, &fetches))
	if fetches != 2 {
		t.Errorf("%d fetches, want 2 (a, b)", fetches)
	}
	if !has(colors, green) {
		t.Errorf("the referenced content is drawn: %v", colors)
	}
	// a document referencing itself
	fetches = 0
	self := map[string]string{"https://x/s.svg": `<svg xmlns="http://www.w3.org/2000/svg"><use href="s.svg"/><use href="https://x/s.svg#a"/></svg>`}
	fills(t, `<svg xmlns="http://www.w3.org/2000/svg"><use href="https://x/s.svg"/></svg>`, "https://x/main.svg", serve(self, &fetches))
	if fetches != 1 {
		t.Errorf("self reference: %d fetches", fetches)
	}
	// fan-out
	fetches = 0
	fan := map[string]string{}
	for i := range 200 {
		fan[fmt.Sprintf("asset:%d", i)] = fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg"><use href="asset:%d"/><use href="asset:%d"/></svg>`, i+1, i+2)
	}
	fills(t, `<svg xmlns="http://www.w3.org/2000/svg"><use href="asset:0"/></svg>`, "asset:main", serve(fan, &fetches))
	if fetches > limits.UseDocuments {
		t.Errorf("fan-out: %d fetches, want at most %d", fetches, limits.UseDocuments)
	}
}

// SV3: an external <use> selects its fragment, and the referenced document
// resolves its own references and keeps the referencing document's
// definitions.
func TestUseExternalDocument(t *testing.T) {
	var fetches int
	docs := map[string]string{"https://x/b.svg": `<svg xmlns="http://www.w3.org/2000/svg">
		<rect id="x" width="1" height="1" fill="lime"/>
		<rect id="y" width="1" height="1" fill="red"/>
		<use id="z" href="#w"/>
		<rect id="w" width="1" height="1" fill="lime"/>
		<linearGradient id="g"><stop offset="0" stop-color="red"/></linearGradient>
	</svg>`}
	fetcher := serve(docs, &fetches)
	colors := fills(t, `<svg xmlns="http://www.w3.org/2000/svg"><use href="b.svg#x"/></svg>`, "https://x/a.svg", fetcher)
	if has(colors, red) || !has(colors, green) {
		t.Errorf("fragment: %v, want only lime", colors)
	}
	// #w resolves in b.svg, not in a.svg
	colors = fills(t, `<svg xmlns="http://www.w3.org/2000/svg"><rect id="w" width="1" height="1" fill="blue"/><use href="b.svg#z"/></svg>`, "https://x/a.svg", fetcher)
	if !has(colors, green) {
		t.Errorf("references of the referenced document: %v, want lime", colors)
	}
	// b.svg's gradient #g does not replace a.svg's
	img, err := Parse(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg">
		<linearGradient id="g"><stop offset="0" stop-color="lime"/><stop offset="1" stop-color="lime"/></linearGradient>
		<use href="b.svg#x"/></svg>`), "https://x/a.svg", nil, fetcher)
	if err != nil {
		t.Fatal(err)
	}
	if grad, ok := img.definitions.paintServers["g"].(gradient); !ok || len(grad.colors) != 2 {
		t.Errorf("the referencing document's gradient was replaced: %#v", img.definitions.paintServers["g"])
	}
}

// SV7: a <use> of another document with the same path is not local.
func TestUseOtherOrigin(t *testing.T) {
	var fetches int
	docs := map[string]string{
		"https://other.com/a.svg": `<svg xmlns="http://www.w3.org/2000/svg"><rect id="x" width="1" height="1" fill="lime"/></svg>`,
		"asset:b":                 `<svg xmlns="http://www.w3.org/2000/svg"><rect id="x" width="1" height="1" fill="lime"/></svg>`,
	}
	fetcher := serve(docs, &fetches)
	for _, href := range []string{"https://other.com/a.svg#x", "asset:b#x"} {
		fetches = 0
		colors := fills(t, `<svg xmlns="http://www.w3.org/2000/svg"><use href="`+href+`"/></svg>`, "https://example.com/a.svg", fetcher)
		if fetches != 1 || !has(colors, green) {
			t.Errorf("%s: %d fetches, colors %v", href, fetches, colors)
		}
	}
	// a URL to the document itself is local
	fetches = 0
	colors := fills(t, `<svg xmlns="http://www.w3.org/2000/svg"><defs><rect id="x" width="1" height="1" fill="lime"/></defs><use href="a.svg#x"/></svg>`, "https://example.com/a.svg", fetcher)
	if fetches != 0 || !has(colors, green) {
		t.Errorf("own document: %d fetches, colors %v", fetches, colors)
	}
}

// SV5, SV6: pseudo-element rules do not apply to elements; unset and
// initial are resolved.
func TestSVGCascadeKeywords(t *testing.T) {
	black := parser.RGBA{A: 1}
	for _, tc := range []struct {
		src  string
		want parser.RGBA
	}{
		{`<style>rect::before { fill: red } rect::marker { fill: red !important }</style><rect width="1" height="1" fill="lime"/>`, green},
		{`<style>rect { fill: unset }</style><g fill="lime"><rect width="1" height="1" fill="red"/></g>`, green},
		{`<style>rect { fill: UNSET }</style><g fill="lime"><rect width="1" height="1" fill="red"/></g>`, green},
		{`<style>rect { fill: initial }</style><g fill="lime"><rect width="1" height="1" fill="red"/></g>`, black},
		{`<g fill="lime"><rect width="1" height="1" fill="inherit"/></g>`, green},
	} {
		colors := fills(t, `<svg xmlns="http://www.w3.org/2000/svg">`+tc.src+`</svg>`, "", nil)
		if len(colors) == 0 || colors[len(colors)-1] != tc.want {
			t.Errorf("%s\n fills %v, want %v last", tc.src, colors, tc.want)
		}
	}
}

// SV8: an invalid or "auto" size does not fail the document.
func TestSVGRootSizeAuto(t *testing.T) {
	for _, attrs := range []string{`width="10" height="auto"`, `width="2ch" height="10"`, `width="banana"`} {
		img, err := Parse(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg" `+attrs+`><rect width="1" height="1"/></svg>`), "", nil, nil)
		if err != nil {
			t.Errorf("%s: %s", attrs, err)
			continue
		}
		if _, h := img.DisplayedSize(); strings.Contains(attrs, "auto") && h.U != Perc {
			t.Errorf("%s: height %v, want auto (100%%)", attrs, h)
		}
	}
}

// G2-1: chains of clip paths (masks, markers, patterns) referencing each
// other are bounded at draw time, relative to the document: a large
// document without amplification is drawn whole.
func TestDrawAmplificationBounded(t *testing.T) {
	chain := func(depth int) string {
		var b strings.Builder
		b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg"><defs>`)
		for k := 0; k < depth; k++ {
			fmt.Fprintf(&b, `<clipPath id="c%d">`, k)
			for range 10 {
				fmt.Fprintf(&b, `<rect width="1" height="1" clip-path="url(#c%d)"/>`, k+1)
			}
			b.WriteString(`</clipPath>`)
		}
		fmt.Fprintf(&b, `<clipPath id="c%d"><rect width="1" height="1"/></clipPath>`, depth)
		b.WriteString(`</defs><rect width="10" height="10" clip-path="url(#c0)"/></svg>`)
		return b.String()
	}
	start := time.Now()
	fills(t, chain(9), "", nil) // 10^9 draws without the bound
	if d := time.Since(start); d > 5*timeScale*time.Second {
		t.Errorf("clip chain drawn in %s", d)
	}
	// 30,000 rects sharing one clip path: 60,000 draws, all done
	var big strings.Builder
	big.WriteString(`<svg xmlns="http://www.w3.org/2000/svg"><clipPath id="c"><rect width="5" height="5"/></clipPath>`)
	for range 30_000 {
		big.WriteString(`<rect width="1" height="1" fill="lime" clip-path="url(#c)"/>`)
	}
	big.WriteString(`</svg>`)
	colors := fills(t, big.String(), "", nil)
	if n := len(colors); n < 30_000 {
		t.Errorf("a large document was cut: %d fills", n)
	}
}

// G2-2: a <use> does not restyle the definition it instantiates.
func TestUseDoesNotRestyleDefinition(t *testing.T) {
	colors := fills(t, `<svg xmlns="http://www.w3.org/2000/svg"><defs><symbol id="s"><rect width="1" height="1"/></symbol></defs>
		<use href="#s" fill="lime"/><use href="#s" fill="red"/></svg>`, "", nil)
	if len(colors) < 2 || colors[len(colors)-1] != red {
		t.Errorf("second <use>: %v, want red last", colors)
	}
	colors = fills(t, `<svg xmlns="http://www.w3.org/2000/svg"><use href="#g" fill="red"/><g id="g"><rect width="1" height="1"/></g></svg>`, "", nil)
	if last := colors[len(colors)-1]; last == red {
		t.Errorf("the original element was restyled by the <use>: %v", colors)
	}
}

// G2-3, G2-6: a sprite sheet is fetched and charged once; refused uses do
// not cost their target's size.
func TestUseSpriteSheet(t *testing.T) {
	var sprite strings.Builder
	sprite.WriteString(`<svg xmlns="http://www.w3.org/2000/svg">`)
	for i := range 2000 {
		fmt.Fprintf(&sprite, `<symbol id="i%d"><rect width="1" height="1" fill="lime"/></symbol>`, i)
	}
	sprite.WriteString(`</svg>`)
	var doc strings.Builder
	doc.WriteString(`<svg xmlns="http://www.w3.org/2000/svg">`)
	for i := range 100 {
		fmt.Fprintf(&doc, `<use href="sprite.svg#i%d"/>`, i)
	}
	doc.WriteString(`</svg>`)
	var fetches int
	fetcher := serve(map[string]string{"https://x/sprite.svg": sprite.String()}, &fetches)
	img, err := ParseNode(mustParseHTML(t, doc.String()), "https://x/a.svg", nil, fetcher, "", &Device{})
	if err != nil {
		t.Fatal(err)
	}
	var colors []parser.RGBA
	img.Draw(colorCanvas{colors: &colors}, 100, 100, nil)
	var icons int
	for _, c := range colors {
		if c == green {
			icons++
		}
	}
	if icons != 100 || fetches != 1 {
		t.Errorf("%d icons drawn, %d fetches; want 100, 1", icons, fetches)
	}

	// refused uses of a big target are cheap
	var big strings.Builder
	big.WriteString(`<svg xmlns="http://www.w3.org/2000/svg"><defs><g id="big">`)
	for range 50_000 {
		big.WriteString(`<rect/>`)
	}
	big.WriteString(`</g></defs>`)
	for range 20_000 {
		big.WriteString(`<use href="#big"/>`)
	}
	big.WriteString(`</svg>`)
	start := time.Now()
	if _, err := Parse(strings.NewReader(big.String()), "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*timeScale*time.Second {
		t.Errorf("parse took %s", d)
	}
}

// G2-4: a pattern's content uses the definitions of the document.
func TestPatternUsesDefinitions(t *testing.T) {
	colors := fills(t, `<svg xmlns="http://www.w3.org/2000/svg">
		<linearGradient id="g"><stop offset="0" stop-color="lime"/></linearGradient>
		<pattern id="p" width="10" height="10" patternUnits="userSpaceOnUse"><rect width="10" height="10" fill="url(#g) red"/></pattern>
		<rect width="10" height="10" fill="url(#p)"/></svg>`, "", nil)
	if has(colors, red) {
		t.Errorf("the gradient inside the pattern was not found: %v", colors)
	}
	// a pattern referencing itself through its content is refused, not looped
	fills(t, `<svg xmlns="http://www.w3.org/2000/svg">
		<pattern id="p" width="10" height="10" patternUnits="userSpaceOnUse"><rect width="10" height="10" fill="url(#p)"/></pattern>
		<rect width="10" height="10" fill="url(#p)"/></svg>`, "", nil)
}

func mustParseHTML(t *testing.T, src string) *html.Node {
	t.Helper()
	root, err := html.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// G2-8: without an image loader, an <image> is skipped, not a nil call.
func TestNilImageLoader(t *testing.T) {
	fills(t, `<svg xmlns="http://www.w3.org/2000/svg"><image href="a.png" width="1" height="1"/><rect width="1" height="1"/></svg>`, "", nil)
}
