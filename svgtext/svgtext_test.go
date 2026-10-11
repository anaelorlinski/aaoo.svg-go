package svgtext

// Span-parsing tests, moved here from svg/svg_test.go when text became its
// own package: they exercise how a <text> element's attributes become a
// renderable span, which is this package's job rather than core's.

import (
	"testing"

	"github.com/anaelorlinski/aaoo.textengine-go/layout"
)

func TestNewSpanStyle(t *testing.T) {
	r := New(nil).(*textRenderer)

	// `font: italic 13px sans-serif` is expanded by the CSS font shorthand.
	span, err := r.NewSpan([]byte("My"), map[string]string{
		"font-style": "italic", "font-size": "13px", "font-family": "sans-serif",
	}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	ts, ok := span.(*textSpan)
	if !ok {
		t.Fatalf("unexpected content %T", span)
	}
	if ts.text != "My" {
		t.Fatalf("unexpected text %q", ts.text)
	}
	if ts.style.Style != layout.FSty_Italic {
		t.Fatalf("unexpected font style %v", ts.style.Style)
	}
}

func TestNewSpanPositions(t *testing.T) {
	r := New(nil).(*textRenderer)

	span, err := r.NewSpan([]byte("ABCD"), map[string]string{
		"x": "0 4 7", "y": "1.5", "dx": "2pt 1px", "rotate": "1 2 3",
		"font-family": "weasyprint", "font-size": "2",
	}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	ts := span.(*textSpan)
	if ts.text != "ABCD" {
		t.Fatalf("unexpected text %q", ts.text)
	}
	if len(ts.x) != 3 {
		t.Fatalf("expected 3 x positions, got %d", len(ts.x))
	}
	if len(ts.dx) != 2 {
		t.Fatalf("expected 2 dx offsets, got %d", len(ts.dx))
	}
	if len(ts.rotate) != 3 {
		t.Fatalf("expected 3 rotations, got %v", ts.rotate)
	}
	// isText distinguishes <text> from <tspan>; the anchor pass depends on it.
	if !ts.isText {
		t.Fatal("expected isText for a <text> element")
	}
}
