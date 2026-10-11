package svg

// SVG's own property vocabulary is registered into the CSS engine by
// package svgcss; the typed reads below use it.

import (
	"github.com/anaelorlinski/aaoo.gfxcore-go"
	"github.com/anaelorlinski/aaoo.svg-go/svgcss"

	pr "github.com/anaelorlinski/aaoo.cssengine-go/properties"
)

// The SVG properties, registered by svgcss (whose init runs first).
var (
	PFill, PFillOpacity, PFillRule                     = svgcss.PFill, svgcss.PFillOpacity, svgcss.PFillRule
	PStroke, PStrokeWidth, PStrokeOpacity              = svgcss.PStroke, svgcss.PStrokeWidth, svgcss.PStrokeOpacity
	PStrokeLinecap, PStrokeLinejoin, PStrokeMiterlimit = svgcss.PStrokeLinecap, svgcss.PStrokeLinejoin, svgcss.PStrokeMiterlimit
	PStrokeDasharray, PStrokeDashoffset                = svgcss.PStrokeDasharray, svgcss.PStrokeDashoffset
	PMarkerStart, PMarkerMid, PMarkerEnd               = svgcss.PMarkerStart, svgcss.PMarkerMid, svgcss.PMarkerEnd
	PStopColor, PStopOpacity                           = svgcss.PStopColor, svgcss.PStopOpacity
	PTextAnchor, PDominantBaseline, PAlignmentBaseline = svgcss.PTextAnchor, svgcss.PDominantBaseline, svgcss.PAlignmentBaseline
	PClipRule, PClipPath, PMask, PFilter               = svgcss.PClipRule, svgcss.PClipPath, svgcss.PMask, svgcss.PFilter
	PMaskType, PMixBlendMode                           = svgcss.PMaskType, svgcss.PMixBlendMode
	PPaintOrder, PShapeRendering, PColorInterpolation  = svgcss.PPaintOrder, svgcss.PShapeRendering, svgcss.PColorInterpolation
)

// --- typed reads -------------------------------------------------------
//
// The accessors below read a property from the validated store rather than
// re-parsing the attribute string. props holds only values that were present
// and valid, so a miss falls back to the registered initial value — which is
// exactly what an absent or rejected declaration should resolve to.

// stringOf returns a string-valued property, or its initial value.
func stringOf(props pr.Properties, p pr.KnownProp) string {
	if v, ok := props[p].(pr.String); ok {
		return string(v)
	}
	return initialString(p)
}

// KeywordOf returns a keyword-valued property, or its initial value. It is the
// exported form of stringOf, for the packages layered on this one — svgtext
// reads text-anchor and the baselines through it rather than re-parsing the
// raw attribute.
func KeywordOf(props pr.Properties, p pr.KnownProp) string { return stringOf(props, p) }

// floatOf returns a number-valued property, or its initial value.
func floatOf(props pr.Properties, p pr.KnownProp) Fl {
	if v, ok := props[p].(pr.Float); ok {
		return Fl(v)
	}
	return Fl(initialNumber(p))
}

// lengthOf returns a length-valued property, or its initial value.
func lengthOf(props pr.Properties, p pr.KnownProp) Value {
	if v, ok := props[p].(pr.TaggedDim); ok {
		return Value{U: v.Unit, V: Fl(v.Value)}
	}
	return initialLength(p)
}

// colorOf returns a colour-valued property, or its initial value.
func colorOf(props pr.Properties, p pr.KnownProp) (pr.Color, bool) {
	if v, ok := props[p].(pr.Color); ok {
		return v, true
	}
	return pr.Color{}, false
}

// referenceOf returns the fragment id of a url(...) property, or "".
func referenceOf(props pr.Properties, p pr.KnownProp) string {
	return parseURLFragment(stringOf(props, p))
}

// lineCapOf and lineJoinOf map the validated keyword onto the backend enum.
// The keyword set is checked at validation, so an unknown value never reaches
// here — it was rejected and the initial value applies.
func lineCapOf(props pr.Properties) gfxcore.StrokeCapMode {
	switch stringOf(props, PStrokeLinecap) {
	case "round":
		return gfxcore.RoundCap
	case "square":
		return gfxcore.SquareCap
	default:
		return gfxcore.ButtCap
	}
}

func lineJoinOf(props pr.Properties) gfxcore.StrokeJoinMode {
	switch stringOf(props, PStrokeLinejoin) {
	case "round":
		return gfxcore.Round
	case "bevel":
		return gfxcore.Bevel
	default:
		return gfxcore.Miter
	}
}
