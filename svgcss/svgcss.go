// Package svgcss registers the CSS properties of SVG into the CSS engine:
// fill, stroke, marker-*… are SVG's vocabulary, not the engine's, so they
// are defined here, with their validators, and registered at setup.
//
// The registration runs in the init function of this package: the
// packages parsing style sheets (svg, html/tree) import it, so Go runs it
// before them, whatever the import order of the program.
//
// An invalid value is rejected and reported instead of silently resolving
// to zero, which is the failure mode the conformance harness exists to
// measure.
package svgcss

import (
	"strings"

	pa "github.com/anaelorlinski/aaoo.cssengine-go/parser"
	pr "github.com/anaelorlinski/aaoo.cssengine-go/properties"
	"github.com/anaelorlinski/aaoo.cssengine-go/validation"
)

// The registered SVG properties, filled in by init.
var (
	PFill, PFillOpacity, PFillRule                     pr.KnownProp
	PStroke, PStrokeWidth, PStrokeOpacity              pr.KnownProp
	PStrokeLinecap, PStrokeLinejoin, PStrokeMiterlimit pr.KnownProp
	PStrokeDasharray, PStrokeDashoffset                pr.KnownProp
	PMarkerStart, PMarkerMid, PMarkerEnd               pr.KnownProp
	PStopColor, PStopOpacity                           pr.KnownProp
	PTextAnchor, PDominantBaseline, PAlignmentBaseline pr.KnownProp
	PClipRule, PClipPath, PMask, PFilter               pr.KnownProp
	PPaintOrder, PShapeRendering, PColorInterpolation  pr.KnownProp
	PMaskType, PMixBlendMode, PIsolation               pr.KnownProp
)

func init() {
	// paint: a colour, a url(...) reference, "none" or "currentColor".
	PFill = paintProp("fill", "black", true)
	PStroke = paintProp("stroke", "none", true)

	// <number> in [0, 1], clamped as SVG requires rather than rejected.
	PFillOpacity = numberProp("fill-opacity", 1, true)
	PStrokeOpacity = numberProp("stroke-opacity", 1, true)
	PStopOpacity = numberProp("stop-opacity", 1, false)
	PStrokeMiterlimit = numberProp("stroke-miterlimit", 4, true)

	// <length>
	PStrokeWidth = lengthProp("stroke-width", pr.Dimension{Value: 1, Unit: pr.Px}, true)
	PStrokeDashoffset = lengthProp("stroke-dashoffset", pr.Dimension{Value: 0, Unit: pr.Px}, true)

	// keyword sets — the values outside them are what used to pass silently
	PFillRule = keywordProp("fill-rule", "nonzero", true, "nonzero", "evenodd")
	PClipRule = keywordProp("clip-rule", "nonzero", true, "nonzero", "evenodd")
	PStrokeLinecap = keywordProp("stroke-linecap", "butt", true, "butt", "round", "square")
	PStrokeLinejoin = keywordProp("stroke-linejoin", "miter", true, "miter", "round", "bevel", "arcs", "miter-clip")
	PTextAnchor = keywordProp("text-anchor", "start", true, "start", "middle", "end")
	PDominantBaseline = keywordProp("dominant-baseline", "auto", false,
		"auto", "text-bottom", "alphabetic", "ideographic", "middle",
		"central", "mathematical", "hanging", "text-top")
	PAlignmentBaseline = keywordProp("alignment-baseline", "auto", false,
		"auto", "baseline", "before-edge", "text-before-edge", "middle",
		"central", "after-edge", "text-after-edge", "ideographic",
		"alphabetic", "hanging", "mathematical")
	PShapeRendering = keywordProp("shape-rendering", "auto", true,
		"auto", "optimizeSpeed", "crispEdges", "geometricPrecision")
	PColorInterpolation = keywordProp("color-interpolation", "sRGB", true, "auto", "sRGB", "linearRGB")
	PPaintOrder = keywordProp("paint-order", "normal", true, "normal", "fill", "stroke", "markers")
	PMaskType = keywordProp("mask-type", "luminance", false, "luminance", "alpha")
	PMixBlendMode = keywordProp("mix-blend-mode", "normal", false,
		"normal", "multiply", "screen", "overlay", "darken", "lighten", "color-dodge",
		"color-burn", "hard-light", "soft-light", "difference", "exclusion", "hue",
		"saturation", "color", "luminosity")
	PIsolation = keywordProp("isolation", "auto", false, "auto", "isolate")

	// url(...) or none
	PMarkerStart = referenceProp("marker-start", true)
	PMarkerMid = referenceProp("marker-mid", true)
	PMarkerEnd = referenceProp("marker-end", true)
	PClipPath = referenceProp("clip-path", false)
	PMask = referenceProp("mask", false)
	PFilter = referenceProp("filter", false)

	// <color>
	PStopColor = colorProp("stop-color", false)

	// dash pattern: a list of lengths, or none
	PStrokeDasharray = dasharrayProp()
}

// register wires a property into the engine and installs its validator.
func register(name string, initial pr.CssProperty, inherited bool,
	validate func(tokens []pa.Token, baseURL string) pr.CssProperty,
) pr.KnownProp {
	p := pr.RegisterProperty(name, initial, inherited)
	validation.RegisterValidator(p, validate)
	return p
}

// serialized keeps the value as written, for the properties svg still parses
// itself (paint and references). Validation rejects what is not well formed;
// interpretation stays in svg.
func serialized(tokens []pa.Token) pr.String { return pr.String(pa.Serialize(tokens)) }

func paintProp(name, initial string, inherited bool) pr.KnownProp {
	return register(name, pr.String(initial), inherited, func(tokens []pa.Token, _ string) pr.CssProperty {
		if len(tokens) == 0 {
			return nil
		}
		s := strings.TrimSpace(pa.Serialize(tokens))
		low := strings.ToLower(s)
		// SVG 2 paint: none | <color> | <url> | context-fill | context-stroke
		if low == "none" || low == "currentcolor" ||
			low == "context-fill" || low == "context-stroke" ||
			strings.HasPrefix(low, "url(") {
			return pr.String(s)
		}
		if c := pa.ParseColorString(s); c.Type != pa.ColorInvalid {
			return pr.String(s)
		}
		return nil // invalid: reported by the caller, not silently zero
	})
}

func colorProp(name string, inherited bool) pr.KnownProp {
	return register(name, pr.NewColor(0, 0, 0, 1), inherited, func(tokens []pa.Token, _ string) pr.CssProperty {
		if len(tokens) != 1 {
			return nil
		}
		c := pa.ParseColor(tokens[0])
		if c.Type == pa.ColorInvalid {
			return nil
		}
		return pr.Color(c)
	})
}

func numberProp(name string, initial pr.Float, inherited bool) pr.KnownProp {
	return register(name, initial, inherited, func(tokens []pa.Token, _ string) pr.CssProperty {
		if len(tokens) != 1 {
			return nil
		}
		switch t := tokens[0].(type) {
		case pa.Number:
			return pr.Float(t.ValueF)
		case pa.Percentage:
			return pr.Float(t.ValueF / 100)
		}
		return nil
	})
}

func lengthProp(name string, initial pr.Dimension, inherited bool) pr.KnownProp {
	return register(name, initial.Tagged(), inherited, func(tokens []pa.Token, _ string) pr.CssProperty {
		if len(tokens) != 1 {
			return nil
		}
		d, ok := length(tokens[0])
		if !ok {
			return nil
		}
		// Keep the parsed unit: stroke-width="10%" is a percentage of the
		// viewport diagonal, not 10px.
		return d.Tagged()
	})
}

func keywordProp(name, initial string, inherited bool, allowed ...string) pr.KnownProp {
	set := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		set[a] = true
	}
	return register(name, pr.String(initial), inherited, func(tokens []pa.Token, _ string) pr.CssProperty {
		if len(tokens) == 0 {
			return nil
		}
		// paint-order takes an ordered list; the rest take one keyword.
		for _, t := range tokens {
			if _, ok := t.(pa.Ident); !ok {
				return nil
			}
			if !set[pa.Serialize([]pa.Token{t})] {
				return nil
			}
		}
		return serialized(tokens)
	})
}

func referenceProp(name string, inherited bool) pr.KnownProp {
	return register(name, pr.String("none"), inherited, func(tokens []pa.Token, _ string) pr.CssProperty {
		if len(tokens) == 0 {
			return nil
		}
		s := strings.TrimSpace(pa.Serialize(tokens))
		low := strings.ToLower(s)
		if low == "none" || strings.HasPrefix(low, "url(") {
			return pr.String(s)
		}
		return nil
	})
}

func dasharrayProp() pr.KnownProp {
	return register("stroke-dasharray", pr.Values(nil), true, func(tokens []pa.Token, _ string) pr.CssProperty {
		if len(tokens) == 1 {
			if id, ok := tokens[0].(pa.Ident); ok && strings.EqualFold(id.Value, "none") {
				return pr.Values(nil)
			}
		}
		// Validated per token, not by re-serializing: the declaration arrives
		// with whitespace already removed, so "4 2 1" would serialize to
		// "421". Losing the separators that way is the exact failure the
		// string round-trip used to cause.
		out := make(pr.Values, 0, len(tokens))
		for _, t := range tokens {
			switch v := t.(type) {
			case pa.Number:
				out = append(out, pr.Dimension{Value: pr.Float(v.ValueF), Unit: pr.Px}.Tagged())
			case pa.Dimension:
				d, ok := length(t)
				if !ok {
					return nil
				}
				out = append(out, d.Tagged())
			case pa.Percentage:
				out = append(out, pr.PercToD(pr.Fl(v.ValueF)).Tagged())
			case pa.Literal:
				if v.Value != "," {
					return nil
				}
			default:
				return nil
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	})
}

// SupportedUnits is the subset of CSS units SVG can resolve. The rest —
// ch, lh, rlh — need font metrics the renderer may not have, so they are
// reported rather than silently taken as zero.
var SupportedUnits = map[pr.Unit]bool{
	pr.Px: true, pr.Cm: true, pr.Mm: true, pr.Pt: true, pr.In: true,
	pr.Q: true, pr.Pc: true, pr.Perc: true, pr.Em: true, pr.Rem: true,
	pr.Ex: true, pr.Vw: true, pr.Vh: true, pr.Vmin: true, pr.Vmax: true,
}

// length returns the SVG length of a token: a number of user units (CSS
// pixels), a percentage, or a dimension in a supported unit.
func length(token pa.Token) (pr.Dimension, bool) {
	switch t := token.(type) {
	case pa.Number:
		return pr.Dimension{Value: pr.Float(t.ValueF), Unit: pr.Px}, true
	case pa.Percentage:
		return pr.Dimension{Value: pr.Float(t.ValueF), Unit: pr.Perc}, true
	case pa.Dimension:
		unit, known := validation.LengthUnit(strings.ToLower(t.Unit))
		if known && SupportedUnits[unit] {
			return pr.Dimension{Value: pr.Float(t.ValueF), Unit: unit}, true
		}
	}
	return pr.Dimension{}, false
}
