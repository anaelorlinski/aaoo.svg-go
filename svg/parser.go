package svg

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	pa "github.com/anaelorlinski/aaoo.cssengine-go/parser"
	pr "github.com/anaelorlinski/aaoo.cssengine-go/properties"
	"github.com/anaelorlinski/aaoo.cssengine-go/validation"
	"github.com/anaelorlinski/aaoo.svg-go/internal/utils"
	"github.com/anaelorlinski/aaoo.svg-go/svgcss"
)

// provide low-level functions to read basic SVG data

type Fl = utils.Fl

// Unit is the CSS length unit enum. SVG used to define a parallel one, which
// meant a name table, a conversion table and a mapping between the two — all
// of it duplicating css/properties. The aliases below keep call sites reading
// as before.
type Unit = pr.Unit

// The units SVG accepts, aliased to the CSS engine's.
const (
	Px   = pr.Px
	Cm   = pr.Cm
	Mm   = pr.Mm
	Pt   = pr.Pt
	In   = pr.In
	Q    = pr.Q
	Pc   = pr.Pc
	Perc = pr.Perc
	Em   = pr.Em
	Rem  = pr.Rem
	Ex   = pr.Ex
	Vw   = pr.Vw
	Vh   = pr.Vh
	Vmin = pr.Vmin
	Vmax = pr.Vmax
)

// 12pt
const defaultFontSize Fl = 96 * 12 / 72

// Value is a Value expressed in a unit.
// It may be relative, meaning that context is needed
// to obtain the actual Value (see the `resolve` method)
type Value struct {
	V Fl
	U Unit
}

// look for an absolute unit, or nothing (considered as pixels)
// % is also supported.
// it returns an empty value when 's' is empty.
// Angle units (deg, rad, grad, turn) are handled by converting to
// degrees at parse time and storing the result with unit Px so that
// downstream code (which treats the numeric arg as degrees for
// rotate/skew/etc.) sees a normalized value.
// angleToDegrees converts the CSS angle units to degrees, the unit SVG
// carries angles in.
var angleToDegrees = map[pr.Unit]Fl{
	pr.Deg: 1, pr.Grad: 360.0 / 400.0, pr.Rad: 180 / Fl(math.Pi), pr.Turn: 360,
}

// parseValue parses one SVG length, percentage, number or angle.
//
// It tokenizes with the CSS parser rather than matching unit suffixes on the
// raw string. That matters for correctness, not just tidiness: suffix
// matching treated "10vmin" as "10vm" + the "in" unit, failed to parse the
// number, and made the caller drop the whole element.
//
// A value this function cannot represent is reported, never silently taken as
// zero — telling "unsupported" apart from "supported but wrong" is the point.
func parseValue(s string) (Value, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Value{}, nil
	}

	toks := pa.RemoveWhitespace(pa.Tokenize([]byte(s), true))
	if len(toks) != 1 {
		return Value{}, fmt.Errorf("expected a single value, got %q", s)
	}

	switch tok := toks[0].(type) {
	case pa.Number:
		// Unitless: user units, which are CSS pixels.
		return Value{U: Px, V: Fl(tok.ValueF)}, nil

	case pa.Percentage:
		return Value{U: Perc, V: Fl(tok.ValueF)}, nil

	case pa.Dimension:
		unit, known := validation.LengthUnit(strings.ToLower(tok.Unit))
		if !known {
			unit, known = validation.AngleUnit(strings.ToLower(tok.Unit))
			if !known {
				return Value{}, fmt.Errorf("unknown unit %q in %q", tok.Unit, s)
			}
			return Value{U: Px, V: Fl(tok.ValueF) * angleToDegrees[unit]}, nil
		}
		if !svgcss.SupportedUnits[unit] {
			return Value{}, fmt.Errorf("unit %q is not supported in SVG (in %q)", tok.Unit, s)
		}
		return Value{U: unit, V: Fl(tok.ValueF)}, nil

	default:
		return Value{}, fmt.Errorf("expected a length, got %q", s)
	}
}

// Resolve converts `v` to user units, resolving percentages against
// `percentageReference` and relative units against `dims`.
//
// rem deliberately uses the root font size rather than the element's, per
// CSS: the two differ whenever an element carries its own font-size.
func (v Value) Resolve(dims *Dims, percentageReference Fl) Fl {
	switch v.U {
	case Px, 0: // fast path for a common case
		return v.V
	case Perc:
		return v.V * percentageReference / 100
	case Em:
		return v.V * dims.FontSize
	case Rem:
		return v.V * dims.RootFontSize
	case Ex: // assume that 1em == 2ex
		return v.V * dims.FontSize / 2
	case Vw:
		return v.V * dims.ViewportW / 100
	case Vh:
		return v.V * dims.ViewportH / 100
	case Vmin:
		return v.V * min(dims.ViewportW, dims.ViewportH) / 100
	case Vmax:
		return v.V * max(dims.ViewportW, dims.ViewportH) / 100
	default:
		// Absolute units, converted with the CSS engine's own table rather
		// than a second copy of it.
		return v.V * Fl(pr.LengthToPixels(v.U))
	}
}

// returns the index of the end of the first number starting at pos
// it is assumed that data[pos] is not a whitespace
// if isFlag is true only "0" or "1" are allowed,
// meaning for instance that 12 is parsed are 1 2, not 12.
func consumeNumber(data []byte, pos int, isFlag bool) int {
	if isFlag {
		return pos + 1
	}
	start := data[pos]
	seenDot := start == '.'
	pos++
	for ; pos < len(data); pos++ {
		c := data[pos]
		switch c {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			continue
		case '.':
			if seenDot { // .5.5 is interpreted as 0.5 0.5
				return pos
			}
			// else continue: floating point
			seenDot = true
		case '-', '+':
			// '+' / '-' immediately after an exponent marker ('e'/'E') is
			// the exponent's sign — keep consuming. Otherwise it begins a
			// new number, so stop here.
			if data[pos-1] == 'e' || data[pos-1] == 'E' {
				continue
			}
			return pos
		default:
			// accept numbers and exponents
			if c == 'e' || c == 'E' {
				continue
			}
			return pos
		}
	}
	return pos
}

// parsePoints reads a set of floating point values from the SVG format number string.
// units are not supported.
// to reduce allocations, values are appended to `points`, which is supposed to have 0 length,
// and is returned
// isEllipticalArc should be true for 'A' and 'a' commands, and is required
// to handle special case in number parsing
func parsePoints(dataPoints string, points []Fl, isEllipticalArc bool) ([]Fl, error) {
	data := []byte(dataPoints)
	for pos := 0; pos < len(data); {
		c := data[pos]
		// Per WP commit 4cfd3a02: SVG number lists may use `+` as a
		// separator between two unsigned numbers (e.g. `1+2+3` is
		// three numbers). Accept it as a number-start sign.
		if '0' <= c && c <= '9' || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E' {
			// for elliptical arc, arguments 4 and 5 are flags
			// modulo the number of parameters in an elliptical arc command
			isFlag := isEllipticalArc && (len(points)%7 == 3 || len(points)%7 == 4)

			endNumber := consumeNumber(data, pos, isFlag)
			value, err := strconv.ParseFloat(dataPoints[pos:endNumber], 32)
			if err != nil {
				return nil, err
			}
			points = append(points, Fl(value))
			pos = endNumber
		} else {
			pos++ // skip "whitespaces"
		}
	}
	return points, nil
}

// parseValues reads a list of whitespace or comma-separated list of value,
// with units.
// the empty string or "none" are matched to a nil slice
func parseValues(dataPoints string) (points []Value, err error) {
	if dataPoints == "" || dataPoints == "none" {
		return nil, nil
	}

	fields := strings.FieldsFunc(dataPoints, func(r rune) bool { return r == ' ' || r == ',' })
	points = make([]Value, len(fields))
	for i, v := range fields {
		val, err := parseValue(v)
		if err != nil {
			return nil, err
		}
		points[i] = val
	}
	return points, nil
}

// parses opacity, stroke-opacity, fill-opacity attributes,
// returning 1 as a default value
func parseOpacity(value string) (Fl, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 1, nil
	}
	ratio := 1.
	if strings.HasSuffix(value, "%") {
		ratio = 100
		value = strings.TrimSpace(value[:len(value)-1])
	}
	out, err := strconv.ParseFloat(value, 32)
	return Fl(out / ratio), err
}

// if the URL is invalid, the empty string is returned
func parseURLFragment(url_ string) string {
	u, err := parseURL(url_)
	if err != nil {
		return ""
	}
	return u.Fragment
}

// parse a URL, possibly in a "url(…)" string.
func parseURL(url_ string) (*url.URL, error) {
	if strings.HasPrefix(url_, "url(") && strings.HasSuffix(url_, ")") {
		url_ = url_[4 : len(url_)-1]
		if len(url_) >= 2 {
			if (url_[0] == '"' && url_[len(url_)-1] == '"') || (url_[0] == '\'' && url_[len(url_)-1] == '\'') {
				url_ = url_[1 : len(url_)-1]
			}
		}
	}
	return url.Parse(url_)
}

func parseViewbox(attr string) (Rectangle, error) {
	points, err := parsePoints(attr, nil, false)
	if err != nil {
		return Rectangle{}, err
	}
	if len(points) != 4 {
		return Rectangle{}, fmt.Errorf("expected 4 numbers for viewbox, got %s", attr)
	}
	return Rectangle{points[0], points[1], points[2], points[3]}, nil
}

// return an empty list for empty attributes
func parseTransform(attr string) (out []transform, err error) {
	ts := strings.Split(attr, ")")
	for _, t := range ts {
		t = strings.TrimSpace(t)
		if len(t) == 0 {
			continue
		}

		d := strings.Split(t, "(")
		if len(d) != 2 || d[1] == "" {
			return nil, fmt.Errorf("invalid transformation: %s", t) // badly formed transformation
		}
		points, err := parseValues(d[1])
		if err != nil {
			return nil, fmt.Errorf("invalid transform: %s", err)
		}

		transformKind := strings.ToLower(strings.TrimSpace(d[0]))
		L := len(points)
		var tr transform
		copy(tr.args[:], points)

		switch transformKind {
		case "rotate":
			if L == 1 {
				tr.kind = rotate
			} else if L == 3 {
				tr.kind = rotateWithOrigin
			} else {
				return nil, fmt.Errorf("invalid transformation: %s", t)
			}
		case "translate":
			if L == 1 {
				tr.args[1] = Value{0, Px}
				tr.kind = translate
			} else if L == 2 {
				tr.kind = translate
			} else {
				return nil, fmt.Errorf("invalid transformation: %s", t)
			}
		case "translatex":
			if L == 1 {
				tr.args[1] = Value{0, Px}
				tr.kind = translate
			} else {
				return nil, fmt.Errorf("invalid transformation: %s", t)
			}
		case "translatey":
			if L == 1 {
				tr.args[1] = tr.args[0]
				tr.args[0] = Value{0, Px}
				tr.kind = translate
			} else {
				return nil, fmt.Errorf("invalid transformation: %s", t)
			}
		case "skew":
			// skew(thetax) is shorthand for skew(thetax, 0) — same as
			// skewX(thetax). WeasyPrint accepts both forms.
			if L == 1 || L == 2 {
				tr.kind = skew
			} else {
				return nil, fmt.Errorf("invalid transformation: %s", t)
			}
		case "skewx":
			if L == 1 {
				tr.kind = skew
			} else {
				return nil, fmt.Errorf("invalid transformation: %s", t)
			}
		case "skewy":
			if L == 1 {
				tr.kind = skew
				tr.args[1] = tr.args[0]
				tr.args[0] = Value{0, Px}
			} else {
				return nil, fmt.Errorf("invalid transformation: %s", t)
			}
		case "scale":
			if L == 1 {
				tr.args[1] = tr.args[0]
				tr.kind = scale
			} else if L == 2 {
				tr.kind = scale
			} else {
				return nil, fmt.Errorf("invalid transformation: %s", t)
			}
		case "scalex":
			if L == 1 {
				tr.args[1] = Value{1, Px}
				tr.kind = scale
			} else {
				return nil, fmt.Errorf("invalid transformation: %s", t)
			}
		case "scaley":
			if L == 1 {
				tr.args[1] = tr.args[0]
				tr.args[0] = Value{1, Px}
				tr.kind = scale
			} else {
				return nil, fmt.Errorf("invalid transformation: %s", t)
			}
		case "matrix":
			if L == 6 {
				tr.kind = customMatrix
			} else {
				return nil, fmt.Errorf("invalid transformation: %s", t)
			}
		default:
			return nil, fmt.Errorf("invalid transformation: %s", t)
		}

		out = append(out, tr)

	}

	return out, nil
}

type preserveAspectRatio struct {
	xPosition, yPosition string
	none                 bool // align == "none"
	slice                bool // meet or slice
}

func parsePreserveAspectRatio(s string) (out preserveAspectRatio) {
	out.xPosition, out.yPosition = "min", "min"
	aspectRatio := strings.Split(s, " ")
	align := aspectRatio[0]
	if align != "none" || len(align) >= 5 {
		out.xPosition = strings.ToLower(align[1:4])
		out.yPosition = strings.ToLower(align[5:])
	}
	out.none = align == "none"
	out.slice = len(aspectRatio) >= 2 && aspectRatio[1] == "slice"
	return out
}

// accepts angle or "auto" or "auto-start-reverse"
// the angle is expressed in degrees
// the empty is matched to a 0 angle
func parseOrientation(attr string) (orientation, error) {
	switch attr {
	case "":
		return orientation{}, nil
	case "auto":
		return orientation{auto: true}, nil
	case "auto-start-reverse":
		return orientation{auto: true, startReverse: true}, nil
	default:
		// an <angle>: plain number means degrees, and the deg, grad,
		// rad and turn units are allowed
		num, factor := attr, Fl(1)
		for _, unit := range [...]struct {
			suffix string
			factor Fl
		}{
			{"deg", 1},
			{"grad", 360. / 400.},
			{"rad", 180 / math.Pi},
			{"turn", 360},
		} {
			if s, ok := strings.CutSuffix(attr, unit.suffix); ok {
				num, factor = s, unit.factor
				break
			}
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(num), 32)
		return orientation{angle: Fl(f) * factor}, err
	}
}

// orientation is a <marker> element's orient attribute: an explicit angle in
// degrees, or one of the two automatic modes.
//
// It has its own type because it is not a length. It used to be carried in a
// Value with two sentinel members bolted onto the unit enum, which is what
// stopped that enum from simply being the CSS one.
type orientation struct {
	// angle in degrees, used when auto is false.
	angle Fl
	// auto follows the path direction at the vertex.
	auto bool
	// startReverse additionally flips the marker at the path start
	// (orient="auto-start-reverse"). Only meaningful with auto.
	startReverse bool
}

// ParseValue parses one SVG length, percentage, number or angle. Exported for
// element renderers outside this package.
func ParseValue(s string) (Value, error) { return parseValue(s) }

// ParseValues parses a whitespace- or comma-separated list of lengths.
func ParseValues(s string) ([]Value, error) { return parseValues(s) }

// ParsePoints parses a coordinate list, as used by rotate and points.
func ParsePoints(dataPoints string, prev []Fl, isPolyline bool) ([]Fl, error) {
	return parsePoints(dataPoints, prev, isPolyline)
}

// Point is a position in user units.
type Point = point
