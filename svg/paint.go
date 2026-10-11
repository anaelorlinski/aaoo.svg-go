package svg

import (
	"fmt"
	"strings"

	"github.com/anaelorlinski/aaoo.cssengine-go/parser"
	"github.com/anaelorlinski/aaoo.gfxcore-go"
	"github.com/anaelorlinski/aaoo.gfxcore-go/matrix"
	"github.com/anaelorlinski/aaoo.svg-go/internal/limits"
	"github.com/anaelorlinski/aaoo.svg-go/internal/utils"
	"github.com/anaelorlinski/aaoo.svg-go/logger"
)

// handle painter for fill and stroke values

// painter is either a simple RGBA color,
// or a reference to a more complex `paintServer`
type painter struct {
	// value of the url attribute, refering
	// to a paintServer element
	refID string

	color parser.RGBA

	// if 'false', no painting occurs (not the same as black)
	valid bool
}

// parse a fill or stroke attribute
func newPainter(attr string) (painter, error) {
	attr = strings.TrimSpace(attr)
	if attr == "" || attr == "none" {
		return painter{}, nil
	}

	var out painter
	if strings.HasPrefix(attr, "url(") {
		if i := strings.IndexByte(attr, ')'); i != -1 {
			out.refID = parseURLFragment(attr[:i])
			attr = attr[i+1:] // skip the )
		} else {
			return out, fmt.Errorf("invalid url in color '%s'", attr)
		}
	}

	color := parser.ParseColorString(attr)
	// currentColor has been resolved during tree building
	out.color = color.RGBA
	out.valid = true

	return out, nil
}

// ensure that v is positive and equal to offset modulo total
func clampModulo(offset, total Fl) Fl {
	if offset < 0 { // shift to [0, dashesLength]
		offset = -offset
		quotient := utils.Floor(offset / total)
		remainder := offset - quotient*total
		return total - remainder
	}
	return offset
}

func (dims drawingDims) resolveDashes(dashArray []Value, dashOffset Value) ([]Fl, Fl) {
	dashes := make([]Fl, len(dashArray))
	var dashesLength Fl
	for i, v := range dashArray {
		dashes[i] = dims.length(v)
		if dashes[i] < 0 {
			return nil, 0
		}
		dashesLength += dashes[i]
	}
	if dashesLength == 0 {
		return nil, 0
	}
	offset := dims.length(dashOffset)
	offset = clampModulo(offset, dashesLength)
	return dashes, offset
}

// apply fill and stroke painters
func (svg *SVGImage) applyPainters(dst gfxcore.Canvas, node *svgNode, dims drawingDims) (doFill, doStroke bool) {
	strokeWidth := dims.length(node.strokeWidth)
	doFill = node.fill.valid
	doStroke = node.stroke.valid && strokeWidth > 0

	// fill
	if doFill {
		svg.applyPainter(dst, node, node.fill, node.fillOpacity, dims, false)
	}

	// stroke
	if doStroke {
		svg.applyPainter(dst, node, node.stroke, node.strokeOpacity, dims, true)

		// stroke options
		dashes, offset := dims.resolveDashes(node.dashArray, node.dashOffset)
		dst.State().SetDash(dashes, offset)

		dst.State().SetLineWidth(strokeWidth)
		dst.State().SetStrokeOptions(node.strokeOptions)
	}

	return
}

// ApplyPainters resolves the fill and stroke of an element rendered by an
// external [Drawable] and reports whether each should be painted. `content`
// is the renderer itself, which the painters may need for pattern and
// gradient bounding boxes.
//
// It is the callback the text renderer needs; without it an out-of-package
// element could draw geometry but never paint it.
func (svg *SVGImage) ApplyPainters(dst gfxcore.Canvas, el *Element, content Drawable) (doFill, doStroke bool) {
	return svg.applyPainters(dst, &svgNode{
		graphicContent: external{content},
		attributes:     *el.attrs,
	}, el.dims)
}

// NewPaintOp builds the paint operation for the given fill and stroke, with
// the even-odd or non-zero fill rule.
func NewPaintOp(fill, stroke, evenOdd bool) gfxcore.PaintOp { return newPaintOp(fill, stroke, evenOdd) }

func newPaintOp(fill, stroke, evenOdd bool) gfxcore.PaintOp {
	var op gfxcore.PaintOp
	if fill {
		if evenOdd {
			op |= gfxcore.FillEvenOdd
		} else {
			op |= gfxcore.FillNonZero
		}
	}
	if stroke {
		op |= gfxcore.Stroke
	}
	return op
}

// apply the given painter to the given node, outputing the
// the result in `dst`
// opacity is an additional opacity factor
func (svg *SVGImage) applyPainter(dst gfxcore.Canvas, node *svgNode, pt painter, opacity Fl, dims drawingDims, stroke bool) {
	if !pt.valid {
		return
	}

	// check for a paintServer
	if ps := svg.definitions.paintServers[pt.refID]; ps != nil {
		wasPainted := ps.paint(dst, node, opacity, dims, svg, stroke)
		if wasPainted {
			return
		} // else default to a plain color
	}

	pt.color.A *= opacity // apply the opacity factor
	dst.State().SetColorRgba(pt.color, stroke)
}

// gradient or pattern
type paintServer interface {
	// setup the "color" for the given `node`
	paint(dst gfxcore.Canvas, node *svgNode, opacity Fl, dims drawingDims, svg *SVGImage, stroke bool) bool
}

// either linear or radial
type gradientKind interface {
	isGradient()
}

func (gradientLinear) isGradient() {}
func (gradientRadial) isGradient() {}

type gradientLinear struct {
	x1, y1, x2, y2 Value
}

func newGradientLinear(node *cascadedNode) (out gradientLinear, err error) {
	out.x1, err = parseValue(node.attrs["x1"])
	if err != nil {
		return out, err
	}
	out.y1, err = parseValue(node.attrs["y1"])
	if err != nil {
		return out, err
	}
	out.x2, err = parseValue(node.attrs["x2"])
	if err != nil {
		return out, err
	}
	out.y2, err = parseValue(node.attrs["y2"])
	if err != nil {
		return out, err
	}

	// default values
	if out.x2.U == 0 {
		out.x2 = Value{100, Perc} // 100%
	}

	return out, nil
}

type gradientRadial struct {
	// if fx or fy are empty, they should default to
	// the resolved values of cx and cy
	cx, cy, r, fx, fy, fr Value
}

func newGradientRadial(node *cascadedNode) (out gradientRadial, err error) {
	cx, cy, r := node.attrs["cx"], node.attrs["cy"], node.attrs["r"]
	if cx == "" {
		cx = "50%"
	}
	if cy == "" {
		cy = "50%"
	}
	if r == "" {
		r = "50%"
	}
	fx, fy, fr := node.attrs["fx"], node.attrs["fy"], node.attrs["fr"]

	out.cx, err = parseValue(cx)
	if err != nil {
		return out, err
	}
	out.cy, err = parseValue(cy)
	if err != nil {
		return out, err
	}
	out.r, err = parseValue(r)
	if err != nil {
		return out, err
	}
	out.fx, err = parseValue(fx)
	if err != nil {
		return out, err
	}
	out.fy, err = parseValue(fy)
	if err != nil {
		return out, err
	}
	out.fr, err = parseValue(fr)
	if err != nil {
		return out, err
	}

	return out, nil
}

// gradient specification, prior to resolving units
type gradient struct {
	kind gradientKind

	spreadMethod GradientSpread // default to NoRepeat

	positions []Value
	colors    []parser.RGBA

	transforms []transform

	isUnitsUserSpace bool
}

// parse a linearGradient or radialGradient node
func newGradient(node *cascadedNode) (out gradient, err error) {
	out.positions = make([]Value, len(node.children))
	out.colors = make([]parser.RGBA, len(node.children))
	for i, child := range node.children {
		out.positions[i], err = parseValue(child.attrs["offset"])
		if err != nil {
			return out, err
		}

		// stop-color and stop-opacity come from the validated store: the
		// colour was parsed once by the CSS engine, and its initial value
		// lives with the property rather than being repeated here.
		stopColor := parser.RGBA{}
		if c, ok := colorOf(child.props, PStopColor); ok {
			stopColor = c.RGBA
		} else {
			stopColor = parser.ParseColorString(initialString(PStopColor)).RGBA
		}
		stopColor.A = utils.Fl(floatOf(child.props, PStopOpacity))

		out.colors[i] = stopColor
	}

	out.isUnitsUserSpace = node.attrs["gradientUnits"] == "userSpaceOnUse"
	switch node.attrs["spreadMethod"] {
	case "repeat":
		out.spreadMethod = Repeat
	case "reflect":
		out.spreadMethod = Reflect
		// default NoRepeat
	}

	out.transforms, err = parseTransform(node.attrs["gradientTransform"])
	if err != nil {
		return out, err
	}

	switch node.tag {
	case "linearGradient":
		out.kind, err = newGradientLinear(node)
		if err != nil {
			return out, fmt.Errorf("invalid linear gradient: %s", err)
		}
	case "radialGradient":
		out.kind, err = newGradientRadial(node)
		if err != nil {
			return out, fmt.Errorf("invalid radial gradient: %s", err)
		}
	default:
		panic("unexpected node tag " + node.tag)
	}

	return out, nil
}

func (gr gradient) paint(dst gfxcore.Canvas, node *svgNode, opacity Fl, dims drawingDims, _ *SVGImage, stroke bool) bool {
	if len(gr.colors) == 0 {
		return false
	}

	if len(gr.colors) == 1 { // actually solid
		color := gr.colors[0]
		color.A *= opacity
		dst.State().SetColorRgba(color, stroke)
		return true
	}

	// The box the units of the gradient are fractions of: the one of the
	// shape, without its stroke. A shape with no width or no height, like
	// a horizontal line, has none, and such a gradient does not paint it.
	bbox, ok := node.maskBox(dims)
	if !gr.isUnitsUserSpace && !(ok && bbox.Width > 0 && bbox.Height > 0) {
		return false
	}

	x, y := bbox.X, bbox.Y
	width, height := bbox.Width, bbox.Height
	if gr.isUnitsUserSpace {
		width, height = dims.ViewportW, dims.ViewportH
	}

	// resolve positions values
	positions := make([]Fl, len(gr.positions))
	var previousPos Fl
	for i, p := range gr.positions {
		pos := p.Resolve(&dims.Dims, 1)
		positions[i] = max(pos, previousPos) // ensure positions is increasing
		previousPos = pos
	}
	colors := append([]parser.RGBA(nil), gr.colors...)
	// the opacity of the fill, or of the stroke, is the one of every stop
	for i := range colors {
		colors[i].A *= opacity
	}

	switch gr.spreadMethod {
	case Repeat, Reflect:
		if positions[0] > 0 {
			positions = append([]Fl{0}, positions...)
			colors = append([]parser.RGBA{colors[0]}, colors...)
		}
		if positions[len(positions)-1] < 1 {
			positions = append(positions, 1)
			colors = append(colors, colors[len(colors)-1])
		}
	default:
		// Add explicit colors at boundaries if needed, because PDF doesn’t
		// extend color stops that are not displayed
		if positions[0] == positions[1] {
			if _, isRadial := gr.kind.(gradientRadial); isRadial {
				// avoid negative radius for radial gradients
				positions = append([]Fl{0}, positions...)
			} else {
				positions = append([]Fl{positions[0] - 1}, positions...)
			}
			colors = append([]parser.RGBA{colors[0]}, colors...)
		}
		if L := len(positions); positions[L-2] == positions[L-1] {
			positions = append(positions, positions[L-1]+1)
			colors = append(colors, colors[len(colors)-1])
		}
	}

	var laidOutGradient gfxcore.GradientLayout
	// length is the bbox shorter axis used by the objectBoundingBox path
	// to pre-scale gradient endpoints into "min-axis pixel" space (so a
	// single Scaling matrix can fix the longer axis afterward). It is
	// also used to scale gradientTransform's translate components from
	// unit space to pre-scaled space when objectBoundingBox is in use.
	length := min(width, height)
	mt := matrix.Translation(x, y)
	switch kind := gr.kind.(type) {
	case gradientLinear:
		x1 := kind.x1.Resolve(&dims.Dims, 1)
		y1 := kind.y1.Resolve(&dims.Dims, 1)
		x2 := kind.x2.Resolve(&dims.Dims, 1)
		y2 := kind.y2.Resolve(&dims.Dims, 1)
		if gr.isUnitsUserSpace {
			x1 -= x
			y1 -= y
			x2 -= x
			y2 -= y
		} else {
			length := min(width, height)
			x1 *= length
			y1 *= length
			x2 *= length
			y2 *= length

			// update the transformation matrix
			var a, d Fl = 1, 1
			if height < width {
				a = width / height
			} else {
				d = height / width
			}
			mt.LeftMultBy(matrix.Scaling(a, d))
		}
		dx, dy := x2-x1, y2-y1
		vectorLength := utils.Hypot(dx, dy)
		laidOutGradient = gr.spreadMethod.LinearGradient(positions, colors, x1, y1, dx, dy, vectorLength)
	case gradientRadial:
		cx := kind.cx.Resolve(&dims.Dims, 1)
		cy := kind.cy.Resolve(&dims.Dims, 1)
		r := kind.r.Resolve(&dims.Dims, 1)

		fx, fy := cx, cy
		if kind.fx.U != 0 {
			fx = kind.fx.Resolve(&dims.Dims, width)
		}
		if kind.fy.U != 0 {
			fy = kind.fy.Resolve(&dims.Dims, height)
		}
		fr := kind.fr.Resolve(&dims.Dims, 1)
		if gr.isUnitsUserSpace {
			cx -= x
			cy -= y
			fx -= x
			fy -= y
		} else {
			length := min(width, height)
			cx *= length
			cy *= length
			r *= length
			fx *= length
			fy *= length
			fr *= length

			// update the transformation matrix
			var a, d Fl = 1, 1
			if height < width {
				a = width / height
			} else {
				d = height / width
			}
			mt.RightMultBy(matrix.Scaling(a, d))
		}

		laidOutGradient = gr.spreadMethod.RadialGradient(positions, colors, fx, fy, fr, cx, cy, r, width, height)
	}

	laidOutGradient.Repeating = gr.spreadMethod != NoRepeat

	if trs := gr.transforms; len(trs) != 0 {
		mat := aggregateTransforms(trs, &dims, dims.normalizedDiagonal)
		if mat.Determinant() == 0 {
			// a non-invertible gradientTransform makes the gradient
			// invalid: nothing is painted (matching resvg)
			logger.Warn("ignoring gradient with singular gradientTransform")
			return false
		}
		// gradientTransform applies in the gradient's own coordinate
		// system, before bbox/userspace mapping. Compose it on the
		// right (= apply first when reading column-vector style).
		// For objectBoundingBox the spec says gradientTransform is in
		// unit (0..1) space; our endpoints have been pre-scaled by
		// `length`, so scale the translate part to match before
		// composing — otherwise `translate(0, 0.5)` would shift by
		// half a pixel rather than half a bbox.
		if !gr.isUnitsUserSpace {
			mat.E *= length
			mat.F *= length
		}
		mt.RightMultBy(mat)
	}

	if laidOutGradient.Kind == "solid" {
		dst.State().SetColorRgba(laidOutGradient.Colors[0], stroke)
		return true
	}

	dst.State().SetColorGradient(laidOutGradient, mt, stroke)
	return true
}

// pattern is a container for shapes to be used as
// fill or stroke color
type pattern struct {
	svgNode

	isUnitsUserSpace   bool // patternUnits
	isContentUnitsBBox bool // patternContentUnits
	// transforms is the patternTransform: it applies to the tiling as a
	// whole, tiles and content.
	transforms []transform
	// preserveRatio fits the viewBox of the pattern in a tile.
	preserveRatio preserveAspectRatio
}

func newPattern(node *cascadedNode, children []*svgNode) (out pattern, err error) {
	out.children = children
	out.isUnitsUserSpace = node.attrs["patternUnits"] == "userSpaceOnUse"
	out.isContentUnitsBBox = node.attrs["patternContentUnits"] == "objectBoundingBox"
	out.preserveRatio = node.attrs.aspectRatio()

	err = node.attrs.parseCommonAttributes(node.props, &out.attributes)
	if err != nil {
		return out, fmt.Errorf("parsing pattern element: %s", err)
	}
	// a pattern has no place of its own: what moves it is patternTransform
	out.attributes.transforms = nil
	out.transforms, err = parseTransform(node.attrs["patternTransform"])
	if err != nil {
		return out, fmt.Errorf("parsing pattern element: %s", err)
	}

	return out, nil
}

func (pt pattern) paint(dst gfxcore.Canvas, node *svgNode, opacity Fl, dims drawingDims, svg *SVGImage, stroke bool) bool {
	if opacity <= 0 {
		// nothing shows
		dst.State().SetColorRgba(parser.RGBA{}, stroke)
		return true
	}
	// the box the units of the pattern are fractions of: the one of the
	// shape, without its stroke
	bbox, hasBBox := node.maskBox(dims)
	contentInBBox := pt.isContentUnitsBBox && pt.viewbox == nil
	if (!pt.isUnitsUserSpace || contentInBBox) && !(hasBBox && bbox.Width > 0 && bbox.Height > 0) {
		return false
	}

	// the first tile, in the coordinates of the element
	var x, y, patternWidth, patternHeight Fl
	if pt.isUnitsUserSpace {
		x = pt.x.Resolve(&dims.Dims, dims.ViewportW)
		y = pt.y.Resolve(&dims.Dims, dims.ViewportH)
		patternWidth = pt.width.Resolve(&dims.Dims, dims.ViewportW)
		patternHeight = pt.height.Resolve(&dims.Dims, dims.ViewportH)
	} else {
		x = bbox.X + fraction(pt.x)*bbox.Width
		y = bbox.Y + fraction(pt.y)*bbox.Height
		patternWidth = fraction(pt.width) * bbox.Width
		patternHeight = fraction(pt.height) * bbox.Height
	}

	if !(patternWidth > 0 && patternHeight > 0) {
		// a degenerate tile cannot be rasterized; the pattern paints
		// nothing
		if patternWidth != 0 && patternHeight != 0 {
			logger.Warn("ignoring pattern with zero-sized tile")
		}
		return false
	}

	// From a tile to the element: the tile is put at (x, y), then the
	// whole tiling is moved by patternTransform.
	mat := aggregateTransforms(pt.transforms, &dims, dims.diagonal)
	if mat.Determinant() == 0 {
		// a transform that cannot be inverted places nothing: the tiling
		// is left where it is, as resvg does
		mat = matrix.Identity()
	}
	mat.RightMultBy(matrix.Translation(x, y))

	// a tile costs a group and a deferred tile: more than a plain draw
	if svg.budgetExhausted() {
		return false
	}
	svg.drawBudget -= limits.PatternTileDraws

	// draw the pattern content on the temporary target
	pat := dst.NewPatternTile(patternWidth, patternHeight)
	// with the definitions, the reference guard and the draw budget of the
	// image: a pattern may use gradients, clip paths, other patterns. An
	// empty pattern paints nothing (and cannot recurse).
	if len(pt.svgNode.children) != 0 {
		key := fmt.Sprintf("pattern:%p", pt.svgNode.children)
		if !svg.enterRef(key, "pattern") {
			return false
		}
		svg.materialiseText(&pt.svgNode)
		pat.OnNewStack(func() {
			// The content is in the coordinates of the tile, unless the
			// pattern has a viewBox, fitted in the tile, or asks for the
			// ones of the bounding box.
			// the size of the tile, in the units of its content
			width, height := patternWidth, patternHeight
			switch {
			case pt.viewbox != nil:
				sx, sy, tx, ty := pt.preserveRatio.resolveTransforms(patternWidth, patternHeight, pt.viewbox, nil)
				pat.State().Transform(matrix.New(sx, 0, 0, sy, tx, ty))
			case pt.isContentUnitsBBox:
				pat.State().Transform(matrix.Scaling(bbox.Width, bbox.Height))
				width, height = patternWidth/bbox.Width, patternHeight/bbox.Height
			}
			content := pt.svgNode
			content.x, content.y, content.width, content.height = Value{}, Value{}, Value{}, Value{}
			svg.drawRoot(pat, &content, width, height)
		})
		svg.leaveRef(key)
	}

	// apply the pattern
	dst.State().SetColorPattern(gfxcore.Pattern{
		Tile:  pat,
		Width: patternWidth, Height: patternHeight,
		Transform: mat,
		Opacity:   opacity,
	}, stroke)

	return true
}
