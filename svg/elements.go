package svg

import (
	"fmt"
	"math"
	"slices"
	"strings"

	pr "github.com/anaelorlinski/aaoo.cssengine-go/properties"
	"github.com/anaelorlinski/aaoo.gfxcore-go"
	"github.com/anaelorlinski/aaoo.gfxcore-go/matrix"
	"github.com/anaelorlinski/aaoo.svg-go/internal/limits"
	"github.com/anaelorlinski/aaoo.svg-go/internal/utils"
	"github.com/anaelorlinski/aaoo.svg-go/logger"
)

// Element is an opaque handle to the element being drawn: the resolved
// attributes and the drawing state a [Drawable] is invoked with.
//
// External renderers cannot read it — they resolve geometry through [Dims]
// and hand the element back to [SVGImage.ApplyPainters] to have fill and
// stroke applied. Keeping it opaque is what lets the 23-field attribute
// struct stay unexported.
type Element struct {
	node  *svgNode
	attrs *attributes
	dims  drawingDims
}

// Dims returns the context lengths on this element resolve against.
func (e *Element) Dims() *Dims { return &e.dims.Dims }

// Children returns the element's children, sharing its drawing context.
// Renderers that measure a subtree — text anchoring, for one — walk these.
func (e *Element) Children() []*Element {
	if e.node == nil {
		return nil
	}
	out := make([]*Element, len(e.node.children))
	for i, c := range e.node.children {
		out[i] = &Element{node: c, attrs: &c.attributes, dims: e.dims}
	}
	return out
}

// Content returns the renderer that produced this element, unwrapped, so an
// external renderer can recover its own type from a child. Returns nil when
// the element was rendered by this package.
func (e *Element) Content() Drawable {
	if e.node == nil {
		return nil
	}
	switch c := e.node.graphicContent.(type) {
	case external:
		return c.Drawable
	case *pendingText:
		if ext, ok := c.built.(external); ok {
			return ext.Drawable
		}
	}
	return nil
}

// FontSize resolves the element's own font-size attribute, which a renderer
// measuring a subtree needs per child rather than the inherited value.
func (e *Element) FontSize() Fl {
	return e.attrs.fontSize.Resolve(&e.dims.Dims, e.dims.FontSize)
}

// Visible reports whether the element is drawn at all.
func (e *Element) Visible() bool { return e.node != nil && e.node.visible }

// Drawable renders one SVG element. It is exported so element renderers can
// live outside this package, while the
// shapes implemented here keep using the unexported form.
type Drawable interface {
	// Draw paints the element onto dst. It returns the vertices of the
	// path for path, line, polyline and polygon elements, or nil.
	Draw(dst gfxcore.Canvas, img *SVGImage, el *Element) []Vertex

	// BoundingBox reports the element's bounds, or false if it has none —
	// an empty path, for instance.
	BoundingBox(el *Element) (Rectangle, bool)
}

// drawable is the internal form, which additionally carries the resolved
// attributes and the full drawing dimensions that this package's own shapes
// need. External implementations are adapted to it by [external].
type drawable interface {
	// Draws the node onto `dst` with the given dimensions.
	// It should return the vertices of the path for path, line, polyline and polygon elements
	// or nil
	draw(dst gfxcore.Canvas, attrs *attributes, img *SVGImage, dims drawingDims) []vertex

	// computes the bounding box of the node, or returns false
	// if the node has no valid bounding box, like empty paths.
	boundingBox(attrs *attributes, dims drawingDims) (Rectangle, bool)
}

// external adapts a [Drawable] implemented outside this package to the
// internal interface, dropping the state such an implementation cannot see.
type external struct{ Drawable }

func (e external) draw(dst gfxcore.Canvas, attrs *attributes, img *SVGImage, dims drawingDims) []vertex {
	return e.Drawable.Draw(dst, img, &Element{attrs: attrs, dims: dims})
}

func (e external) boundingBox(attrs *attributes, dims drawingDims) (Rectangle, bool) {
	return e.Drawable.BoundingBox(&Element{attrs: attrs, dims: dims})
}

// TextRenderer draws <text> elements. Core holds one but never names the text
// package: the default does nothing, and a real implementation is supplied by
// the caller.
type TextRenderer interface {
	// NewSpan builds the renderer for one <text> or <tspan> element.
	//
	// props holds the element's cascaded, validated CSS properties (the SVG
	// vocabulary registered by this package); attrs holds the raw XML
	// attributes that are not properties — x, y, dx, dy, rotate, textLength.
	NewSpan(text []byte, attrs map[string]string, props pr.Properties, isTextTag bool) (Drawable, error)

	// Begin is called before an element is drawn. It may swap the canvas —
	// anchored text buffers into a group — and returns the pass that
	// brackets the draw.
	Begin(el *Element, dst gfxcore.Canvas) (TextPass, gfxcore.Canvas)
}

// TextPass is the state a [TextRenderer] carries across one element's draw.
type TextPass interface {
	// IsText reports a <text> element, which core does not paint itself.
	IsText() bool
	// AbsorbChild folds a drawn child's extent into this element's.
	AbsorbChild(child *Element)
	// End closes whatever Begin opened, returning the canvas to continue on.
	End(dst gfxcore.Canvas) gfxcore.Canvas
}

// noTextRenderer is the default: it renders no text and says so once, rather
// than dropping <text> elements silently.
type noTextRenderer struct{ warned bool }

func (n *noTextRenderer) NewSpan([]byte, map[string]string, pr.Properties, bool) (Drawable, error) {
	if !n.warned {
		n.warned = true
		logger.Warn("<text> ignored: no text renderer configured")
	}
	return nil, nil
}

func (n *noTextRenderer) Begin(_ *Element, dst gfxcore.Canvas) (TextPass, gfxcore.Canvas) {
	return noTextPass{}, dst
}

type noTextPass struct{}

func (noTextPass) IsText() bool                          { return false }
func (noTextPass) AbsorbChild(*Element)                  {}
func (noTextPass) End(dst gfxcore.Canvas) gfxcore.Canvas { return dst }

// <line> tag
type line struct {
	x1, y1, x2, y2 Value
}

func newLine(node *cascadedNode, _ *svgContext) (drawable, error) {
	var (
		out line
		err error
	)
	out.x1, err = parseValue(node.attrs["x1"])
	if err != nil {
		return nil, err
	}
	out.y1, err = parseValue(node.attrs["y1"])
	if err != nil {
		return nil, err
	}
	out.x2, err = parseValue(node.attrs["x2"])
	if err != nil {
		return nil, err
	}
	out.y2, err = parseValue(node.attrs["y2"])
	if err != nil {
		return nil, err
	}

	return out, nil
}

// vectorAngle returns the direction of the vector (dx, dy) in radians,
// normalized to [0, 2π).
func vectorAngle(dx, dy Fl) Fl {
	a := math.Atan2(float64(dy), float64(dx))
	if math.IsNaN(a) {
		return 0
	}
	if a < 0 {
		a += 2 * math.Pi
	}
	return Fl(a)
}

// bisectorAngle merges the arrival and departure directions at a path
// vertex, following resvg's calc_angle: the half-way angle, flipped
// when the turn exceeds a quarter circle so the marker keeps pointing
// forward.
func bisectorAngle(inA, outA Fl) Fl {
	d := (outA - inA) / 2
	angle := inA + d
	if d < 0 {
		d = -d
	}
	if d > math.Pi/2 {
		angle -= math.Pi
	}
	a := math.Mod(float64(angle), 2*math.Pi)
	if a < 0 {
		a += 2 * math.Pi
	}
	return Fl(a)
}

func (l line) draw(dst gfxcore.Canvas, _ *attributes, _ *SVGImage, dims drawingDims) []vertex {
	x1, y1 := dims.point(l.x1, l.y1)
	x2, y2 := dims.point(l.x2, l.y2)
	dst.MoveTo(x1, y1)
	dst.LineTo(x2, y2)

	angle := vectorAngle(x2-x1, y2-y1)
	return []vertex{
		{x1, y1, angle},
		{x2, y2, angle},
	}
}

// <rect> tag
type rect struct {
	// x, y, width, height are common attributes

	rx, ry Value
}

func newRect(node *cascadedNode, _ *svgContext) (drawable, error) {
	rx_, ry_ := node.attrs["rx"], node.attrs["ry"]
	if rx_ == "" {
		rx_ = ry_
	} else if ry_ == "" {
		ry_ = rx_
	}

	var (
		out rect
		err error
	)
	out.rx, err = parseValue(rx_)
	if err != nil {
		return nil, err
	}
	out.ry, err = parseValue(rx_)
	if err != nil {
		return nil, err
	}

	return out, nil
}

func (r rect) draw(dst gfxcore.Canvas, attrs *attributes, _ *SVGImage, dims drawingDims) []vertex {
	width, height := dims.point(attrs.width, attrs.height)
	if width <= 0 || height <= 0 { // nothing to draw
		return nil
	}
	x, y := dims.point(attrs.x, attrs.y)
	rx, ry := dims.point(r.rx, r.ry)

	if rx == 0 || ry == 0 { // no border radius
		dst.Rectangle(x, y, width, height)
		return nil
	}

	if rx > width/2 {
		rx = width / 2
	}
	if ry > height/2 {
		ry = height / 2
	}

	// Inspired by Cairo Cookbook
	// http://cairographics.org/cookbook/roundedrectangles/
	const arcToBezier = 4 * (math.Sqrt2 - 1) / 3
	c1, c2 := arcToBezier*rx, arcToBezier*ry

	dst.MoveTo(x+rx, y)
	dst.LineTo(x+width-rx, y)
	dst.CubicTo(x+width-rx+c1, y, x+width, y+c2, x+width, y+ry)
	dst.LineTo(x+width, y+height-ry)
	dst.CubicTo(
		x+width, y+height-ry+c2, x+width+c1-rx, y+height,
		x+width-rx, y+height)
	dst.LineTo(x+rx, y+height)
	dst.CubicTo(x+rx-c1, y+height, x, y+height-c2, x, y+height-ry)
	dst.LineTo(x, y+ry)
	dst.CubicTo(x, y+ry-c2, x+rx-c1, y, x+rx, y)
	dst.LineTo(x+rx, y)

	return nil
}

// polyline or polygon
type polyline struct {
	points []point // x, y
	close  bool    // true for polygon
}

func newPolyline(node *cascadedNode, _ *svgContext) (drawable, error) {
	return parsePoly(node)
}

func newPolygon(node *cascadedNode, _ *svgContext) (drawable, error) {
	out, err := parsePoly(node)
	out.close = true
	return out, err
}

func parsePoly(node *cascadedNode) (polyline, error) {
	var out polyline

	pts, err := parsePoints(node.attrs["points"], nil, false)
	if err != nil {
		return out, err
	}

	// "If the attribute contains an odd number of coordinates, the last one will be ignored."
	out.points = make([]point, len(pts)/2)
	for i := range out.points {
		out.points[i].x = pts[2*i]
		out.points[i].y = pts[2*i+1]
	}

	return out, nil
}

func (r polyline) draw(dst gfxcore.Canvas, _ *attributes, _ *SVGImage, _ drawingDims) []vertex {
	if len(r.points) == 0 {
		return nil
	}
	p1, points := r.points[0], r.points[1:]
	dst.MoveTo(p1.x, p1.y)
	for _, point := range points {
		dst.LineTo(point.x, point.y)
	}
	if r.close {
		dst.ClosePath()
	}

	// segment directions, then per-vertex angles: departure for the
	// first vertex, arrival for the last, bisector in between (same
	// rules as path vertices)
	pts := r.points
	angleOf := func(from, to point) (Fl, bool) {
		if from == to {
			return 0, false
		}
		return vectorAngle(to.x-from.x, to.y-from.y), true
	}
	vertices := make([]vertex, len(pts))
	for i, pt := range pts {
		var (
			inA, outA   Fl
			okIn, okOut bool
		)
		if i > 0 {
			inA, okIn = angleOf(pts[i-1], pt)
		}
		if i+1 < len(pts) {
			outA, okOut = angleOf(pt, pts[i+1])
		}
		var angle Fl
		switch {
		case okIn && okOut:
			angle = bisectorAngle(inA, outA)
		case okIn:
			angle = inA
		case okOut:
			angle = outA
		}
		vertices[i] = vertex{pt.x, pt.y, angle}
	}
	return vertices
}

// ellipse or circle
type ellipse struct {
	rx, ry, cx, cy Value
}

func newEllipse(node *cascadedNode, _ *svgContext) (drawable, error) {
	r_, rx_, ry_ := node.attrs["r"], node.attrs["rx"], node.attrs["ry"]
	if rx_ == "" {
		rx_ = r_
	}
	if ry_ == "" {
		ry_ = r_
	}

	var (
		out ellipse
		err error
	)
	out.rx, err = parseValue(rx_)
	if err != nil {
		return nil, err
	}
	out.ry, err = parseValue(ry_)
	if err != nil {
		return nil, err
	}
	out.cx, err = parseValue(node.attrs["cx"])
	if err != nil {
		return nil, err
	}
	out.cy, err = parseValue(node.attrs["cy"])
	if err != nil {
		return nil, err
	}

	return out, nil
}

func (e ellipse) draw(dst gfxcore.Canvas, _ *attributes, _ *SVGImage, dims drawingDims) []vertex {
	rx, ry := dims.point(e.rx, e.ry)
	if rx == 0 || ry == 0 {
		return nil
	}
	// kappa: control-handle length approximating a quarter circle with
	// one cubic Bézier (max radial error ~0.027%). The previous 1/√π
	// (~0.5642) bulged the arc 0.64% outward at its midpoint.
	const kappa = 4 * (math.Sqrt2 - 1) / 3
	ratioX := rx * kappa
	ratioY := ry * kappa
	cx, cy := dims.point(e.cx, e.cy)

	dst.MoveTo(cx+rx, cy)
	dst.CubicTo(cx+rx, cy+ratioY, cx+ratioX, cy+ry, cx, cy+ry)
	dst.CubicTo(cx-ratioX, cy+ry, cx-rx, cy+ratioY, cx-rx, cy)
	dst.CubicTo(cx-rx, cy-ratioY, cx-ratioX, cy-ry, cx, cy-ry)
	dst.CubicTo(cx+ratioX, cy-ry, cx+rx, cy-ratioY, cx+rx, cy)
	dst.LineTo(cx+rx, cy)

	return nil
}

// <path> tag
type path []pathItem

func newPath(node *cascadedNode, context *svgContext) (drawable, error) {
	out, err := context.pathParser.parsePath(node.attrs["d"])
	if err != nil {
		return nil, err
	}

	return path(out), err
}

func (p path) draw(dst gfxcore.Canvas, _ *attributes, _ *SVGImage, _ drawingDims) []vertex {
	// draw, recording each segment's start point
	starts := make([]point, len(p))
	var segmentStart point
	for i, item := range p {
		item.draw(dst)
		starts[i] = segmentStart
		segmentStart = item.endPoint()
	}

	// marker vertex angles (https://www.w3.org/TR/SVG2/painting.html#OrientAttribute):
	// a subpath's first vertex takes the departure direction, its last
	// the arrival direction, and interior vertices the bisector of both.
	out := make([]vertex, len(p))
	for i, item := range p {
		endPt := item.endPoint()
		var angle Fl
		if item.op == moveTo {
			if i+1 < len(p) {
				angle, _ = p[i+1].departureAngle(endPt)
			}
		} else {
			inA, okIn := item.arrivalAngle(starts[i])
			var (
				outA  Fl
				okOut bool
			)
			if i+1 < len(p) && p[i+1].op != moveTo {
				outA, okOut = p[i+1].departureAngle(endPt)
			}
			switch {
			case okIn && okOut:
				angle = bisectorAngle(inA, outA)
			case okIn:
				angle = inA
			case okOut:
				angle = outA
			}
		}
		out[i] = vertex{endPt.x, endPt.y, angle}
	}
	return out
}

// <svg> tag
type svg struct {
	preserveRatio    preserveAspectRatio
	isRoot           bool
	isOverflowHidden bool
}

func newSvg(node *cascadedNode, tree *svgContext) (drawable, error) {
	var out svg
	out.preserveRatio = node.attrs.aspectRatio()
	out.isRoot = node == tree.root
	if overflow := node.attrs["overflow"]; overflow == "" || overflow == "hidden" {
		out.isOverflowHidden = true
	}

	return out, nil
}

func (s svg) draw(dst gfxcore.Canvas, attrs *attributes, img *SVGImage, dims drawingDims) []vertex {
	x, y := dims.point(attrs.x, attrs.y)
	dst.State().Transform(matrix.Translation(x, y))
	width, height := dims.concreteWidth, dims.concreteHeight
	if !s.isRoot {
		width, height = dims.point(attrs.width, attrs.height)
	}

	viewbox := attrs.viewbox
	syntheticViewbox := false
	if viewbox == nil && s.isRoot {
		dw, dh := img.DisplayedSize()
		if dw.U != Perc && dh.U != Perc {
			w := dw.Resolve(&dims.Dims, 0)
			h := dh.Resolve(&dims.Dims, 0)
			viewbox = &Rectangle{Width: w, Height: h}
			syntheticViewbox = true
		}
	}

	// When using a synthetic viewBox (no explicit viewBox attribute),
	// preserveAspectRatio has no effect per SVG spec — the content
	// stretches to fill the viewport.
	pr := s.preserveRatio
	if syntheticViewbox {
		pr = preserveAspectRatio{none: true}
	}
	sx, sy, tx, ty := pr.resolveTransforms(width, height, viewbox, nil)
	if !s.isRoot && s.isOverflowHidden {
		dst.Rectangle(0, 0, width, height)
		dst.State().Clip(false)
	}

	dst.State().Transform(matrix.New(sx, 0, 0, sy, tx, ty))
	return nil
}

// <image> tag
type image struct {
	// width, height are common attributes

	img            Image
	preserveRatio  preserveAspectRatio
	imageRendering string
}

func newImage(node *cascadedNode, context *svgContext) (drawable, error) {
	baseURL := node.attrs["base"]
	if baseURL == "" {
		baseURL = context.baseURL
	}

	href := node.attrs["href"]
	url, err := utils.SafeUrljoin(baseURL, href, true)
	if err != nil {
		return nil, fmt.Errorf("invalid image source: %s", err)
	}
	if context.imageLoader == nil {
		return nil, fmt.Errorf("no image loader, ignoring the image %s", url)
	}
	img, err := context.imageLoader(url)
	if err != nil {
		return nil, fmt.Errorf("failed to load image: %s", err)
	}

	var out image
	out.img = img
	out.preserveRatio = node.attrs.aspectRatio()
	// Per WP commit 6446aa48: honor the image-rendering attribute
	// instead of always passing "auto" to the gfxcore.
	if r := node.attrs["image-rendering"]; r != "" {
		out.imageRendering = r
	} else {
		out.imageRendering = "auto"
	}

	return out, nil
}

func (img image) draw(dst gfxcore.Canvas, attrs *attributes, svg *SVGImage, dims drawingDims) []vertex {
	x, y := dims.point(attrs.x, attrs.y)
	dst.State().Transform(matrix.Translation(x, y))
	// base_url = node.get("{http://www.w3.org/XML/1998/namespace}base")
	// url = node.get_href(base_url || svg.url)
	// image = svg.context.get_image_from_uri(url, "image/*")
	// if image == nil {
	// 	return
	// }

	width, height := dims.point(attrs.width, attrs.height)
	intrinsicWidth, intrinsicHeight, intrinsicRatio := img.img.IntrinsicSize(dims.FontSize)
	if intrinsicWidth == nil && intrinsicHeight == nil {
		if intrinsicRatio == nil || (width == 0 && height == 0) {
			intrinsicWidth, intrinsicHeight = pr.Float(300), pr.Float(150)
		} else if width == 0 {
			intrinsicWidth, intrinsicHeight = intrinsicRatio.V()*pr.Float(height), pr.Float(height)
		} else {
			intrinsicWidth, intrinsicHeight = pr.Float(width), pr.Float(width)/intrinsicRatio.V()
		}
	} else if intrinsicWidth == nil {
		intrinsicWidth = intrinsicRatio.V() * intrinsicHeight.V()
	} else if intrinsicHeight == nil {
		intrinsicHeight = intrinsicWidth.V() / intrinsicRatio.V()
	}
	intrinsic := Rectangle{0, 0, Fl(intrinsicWidth.V()), Fl(intrinsicHeight.V())}
	if width == 0 {
		if height != 0 && intrinsicRatio != nil {
			width = height * Fl(intrinsicRatio.V())
		} else {
			width = intrinsic.Width
		}
	}
	if height == 0 {
		if width != 0 && intrinsicRatio != nil {
			height = width / Fl(intrinsicRatio.V())
		} else {
			height = intrinsic.Height
		}
	}

	scale_x, scale_y, translate_x, translate_y := img.preserveRatio.resolveTransforms(width, height, &intrinsic, nil)
	dst.Rectangle(0, 0, width, height)
	dst.State().Clip(false)
	imageRendering := img.imageRendering
	if imageRendering == "" {
		imageRendering = "auto"
	}
	dst.OnNewStack(func() {
		dst.State().Transform(matrix.Transform{A: scale_x, D: scale_y, E: translate_x, F: translate_y})
		img.img.Draw(dst, intrinsic.Width, intrinsic.Height, imageRendering, svg.renderer())
	})

	return nil
}

func (b *useBudget) remainingNodes() int { return limits.UseElements - b.nodes }

// useDocument fetches and parses the document at url (without fragment),
// referenced by a <use>, styled for the same medium and sharing the <use>
// limits of context. Its definitions (gradients, clip paths, …) are added
// to defs, except the ones the referencing documents already define. It
// returns nil, with a warning, when the document cannot or may not be used.
func (context *svgContext) useDocument(url string, defs definitions) *svgContext {
	if slices.Contains(context.useChain, url) {
		logger.Warnf("SVG: <use> cycle through %s, ignored", url)
		return nil
	}
	// each document is fetched, parsed and charged once: a sprite sheet
	// is referenced by many <use>
	if tree, done := context.useBudget.parsed[url]; done {
		return tree
	}
	// refusals depending on the referencing chain are not cached
	if len(context.useChain) >= limits.UseDepth {
		logger.Warnf("SVG: more than %d nested documents referenced by <use>, ignoring %s", limits.UseDepth, url)
		return nil
	}
	if context.useBudget.documents >= limits.UseDocuments {
		logger.Warnf("SVG: more than %d documents referenced by <use>, ignoring %s", limits.UseDocuments, url)
		return nil
	}
	tree := context.loadUseDocument(url, defs)
	if context.useBudget.parsed == nil {
		context.useBudget.parsed = map[string]*svgContext{}
	}
	context.useBudget.parsed[url] = tree
	return tree
}

// loadUseDocument is [svgContext.useDocument], for a document not loaded
// yet.
func (context *svgContext) loadUseDocument(url string, defs definitions) *svgContext {
	if context.urlFetcher == nil {
		logger.Warnf("SVG: no URL fetcher, ignoring <use> of %s", url)
		return nil
	}
	context.useBudget.documents++
	content, err := context.urlFetcher(url)
	if err != nil {
		logger.Warnf("SVG: fetching <use> content: %s", err)
		return nil
	}
	tree, err := newSVGContextReader(content.Content, url, context.urlFetcher, &context.device)
	if err != nil {
		logger.Warnf("SVG: parsing <use> content: %s", err)
		return nil
	}
	tree.imageLoader = context.imageLoader
	tree.useBudget = context.useBudget
	tree.useChain = append(slices.Clip(context.useChain), url)

	// register the definitions of the document: process it whole into its
	// own definitions, merged without overriding the existing ones
	own := newDefinitions()
	if n := tree.root.subtreeSize(); n > context.useBudget.remainingNodes() {
		logger.Warnf("SVG: more than %d elements instantiated by <use>, ignoring %s", limits.UseElements, url)
		return nil
	} else {
		context.useBudget.nodes += n
	}
	if _, err := tree.processNode(tree.root, own); err != nil {
		logger.Warnf("SVG: processing <use> content: %s", err)
		return nil
	}
	defs.mergeMissing(own)
	return tree
}

// wraps a node
// it is a special case of content, handled in drawNode
type use struct {
	target *svgNode
}

// useBudget is what remains of the <use> limits of a document, shared with
// the documents it references.
type useBudget struct {
	nodes, documents int
	// the documents referenced by <use>, parsed once (nil: the fetch or
	// the parse failed, or the document may not be used)
	parsed map[string]*svgContext
}

// subtreeSize returns the number of elements of the tree n, computed once:
// the trees <use> instantiates do not change after they are built, and a
// target may be referenced many times.
func (n *cascadedNode) subtreeSize() int {
	if n.size == 0 {
		n.size = 1
		for _, c := range n.children {
			n.size += c.subtreeSize()
		}
	}
	return n.size
}

// contains reports whether node is in the tree n.
func (n *cascadedNode) contains(node *cascadedNode) bool {
	if n == node {
		return true
	}
	for _, c := range n.children {
		if c.contains(node) {
			return true
		}
	}
	return false
}

// deepCopy returns a copy of the tree n whose nodes (attributes, typed
// properties, children) are its own: a <use> cascades its attributes into
// the copy, which must not reach the definition other uses share.
func (n *cascadedNode) deepCopy() cascadedNode {
	out := n.copy()
	if len(n.children) != 0 {
		out.children = make([]*cascadedNode, len(n.children))
		for i, c := range n.children {
			child := c.deepCopy()
			out.children[i] = &child
		}
	}
	return out
}

// withoutFragment returns url without its fragment.
func withoutFragment(url string) string {
	if i := strings.IndexByte(url, '#'); i != -1 {
		return url[:i]
	}
	return url
}

// resolves and returns the <use> target content
// wrapped in graphicContent field
func (context *svgContext) resolveUse(node *cascadedNode, defs definitions) (*svgNode, error) {
	href, err := parseURL(node.attrs["href"])
	if err != nil {
		return nil, err
	}
	// a reference to this document: a fragment alone, or a URL resolving
	// to this document
	var documentURL string // the referenced document, if another one
	if href.Scheme != "" || href.Opaque != "" || href.Host != "" || href.Path != "" {
		joined, err := utils.SafeUrljoin(context.baseURL, href.String(), true)
		if err != nil {
			return nil, err
		}
		if withoutFragment(joined) != withoutFragment(context.baseURL) {
			documentURL = withoutFragment(joined)
		}
	}
	tree, ID := context, href.Fragment
	if documentURL != "" {
		tree = context.useDocument(documentURL, defs)
		if tree == nil {
			return nil, nil
		}
	}

	var source *cascadedNode // the instantiated tree
	switch {
	case ID != "":
		if tree.inUseIDs.Has(ID) {
			return nil, fmt.Errorf("invalid recursive <use>")
		}
		if tree.defs[ID] == nil {
			logger.Warnf("SVG: <use> content not defined")
			return nil, nil
		}
		tree.inUseIDs.Add(ID)
		// update after resolving
		defer func() {
			delete(tree.inUseIDs, ID)
		}()
		source = tree.defs[ID] // inner child
	case tree != context:
		source = tree.root // the whole document
	default:
		return nil, fmt.Errorf("invalid <use> of its own document")
	}

	// charged before the copy: a refused <use> costs nothing
	if n := source.subtreeSize(); n > context.useBudget.remainingNodes() {
		logger.Warnf("SVG: more than %d elements instantiated by <use>, ignoring the rest", limits.UseElements)
		return nil, nil
	} else {
		context.useBudget.nodes += n
	}
	// a target containing the <use> is a circular reference (SVG 2
	// §5.6.3): the walk is bounded by the budget just charged
	if tree == context && source.contains(node) {
		return nil, fmt.Errorf("invalid <use> of one of its ancestors")
	}
	useTarget := source.deepCopy()

	// the instance inherits from the <use>, not from the ancestors of its
	// definition (SVG 2 §5.6.2): its tree is cascaded again, currentColor
	// included
	var recascade func(n *cascadedNode, parent nodeAttributes)
	recascade = func(n *cascadedNode, parent nodeAttributes) {
		if n.specified != nil {
			n.cascaded, n.attrs, n.props = cascadeAttributes(n.specified, parent)
		}
		for _, child := range n.children {
			recascade(child, n.cascaded)
		}
	}
	recascade(&useTarget, node.cascaded)

	if useTarget.tag == "svg" || useTarget.tag == "symbol" {
		// the width and height of the <use> are those of the instance, or
		// else its own, or else 100% (SVG 2 §5.6.2)
		useTarget.tag = "svg"
		for _, key := range [...]string{"width", "height"} {
			if v, has := node.attrs[key]; has {
				useTarget.attrs[key] = v
			} else if _, has := useTarget.attrs[key]; !has {
				useTarget.attrs[key] = "100%"
			}
		}
		syncProps(useTarget.attrs, useTarget.props)
	}

	// the target's own references (<use href="#…">, nested documents)
	// resolve in its document
	targetDefs := defs
	if tree != context {
		targetDefs = newDefinitions()
	}
	target, err := tree.processNode(&useTarget, targetDefs)
	if err != nil {
		return nil, err
	}
	if tree != context {
		defs.mergeMissing(targetDefs)
	}
	if tree != context && !context.queriesOf[tree] {
		// once per document, however many uses reference it
		if context.queriesOf == nil {
			context.queriesOf = map[*svgContext]bool{}
		}
		context.queriesOf[tree] = true
		context.sizeQueries = append(context.sizeQueries, tree.sizeQueries...)
	}

	out := &svgNode{
		graphicContent: use{target: target},
	}
	err = node.attrs.parseCommonAttributes(node.props, &out.attributes)
	return out, err
}

func (use) boundingBox(attrs *attributes, dims drawingDims) (Rectangle, bool) {
	return Rectangle{}, false
}

func (u use) draw(dst gfxcore.Canvas, attrs *attributes, svg *SVGImage, dims drawingDims) []vertex {
	if u.target == nil {
		return nil
	}
	x, y := dims.point(attrs.x, attrs.y)

	dst.OnNewStack(func() {
		dst.State().Transform(matrix.Translation(x, y))
		svg.drawNode(dst, u.target, dims, true) // actually draw the target
	})
	return nil
}

// definitions

type filter interface {
	isFilter()
}

func (filterOffset) isFilter() {}
func (filterBlend) isFilter()  {}

type filterOffset struct {
	dx, dy      Value
	isUnitsBBox bool
}

type filterBlend string

// parse a <filter> node
func newFilter(node *cascadedNode) (out []filter, err error) {
	for _, child := range node.children {
		switch child.tag {
		case "feOffset":
			fi := filterOffset{
				isUnitsBBox: node.attrs["primitiveUnits"] == "objectBoundingBox",
			}
			fi.dx, err = parseValue(child.attrs["dx"])
			if err != nil {
				return nil, err
			}
			fi.dy, err = parseValue(child.attrs["dy"])
			if err != nil {
				return nil, err
			}
			out = append(out, fi)
		case "feBlend":
			fi := filterBlend("normal")
			if mode, has := child.attrs["mode"]; has {
				fi = filterBlend(mode)
			}
			out = append(out, fi)
		default:
			logger.Warnf("unsupported filter element: %s", child.tag)
		}
	}

	return out, nil
}

// clipPath is a container for
// graphic nodes, which will use as clipping path,
// that is drawn but not stroked nor filled.
type clipPath struct {
	svgNode
	isUnitsBBox bool
	// isClipUseEvenOdd is true when clip-rule="evenodd" is set on the
	// <clipPath> element. Per SVG 1.1, clip-rule may be set on each
	// child of <clipPath>; supporting that would require per-child
	// tracking. Most real-world usage sets clip-rule on the parent, so
	// we honor that level only.
	isClipUseEvenOdd bool
}

func newClipPath(node *cascadedNode, children []*svgNode) (*clipPath, error) {
	out := clipPath{
		svgNode:          svgNode{children: children},
		isUnitsBBox:      node.attrs["clipPathUnits"] == "objectBoundingBox",
		isClipUseEvenOdd: stringOf(node.props, PClipRule) == "evenodd",
	}
	err := node.attrs.parseCommonAttributes(node.props, &out.attributes)
	return &out, err
}

// marker is a container for a marker to draw
// on a path (start, middle or end)
type marker struct {
	viewbox *Rectangle

	children []*svgNode

	overflow string // default to "hidden"

	preserveAspectRatio preserveAspectRatio // default to "xMidYMid"

	orient orientation

	markerWidth, markerHeight Value
	refX, refY                Value

	isUnitsUserSpace bool
}

func newMarker(node *cascadedNode, children []*svgNode) (out *marker, err error) {
	out = &marker{
		children:         children,
		isUnitsUserSpace: node.attrs["markerUnits"] == "userSpaceOnUse",
	}

	out.viewbox, err = node.attrs.viewBox()
	if err != nil {
		return nil, err
	}

	out.orient, err = parseOrientation(node.attrs["orient"])
	if err != nil {
		return nil, err
	}

	out.preserveAspectRatio = node.attrs.aspectRatio()

	out.overflow = "hidden"
	if s, has := node.attrs["overflow"]; has {
		out.overflow = s
	}

	mw, mh := "3", "3"
	if s, has := node.attrs["markerWidth"]; has {
		mw = s
	}
	if s, has := node.attrs["markerHeight"]; has {
		mh = s
	}
	out.markerWidth, err = parseValue(mw)
	if err != nil {
		return nil, err
	}
	out.markerHeight, err = parseValue(mh)
	if err != nil {
		return nil, err
	}

	out.refX, err = parseValue(node.attrs["refX"])
	if err != nil {
		return nil, err
	}
	out.refY, err = parseValue(node.attrs["refY"])
	if err != nil {
		return nil, err
	}

	return out, nil
}

// mask is a container for shape that will
// be used as an alpha mask
type mask struct {
	svgNode
	isUnitsBBox bool
}

func newMask(node *cascadedNode, children []*svgNode) (mask, error) {
	out := mask{
		svgNode: svgNode{
			children: children,
		},
		isUnitsBBox: node.attrs["maskUnits"] == "objectBoundingBox",
	}
	err := node.attrs.parseCommonAttributes(node.props, &out.svgNode.attributes)
	if err != nil {
		return mask{}, err
	}
	// default values
	if out.x.U == 0 {
		out.x = Value{-10, Perc} // -10%
	}
	if out.y.U == 0 {
		out.y = Value{-10, Perc} // -10%
	}
	if out.width.U == 0 {
		out.width = Value{120, Perc} // 120%
	}
	if out.height.U == 0 {
		out.height = Value{120, Perc} // 120%
	}
	return out, err
}

// transformation matrices

type transformKind uint8

const (
	_                transformKind = iota
	rotate                         // 1 argument
	rotateWithOrigin               // 3 arguments
	translate                      // 2 arguments
	skew                           // 2 argument
	scale                          // 2 arguments
	customMatrix                   // 6 arguments
)

type transform struct {
	kind transformKind
	args [6]Value // the actual number of arguments depends on kind
}

// right multiply to `mat` to apply the transformation, after resolving units
func (tr transform) applyTo(mat *matrix.Transform, dims *drawingDims, diagonal Fl) {
	switch tr.kind {
	case rotate:
		angle := tr.args[0].Resolve(&dims.Dims, diagonal)
		mat.Rotate(angle * math.Pi / 180)
	case rotateWithOrigin:
		x, y := tr.args[1].Resolve(&dims.Dims, diagonal), tr.args[2].Resolve(&dims.Dims, diagonal)
		angle := tr.args[0].Resolve(&dims.Dims, diagonal)
		mat.Translate(x, y)
		mat.Rotate(angle * math.Pi / 180)
		mat.Translate(-x, -y)
	case translate:
		x, y := tr.args[0].Resolve(&dims.Dims, diagonal), tr.args[1].Resolve(&dims.Dims, diagonal)
		mat.Translate(x, y)
	case skew:
		thetaX, thetaY := tr.args[0].Resolve(&dims.Dims, diagonal), tr.args[1].Resolve(&dims.Dims, diagonal)
		mat.Skew(thetaX*math.Pi/180, thetaY*math.Pi/180)
	case scale:
		sx, sy := tr.args[0].Resolve(&dims.Dims, diagonal), tr.args[1].Resolve(&dims.Dims, diagonal)
		mat.Scale(sx, sy)
	case customMatrix:
		mat.RightMultBy(matrix.New(
			tr.args[0].Resolve(&dims.Dims, diagonal),
			tr.args[1].Resolve(&dims.Dims, diagonal),
			tr.args[2].Resolve(&dims.Dims, diagonal),
			tr.args[3].Resolve(&dims.Dims, diagonal),
			tr.args[4].Resolve(&dims.Dims, diagonal),
			tr.args[5].Resolve(&dims.Dims, diagonal),
		))
	}
}
