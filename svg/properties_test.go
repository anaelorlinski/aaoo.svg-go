package svg

import (
	"testing"

	pa "github.com/anaelorlinski/aaoo.cssengine-go/parser"
	pr "github.com/anaelorlinski/aaoo.cssengine-go/properties"
	"github.com/anaelorlinski/aaoo.cssengine-go/validation"
)

func validate(t *testing.T, prop pr.KnownProp, value string) pr.CssProperty {
	t.Helper()
	toks := pa.RemoveWhitespace(pa.Tokenize([]byte(value), true))
	got, err := validation.ValidateKnown(prop, toks, "")
	if err != nil {
		return nil
	}
	v, _ := got.(pr.CssProperty)
	return v
}

// The registered vocabulary must reach the engine as real properties.
func TestSVGPropertiesRegistered(t *testing.T) {
	for _, name := range []string{
		"fill", "stroke", "stroke-width", "stroke-dasharray", "marker-start",
		"stop-color", "text-anchor", "clip-rule", "paint-order", "mask",
	} {
		p, ok := pr.RegisteredProperty(name)
		if !ok {
			t.Errorf("%s is not registered", name)
			continue
		}
		if !pr.IsKnownProperty(p) {
			t.Errorf("%s is not a known property", name)
		}
		if p.String() != name {
			t.Errorf("String() = %q, want %q", p.String(), name)
		}
	}
	// SVG inheritance differs per property, and getting it wrong is silent.
	if !pr.IsInherited(PFill) {
		t.Error("fill must inherit")
	}
	if pr.IsInherited(PStopColor) {
		t.Error("stop-color must not inherit")
	}
	if pr.IsInherited(PClipPath) {
		t.Error("clip-path must not inherit")
	}
}

// The point of T9: a value the engine cannot understand is rejected, not
// silently taken as zero.
func TestSVGPropertyValidation(t *testing.T) {
	for _, tc := range []struct {
		prop  pr.KnownProp
		value string
		valid bool
	}{
		{PFill, "red", true},
		{PFill, "url(#grad)", true},
		{PFill, "none", true},
		{PFill, "currentColor", true},
		{PFill, "notacolor", false},

		{PStrokeWidth, "2px", true},
		{PStrokeWidth, "3", true},
		{PStrokeWidth, "banana", false},

		{PFillRule, "evenodd", true},
		{PFillRule, "nonzero", true},
		{PFillRule, "sideways", false}, // used to pass through unchecked

		{PStrokeLinecap, "round", true},
		{PStrokeLinecap, "rounded", false},

		{PMarkerStart, "url(#m)", true},
		{PMarkerStart, "none", true},
		{PMarkerStart, "#m", false},

		{PStopColor, "#abc", true},
		{PStopColor, "rgb(1,2,3)", true},
		{PStopColor, "url(#x)", false}, // stop-color takes a colour only

		{PStopOpacity, "0.5", true},
		{PStopOpacity, "50%", true},
		{PStopOpacity, "opaque", false},

		{PStrokeDasharray, "none", true},
		{PStrokeDasharray, "4 2 1", true},

		{PTextAnchor, "middle", true},
		{PTextAnchor, "centre", false},
	} {
		got := validate(t, tc.prop, tc.value)
		if tc.valid && got == nil {
			t.Errorf("%s: %q should be valid, was rejected", tc.prop, tc.value)
		}
		if !tc.valid && got != nil {
			t.Errorf("%s: %q should be rejected, got %v", tc.prop, tc.value, got)
		}
	}
}

// The accessors in tree.go take their defaults from the registered initial
// values rather than repeating them. This pins the values that behaviour
// depends on, so a change to either side has to be deliberate.
func TestInitialValuesMatchSVGSpec(t *testing.T) {
	for _, tc := range []struct {
		prop pr.KnownProp
		want string
	}{
		{PFill, "black"},
		{PStroke, "none"},
		{PFillRule, "nonzero"},
		{PClipRule, "nonzero"},
		{PStrokeLinecap, "butt"},
		{PStrokeLinejoin, "miter"},
		{PTextAnchor, "start"},
		{PMarkerStart, "none"},
		{PClipPath, "none"},
	} {
		if got := initialString(tc.prop); got != tc.want {
			t.Errorf("%s initial = %q, want %q", tc.prop, got, tc.want)
		}
	}
	if got := initialNumber(PStrokeMiterlimit); got != 4 {
		t.Errorf("stroke-miterlimit initial = %v, want 4", got)
	}
	for _, p := range []pr.KnownProp{PFillOpacity, PStrokeOpacity, PStopOpacity} {
		if got := initialNumber(p); got != 1 {
			t.Errorf("%s initial = %v, want 1", p, got)
		}
	}
	if got := initialLength(PStrokeWidth); got.V != 1 || got.U != Px {
		t.Errorf("stroke-width initial = %v%v, want 1px", got.V, got.U)
	}
	if got := initialLength(PStrokeDashoffset); got.V != 0 {
		t.Errorf("stroke-dashoffset initial = %v, want 0", got.V)
	}
}
