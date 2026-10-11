package svg

import (
	"bytes"
	"fmt"
	"testing"
)

// Cascade layers in SVG <style>, with the same rules (and the same layer
// parsing, from the cascade package) as in HTML.

func rectFill(t *testing.T, style string, fetcher Fetcher) string {
	t.Helper()
	src := `<svg xmlns="http://www.w3.org/2000/svg"><style>` + style + `</style><rect id="r" class="c" width="1" height="1"/></svg>`
	rect := findRect(importContext(t, src, "https://example.com/doc.svg", fetcher).root)
	if rect == nil {
		t.Fatal("no rect")
	}
	return rect.attrs["fill"]
}

func TestSVGLayers(t *testing.T) {
	for _, tc := range []struct{ style, want string }{
		// rules inside @layer used to be dropped
		{`@layer a { rect { fill: green } }`, "green"},
		// later layers win; unlayered rules win over layered ones
		{`@layer a { rect { fill: red } } @layer b { rect { fill: green } }`, "green"},
		{`@layer b, a; @layer a { rect { fill: green } } @layer b { rect#r { fill: red } }`, "green"},
		{`rect { fill: green } @layer a { rect.c#r { fill: red } }`, "green"},
		// nesting: a layer's own rules after its nested layers
		{`@layer f { rect { fill: green } @layer t { rect.c { fill: red } } }`, "green"},
		// anonymous layers are distinct
		{`@layer { rect { fill: red } } @layer { rect { fill: green } }`, "green"},
		// !important: earlier layers win, unlayered rules lose
		{`@layer a { rect { fill: green !important } } @layer b { rect { fill: red !important } } rect { fill: blue !important }`, "green"},
		// invalid names ignore the block
		{`rect { fill: green } @layer inherit { rect#r { fill: red !important } }`, "green"},
	} {
		if got := rectFill(t, tc.style, nil); got != tc.want {
			t.Errorf("%s\n fill %q, want %q", tc.style, got, tc.want)
		}
	}
}

func TestSVGImportLayer(t *testing.T) {
	fetcher := func(url string) (Resource, error) {
		switch url {
		case "https://example.com/red.css":
			return Resource{Content: bytes.NewReader([]byte(`rect { fill: red }`))}, nil
		case "https://example.com/green.css":
			return Resource{Content: bytes.NewReader([]byte(`rect { fill: green }`))}, nil
		}
		return Resource{}, fmt.Errorf("not found: %s", url)
	}
	for _, tc := range []struct{ style, want string }{
		{`@import "red.css" layer(x); rect { fill: green }`, "green"},
		{`@import url(red.css) layer; rect { fill: green }`, "green"},
		{`@layer base, theme; @import "green.css" layer(theme); @layer base { rect { fill: red } }`, "green"},
	} {
		if got := rectFill(t, tc.style, fetcher); got != tc.want {
			t.Errorf("%s\n fill %q, want %q", tc.style, got, tc.want)
		}
	}
}

// With the shared stylesheet walker, SVG <style> follows the same rules as
// HTML for conditional rules and @import (it used to ignore them).
func TestSVGConditionalRules(t *testing.T) {
	for _, tc := range []struct{ style, want string }{
		{`@media print { rect { fill: green } }`, "green"},
		{`rect { fill: green } @media screen { rect { fill: red } }`, "green"},
		{`@supports (display: flex) { rect { fill: green } }`, "green"},
		{`rect { fill: green } @supports (display: banana) { rect { fill: red } }`, "green"},
	} {
		if got := rectFill(t, tc.style, nil); got != tc.want {
			t.Errorf("%s\n fill %q, want %q", tc.style, got, tc.want)
		}
	}
}

func TestSVGImportConditions(t *testing.T) {
	fetcher := func(url string) (Resource, error) {
		if url == "https://example.com/red.css" {
			return Resource{Content: bytes.NewReader([]byte(`rect { fill: red }`))}, nil
		}
		return Resource{Content: bytes.NewReader([]byte(`rect { fill: green }`))}, nil
	}
	for _, tc := range []struct{ style, want string }{
		{`@import "green.css" print;`, "green"},
		{`rect { fill: green } @import "red.css" print;`, "green"},     // imports come first
		{`@import "red.css" screen; rect { fill: green }`, "green"},    // false media
		{`@import url("green.css") supports(display: flex);`, "green"}, // quoted url, supports()
	} {
		if got := rectFill(t, tc.style, fetcher); got != tc.want {
			t.Errorf("%s\n fill %q, want %q", tc.style, got, tc.want)
		}
	}
}
