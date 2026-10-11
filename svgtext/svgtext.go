// Package svgtext renders SVG <text> and <tspan> elements.
//
// It is a module of its own so that an SVG consumer needing no text — an
// icon renderer, say — carries none of the font stack. Wire it in
// explicitly:
//
//	img.Draw(dst, w, h, svgtext.New(engine))
//
// The style of a text is the one of the text engine,
// aaoo.textengine-go/layout; the engine itself is given by the host, see
// [Engine].
package svgtext

import (
	"math"
	"strconv"
	"strings"

	pr "github.com/anaelorlinski/aaoo.cssengine-go/properties"
	"github.com/anaelorlinski/aaoo.gfxcore-go"
	"github.com/anaelorlinski/aaoo.svg-go/svg"
	"github.com/anaelorlinski/aaoo.textengine-go/layout"
)

// Engine lays text out for this package. It is given by the host, because
// the text of an SVG document must be set like the rest of what the host
// draws: with the same fonts, the same fallbacks and the same metrics.
type Engine interface {
	// Shape lays text out on one line, as long as it needs to be.
	Shape(text []rune, style *layout.Style) Line
}

// Line is a text laid out on one line by an [Engine].
type Line interface {
	// Width and Height are the size of the line, in pixels.
	Width() svg.Fl
	Height() svg.Fl

	// Drawing returns the glyphs of the line, to be drawn with their
	// origin at (x, y), scaled horizontally by scaleX and rotated by angle,
	// in radians.
	Drawing(scaleX, x, y, angle svg.Fl) gfxcore.TextDrawing
}

// text tags
type textSpan struct {
	style  layout.Style
	text   string
	rotate []svg.Fl // angles in degrees

	x, y, dx, dy []svg.Value

	letterSpacing, textLength svg.Value
	lengthAdjust              bool // true for spacingAndGlyphs

	textAnchor, displayAnchor anchor

	baseline baseline

	isText          bool // only true for tag 'text'
	textBoundingBox svg.Rectangle

	// manualShift, when non-zero, is added to every character's X
	// position by textSpan.draw. svg.go's drawNode sets this on each
	// textSpan in the subtree of a <text> element with text-anchor
	// middle/end, after measuring the union width of the subtree.
	// This produces the equivalent of a post-draw Translate without
	// needing a real group buffer.
	manualShift svg.Fl

	// renderer owns the font context and the layout cursor.
	renderer *textRenderer
}

// textRenderer is the working text implementation. It holds the text engine
// so core never has to carry one.
type textRenderer struct {
	engine Engine
	// state is the layout cursor, carried across the spans of one <text>.
	state textState
}

// New returns a TextRenderer laying text out with the given engine.
// A nil engine yields a renderer that draws no text.
func New(engine Engine) svg.TextRenderer {
	return &textRenderer{engine: engine}
}

// EngineOf returns the engine of a renderer made by [New], or nil.
func EngineOf(r svg.TextRenderer) Engine {
	if r, ok := r.(*textRenderer); ok {
		return r.engine
	}
	return nil
}

func (r *textRenderer) NewSpan(txt []byte, attrs map[string]string, props pr.Properties, isTextTag bool) (svg.Drawable, error) {
	return newTextSpan(txt, attrs, props, isTextTag, r)
}

func (r *textRenderer) Begin(el *svg.Element, dst gfxcore.Canvas) (svg.TextPass, gfxcore.Canvas) {
	return r.beginAnchor(el, dst)
}

// --- text-only parsing -------------------------------------------------
//
// These live with the text renderer rather than in the svg package because
// nothing else uses them: text-anchor, dominant-baseline and font-weight are
// text concepts.

// default to 400 if invalid
func parseFontWeight(s string) int {
	switch s {
	case "normal":
		return 400
	case "bold":
		return 700
	default:
		v, err := strconv.Atoi(s)
		if err != nil {
			return 400
		}
		return v
	}
}

func parseFontStyle(s string) layout.FontStyle {
	switch strings.ToLower(s) {
	case "oblique":
		return layout.FSty_Oblique
	case "italic":
		return layout.FSty_Italic
	default:
		return layout.FSty_Normal
	}
}

type anchor uint8

const (
	start anchor = iota + 1
	middle
	end
	top
	bottom
)

// default to 0 when invalid
func parseAnchor(s string) anchor {
	switch s {
	case "start":
		return start
	case "middle":
		return middle
	case "end":
		return end
	case "top":
		return top
	case "bottom":
		return bottom
	default:
		return 0
	}
}

type baseline uint8

const (
	central baseline = iota + 1
	ascent
	descent
)

// default to 0 when invalid
func parseBaseline(s string) baseline {
	switch s {
	case "central", "middle":
		return central
	case "text-before-edge", "before_edge", "top", "hanging", "text-top":
		return ascent
	case "text-after-edge", "after_edge", "bottom", "text-bottom":
		return descent
	default:
		return 0
	}
}

// textState is the running position of the <text> layout, carried across the
// sibling spans of one <text> element. It is reset at the start of each draw.
//
// It lives here rather than on svg.SVGImage because nothing outside this file
// touches it — see docs/svg-separation.md T7b step 1.
// point is the text layout cursor's own coordinate pair. Deliberately local:
// the cursor is this package's state, so it does not borrow svg's type.
type point struct{ x, y svg.Fl }

type textState struct {
	// cursor is where the next glyph run starts.
	cursor point
	// dCursor accumulates the dx/dy deltas of <tspan>.
	dCursor point
}

func newTextSpan(txt []byte, attrs map[string]string, props pr.Properties, isTextTag bool, r *textRenderer) (svg.Drawable, error) {
	var out textSpan
	out.renderer = r

	out.text = string(txt)
	out.style = layout.DefaultStyle()

	family := "sans-serif"
	if f, has := attrs["font-family"]; has {
		family = f
	}
	// Strip CSS quoting from each comma-separated family name. Values
	// flowing through the `font` shorthand expander come back serialized
	// with quotes (e.g. `font: 2px 'weasyprint'` → font-family: `"weasyprint"`).
	familyList := strings.Split(family, ",")
	for i, f := range familyList {
		f = strings.TrimSpace(f)
		if len(f) >= 2 {
			if (f[0] == '"' && f[len(f)-1] == '"') || (f[0] == '\'' && f[len(f)-1] == '\'') {
				f = f[1 : len(f)-1]
			}
		}
		familyList[i] = f
	}
	out.style.Family = familyList

	if w, has := attrs["font-weight"]; has {
		if weight := parseFontWeight(w); weight != 0 {
			out.style.Weight = uint16(weight)
		}
	}

	if s, has := attrs["font-style"]; has {
		out.style.Style = parseFontStyle(s)
	}

	// get rotations and translations
	var err error
	out.x, err = svg.ParseValues(attrs["x"])
	if err != nil {
		return nil, err
	}
	out.y, err = svg.ParseValues(attrs["y"])
	if err != nil {
		return nil, err
	}

	out.dx, err = svg.ParseValues(attrs["dx"])
	if err != nil {
		return nil, err
	}
	out.dy, err = svg.ParseValues(attrs["dy"])
	if err != nil {
		return nil, err
	}

	out.rotate, err = svg.ParsePoints(attrs["rotate"], nil, false)
	if err != nil {
		return nil, err
	}

	out.letterSpacing, err = svg.ParseValue(attrs["letter-spacing"])
	if err != nil {
		return nil, err
	}
	out.textLength, err = svg.ParseValue(attrs["textLength"])
	if err != nil {
		return nil, err
	}

	out.textAnchor = parseAnchor(svg.KeywordOf(props, svg.PTextAnchor))
	// display-anchor is a WeasyPrint extension, not a CSS property, so it
	// stays an attribute read.
	out.displayAnchor = parseAnchor(attrs["display-anchor"])

	// dominant-baseline takes precedence whenever it is present, even set to
	// "auto"; alignment-baseline is consulted only in its absence. Both
	// resolve to "auto" when unset, so presence is read from the store.
	baseline := svg.KeywordOf(props, svg.PAlignmentBaseline)
	if _, has := props[svg.PDominantBaseline]; has {
		baseline = svg.KeywordOf(props, svg.PDominantBaseline)
	}
	out.baseline = parseBaseline(baseline)

	out.lengthAdjust = attrs["lengthAdjust"] == "spacingAndGlyphs"

	out.isText = isTextTag
	out.textBoundingBox = svg.EmptyBbox

	return &out, nil
}

// returns the text bounding box
func (t *textSpan) BoundingBox(*svg.Element) (svg.Rectangle, bool) {
	return t.textBoundingBox, t.textBoundingBox != svg.EmptyBbox
}

func (t *textSpan) Draw(dst gfxcore.Canvas, img *svg.SVGImage, el *svg.Element) []svg.Vertex {
	engine := t.renderer.engine
	if engine == nil {
		return nil
	}
	dims := *el.Dims()
	t.style.Size = dims.FontSize

	splitted := engine.Shape([]rune(t.text), &t.style)
	width, height := splitted.Width(), splitted.Height()

	// Get rotations and translations
	var xs, ys, dxs, dys []svg.Fl
	for _, v := range t.x {
		xs = append(xs, v.Resolve(&dims, dims.ViewportW))
	}
	for _, v := range t.y {
		ys = append(ys, v.Resolve(&dims, dims.ViewportH))
	}
	for _, v := range t.dx {
		dxs = append(dxs, v.Resolve(&dims, dims.ViewportW))
	}
	for _, v := range t.dy {
		dys = append(dys, v.Resolve(&dims, dims.ViewportH))
	}

	var yAlign svg.Fl

	letterSpacing := dims.Length(t.letterSpacing)
	textLength := dims.Length(t.textLength)
	scaleX := svg.Fl(1.)
	if textLength != 0 && t.text != "" {
		// calculate the number of spaces to be considered for the text
		spacesCount := svg.Fl(len(t.text) - 1)
		if t.lengthAdjust {
			// scale letterSpacing up/down to textLength
			widthWithSpacing := width + spacesCount*letterSpacing
			letterSpacing *= textLength / widthWithSpacing
			// calculate the glyphs scaling factor by:
			// - deducting the scaled letterSpacing from textLength
			// - dividing the calculated value by the original width
			spacelessTextLength := textLength - spacesCount*letterSpacing
			scaleX = spacelessTextLength / width
		} else if spacesCount != 0 {
			// adjust letter spacing to fit textLength
			letterSpacing = (textLength - width) / spacesCount
		}
		width = textLength
	}
	ascentL, descentL := dims.FontSize*.8, dims.FontSize*.2

	// align text box vertically
	if t.displayAnchor == middle {
		yAlign = -height / 2
	} else if t.displayAnchor == top {
		// pass
	} else if t.displayAnchor == bottom {
		yAlign = -height
	} else if t.baseline == central {
		yAlign = (ascentL+descentL)/2 - descentL
	} else if t.baseline == ascent {
		yAlign = ascentL
	} else if t.baseline == descent {
		yAlign = -descentL
	}

	// return early when there’s no text,
	// update the cursor position though
	if t.text == "" {
		x0 := t.renderer.state.cursor.x
		if len(xs) != 0 {
			x0 = xs[0]
		}
		y0 := t.renderer.state.cursor.y
		if len(ys) != 0 {
			y0 = ys[0]
		}
		var dx0, dy0 svg.Fl
		if len(dxs) != 0 {
			dx0 = dxs[0]
		}
		if len(dys) != 0 {
			dy0 = dys[0]
		}
		t.renderer.state.cursor = point{x0 + dx0, y0 + dy0}
		return nil
	}

	// Draw letters
	chars := []rune(t.text)
	var (
		bbox  svg.Rectangle
		texts []gfxcore.TextDrawing
	)
	for i, r := range chars {
		hasX, hasY := i < len(xs), i < len(ys)

		var angle svg.Fl // en radians
		if i < len(t.rotate) {
			angle = t.rotate[i] * math.Pi / 180
		} else if L := len(t.rotate); L != 0 {
			angle = t.rotate[L-1] * math.Pi / 180
		}

		if hasX && xs[i] != 0 { // x specified
			t.renderer.state.dCursor.x = 0
		}
		if hasY && ys[i] != 0 { // y specified
			t.renderer.state.dCursor.y = 0
		}
		if i < len(dxs) {
			t.renderer.state.dCursor.x += dxs[i]
		}
		if i < len(dys) {
			t.renderer.state.dCursor.y += dys[i]
		}

		line := engine.Shape([]rune{r}, &t.style)
		width, height = line.Width(), line.Height()

		x, y := t.renderer.state.cursor.x, t.renderer.state.cursor.y
		if hasX {
			x = xs[i]
		}
		if hasY {
			y = ys[i]
		}

		width *= scaleX
		if i != 0 {
			x += letterSpacing
		}
		t.renderer.state.cursor = point{x + width, y}

		xPosition := x + t.renderer.state.dCursor.x
		yPosition := y + t.renderer.state.dCursor.y + yAlign

		pointsBb := svg.Rectangle{
			X: xPosition, Y: yPosition,
			Width: width, Height: -height,
		}
		if i == 0 {
			bbox = pointsBb
		} else {
			bbox.Union(pointsBb)
		}

		doFill, doStroke := img.ApplyPainters(dst, el, t)
		dst.State().SetTextPaint(svg.NewPaintOp(doFill, doStroke, false))
		// do not draw empty content (upstream fc95106)
		if drawing := line.Drawing(scaleX, xPosition, yPosition, angle); len(drawing.Runs) != 0 || len(drawing.Text) != 0 {
			texts = append(texts, drawing)
		}
	}

	// Apply text-anchor shift. Three cases:
	//   1. svg.go pre-computed a shift across the <text> subtree
	//      (covers <text text-anchor="..."> with or without nested
	//      tspans, where the outer node's bbox is unknown when this
	//      runs because children haven't drawn yet).
	//   2. This is a <tspan> with its own text-anchor (e.g.
	//      <text><tspan x="10" text-anchor="middle">...). The tspan's
	//      bbox is local; we use its own width.
	//   3. Otherwise (no shift), draw at unshifted positions.
	shift := t.manualShift
	if shift == 0 && (t.textAnchor == middle || t.textAnchor == end) && !t.isText {
		// Case 2: tspan with its own anchor and no override from parent.
		if t.textAnchor == middle {
			shift = -bbox.Width / 2
		} else {
			shift = -bbox.Width
		}
	}
	if shift != 0 {
		for i := range texts {
			texts[i].X += shift
		}
		bbox.X += shift
	}

	dst.OnNewStack(func() {
		dst.DrawText(texts)
	})

	t.textBoundingBox = bbox

	return nil
}

// absorbChild grows the <text> element's bounding box to cover a drawn
// child span, so endTextAnchor can size the group. A no-op unless this pass
// is anchoring a <text> element.
func (p textPass) AbsorbChild(child *svg.Element) {
	if !p.isText || !child.Visible() {
		return
	}
	childText, ok := child.Content().(*textSpan)
	if !ok {
		return
	}
	if bb := childText.textBoundingBox; bb != svg.EmptyBbox {
		p.span.textBoundingBox.Union(bb)
	}
}

// --- text anchoring -----------------------------------------------------
//
// drawNode brackets each element with beginTextAnchor/endTextAnchor rather
// than inspecting text itself: the anchor logic, the group buffering it needs
// and the subtree measurement all belong to the text renderer. Keeping them
// here is what lets drawNode stay free of *textSpan (T7b).

// textPass carries the state beginTextAnchor hands to endTextAnchor.
type textPass struct {
	span *textSpan
	// isText is true for a <text> element, as opposed to a <tspan> or a
	// non-text element; drawNode consults it to skip the ordinary paint.
	isText bool
	// anchor is the effective text-anchor, possibly taken from an only child.
	anchor anchor
	// original is the canvas to restore when the group is popped; nil when
	// no group was opened.
	original gfxcore.Canvas
	// dims is kept so End can size the group without core passing it back.
	dims svg.Dims
}

func (p textPass) IsText() bool { return p.isText }

// beginTextAnchor opens a group for middle/end anchored text and pre-computes
// the shift applied to every descendant span. It returns the canvas drawing
// should continue on.
func (r *textRenderer) beginAnchor(el *svg.Element, dst gfxcore.Canvas) (svg.TextPass, gfxcore.Canvas) {
	span, isText := el.Content().(*textSpan)
	if !isText || !span.isText {
		return textPass{span: span, isText: isText && span.isText}, dst
	}

	children := el.Children()
	pass := textPass{span: span, isText: true, anchor: span.textAnchor, dims: *el.Dims()}
	if len(children) != 0 && span.text == "" {
		if child, ok := children[0].Content().(*textSpan); ok {
			pass.anchor = child.textAnchor
		}
	}

	if pass.anchor == middle || pass.anchor == end {
		pass.original = dst
		dst = dst.NewGroup(0, 0, 0, 0) // BBox set after drawing
	}

	// Pre-compute the anchor shift from the union width of this <text>
	// node's content (own text + recursively all child <tspan>s) and
	// propagate it to every descendant textSpan so each draw call positions
	// glyphs at the already-shifted x coordinates. This avoids the upstream
	// pattern (NewGroup buffer + post-draw Translate) which requires a real
	// group buffer not yet implemented here.
	//
	// Only propagate when the OUTER <text> itself has an anchor — if the
	// anchor came from a single child, that child handles its own shift in
	// textSpan.draw via `t.textAnchor`, and propagating would double-apply.
	if (span.textAnchor == middle || span.textAnchor == end) && span.text != "" {
		totalWidth := measureTextSubtreeWidth(el)
		shift := -totalWidth
		if span.textAnchor == middle {
			shift = -totalWidth / 2
		}
		propagateAnchorShift(el, shift)
	}
	return pass, dst
}

// endTextAnchor pops the group opened by beginTextAnchor, if any, and paints
// it at the anchored position. It returns the canvas to continue on.
func (p textPass) End(dst gfxcore.Canvas) gfxcore.Canvas {
	pass, dims := p, p.dims
	if pass.original == nil {
		return dst
	}
	group := dst
	dst = pass.original

	dst.OnNewStack(func() {
		if bbox := pass.span.textBoundingBox; bbox != svg.EmptyBbox {
			x, y, width, height := bbox.X, bbox.Y, bbox.Width, bbox.Height
			// Add extra space to include ink extents
			group.SetBoundingBox(x-dims.FontSize, y-dims.FontSize, x+width+dims.FontSize, y+height+dims.FontSize)
		}
		// The group is put where it was drawn: the spans were drawn at
		// their anchored places, see beginAnchor. Moving the canvas here
		// would move the group, and anchor the text twice.
		dst.DrawGroup(group, gfxcore.Composite{})
	})
	return dst
}

// measureTextSubtreeWidth returns the total CSS-pixel width of all
// glyphs that will be drawn for the given <text>/<tspan> subtree at
// the resolved fontSize. Used by drawNode to compute a single anchor
// shift that gets applied to every textSpan in the subtree.
func measureTextSubtreeWidth(el *svg.Element) svg.Fl {
	var width svg.Fl
	if t, ok := el.Content().(*textSpan); ok && t.text != "" {
		fontSize := el.FontSize()
		// Approximate width: chars × fontSize. Works exactly for
		// monospace test fonts (weasyprint.otf, AHEM); for proportional
		// fonts we'd need to query the actual face. Future improvement.
		width += svg.Fl(len([]rune(t.text))) * fontSize
	}
	for _, child := range el.Children() {
		width += measureTextSubtreeWidth(child)
	}
	return width
}

// propagateAnchorShift sets manualShift on every textSpan in the
// subtree so each child's draw call positions glyphs at the
// anchor-adjusted x coordinates.
func propagateAnchorShift(el *svg.Element, shift svg.Fl) {
	if t, ok := el.Content().(*textSpan); ok {
		t.manualShift = shift
	}
	for _, child := range el.Children() {
		propagateAnchorShift(child, shift)
	}
}
