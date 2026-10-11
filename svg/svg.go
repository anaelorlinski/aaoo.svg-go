// Package svg implements parsing of SVG images.
// It transforms SVG text files into an in-memory structure
// that is easy to draw.
// CSS is supported via the `css` package.
package svg

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strings"

	pr "github.com/anaelorlinski/aaoo.cssengine-go/properties"
	"github.com/anaelorlinski/aaoo.cssengine-go/stylesheet"
	"github.com/anaelorlinski/aaoo.gfxcore-go"
	"github.com/anaelorlinski/aaoo.gfxcore-go/matrix"
	"github.com/anaelorlinski/aaoo.svg-go/internal/limits"
	"github.com/anaelorlinski/aaoo.svg-go/internal/utils"
	"github.com/anaelorlinski/aaoo.svg-go/logger"
	"golang.org/x/net/html"
)

// convert from an svg tree to the final form

// nodes that are not directly draw but may be referenced by ID
// from other nodes
type definitions struct {
	filters      map[string][]filter
	clipPaths    map[string]*clipPath
	masks        map[string]mask
	paintServers map[string]paintServer
	markers      map[string]*marker
	nodes        map[string]*svgNode
}

// mergeMissing adds the definitions of other whose id is not defined in d.
func (d definitions) mergeMissing(other definitions) {
	addMissing(d.filters, other.filters)
	addMissing(d.clipPaths, other.clipPaths)
	addMissing(d.masks, other.masks)
	addMissing(d.paintServers, other.paintServers)
	addMissing(d.markers, other.markers)
	addMissing(d.nodes, other.nodes)
}

func addMissing[V any](dst, src map[string]V) {
	for k, v := range src {
		if _, ok := dst[k]; !ok {
			dst[k] = v
		}
	}
}

func newDefinitions() definitions {
	return definitions{
		filters:      make(map[string][]filter),
		clipPaths:    make(map[string]*clipPath),
		masks:        make(map[string]mask),
		paintServers: make(map[string]paintServer),
		markers:      make(map[string]*marker),
		nodes:        make(map[string]*svgNode),
	}
}

// SVGImage is a parsed SVG document, ready to draw.
//
// It is not safe for concurrent use: drawing records state on the image
// (the text renderer, the clip paths and masks being applied, the text
// built lazily, the styles of other sizes). Draw it from one goroutine at
// a time.
type SVGImage struct {
	root *svgNode

	// setup when calling Draw. Core never names the text package: a real
	// renderer is supplied by the caller, the default draws nothing.
	textRenderer TextRenderer

	definitions definitions

	// activeRefs tracks the clip-path and mask ids currently being
	// applied, to break reference cycles: a recursive reference is
	// ignored and the rest of the clip/mask applies normally (matching
	// resvg).
	activeRefs map[string]bool

	// the elements Draw may still draw (see [limits.DrawsPerElement]),
	// and the elements of the document
	drawBudget   int
	budgetWarned bool
	elements     int

	// non nil when the styles depend on the size of the viewport (@media
	// (width ...) in an SVG image): Draw applies them again for the size
	// it draws at
	resize *resizer
}

// Device is the medium the @media rules of an SVG are evaluated against.
type Device = stylesheet.Device

// resizer keeps what is needed to parse an SVG image again for another
// viewport size, and the results.
//
// Like [SVGImage], it is not safe for concurrent use.
type resizer struct {
	root           *html.Node
	baseURL        string
	imageLoader    ImageLoader
	urlFetcher     Fetcher // memoised: resources are fetched once
	inheritedColor string
	device         Device // the device of the original parse

	original variant   // the image itself
	variants []variant // the other sizes drawn, most recent last
}

// variant is the image styled for some viewport size.
type variant struct {
	queries [][]stylesheet.MediaQuery // the size queries evaluated
	results []bool                    // and their results
	img     *SVGImage
}

func newVariant(img *SVGImage, tree *svgContext) variant {
	out := variant{queries: tree.sizeQueries, img: img}
	for _, q := range tree.sizeQueries {
		out.results = append(out.results, stylesheet.EvaluateMediaQuery(q, tree.device))
	}
	return out
}

// fits reports whether v is the image styled for device: whether each
// query evaluated when styling it gives the same result for device.
func (v variant) fits(device Device) bool {
	for i, q := range v.queries {
		if stylesheet.EvaluateMediaQuery(q, device) != v.results[i] {
			return false
		}
	}
	return true
}

// variant returns the image styled for a viewport of width x height, or
// nil if it is the image itself.
func (r *resizer) variant(width, height Fl) *SVGImage {
	valid := func(v Fl) bool { return v > 0 && !math.IsInf(float64(v), 0) } // false for NaN
	if r == nil || !valid(width) || !valid(height) {
		return nil
	}
	device := r.device
	device.Width, device.Height = width, height
	if r.original.fits(device) {
		return nil
	}
	for i, v := range r.variants {
		if v.fits(device) {
			// most recent last
			r.variants = append(append(r.variants[:i:i], r.variants[i+1:]...), v)
			return v.img
		}
	}
	img, tree, err := parseNode(r.root, r.baseURL, r.imageLoader, r.urlFetcher, r.inheritedColor, &device)
	if err != nil { // not expected, since the first parse succeeded
		logger.Warnf("SVG: styling for %gx%g: %s", width, height, err)
		return nil
	}
	if len(r.variants) == limits.SVGVariants {
		r.variants = r.variants[1:]
	}
	v := newVariant(img, tree)
	r.variants = append(r.variants, v)
	return v.img
}

// memoFetcher fetches each URL once.
type memoFetcher struct {
	fetcher Fetcher
	cache   map[string]memoResult
}

type memoResult struct {
	content  []byte
	resource Resource // without Content
	err      error
}

func (m *memoFetcher) fetch(url string) (Resource, error) {
	r, ok := m.cache[url]
	if !ok {
		r.resource, r.err = m.fetcher(url)
		if r.err == nil && r.resource.Content != nil {
			r.content, r.err = io.ReadAll(r.resource.Content)
			r.resource.Content = nil
		}
		if m.cache == nil {
			m.cache = map[string]memoResult{}
		}
		m.cache[url] = r
	}
	out := r.resource
	out.Content = bytes.NewReader(r.content)
	return out, r.err
}

// DisplayedSize returns the value of the "width" and "height" attributes
// of the <svg> root element, which discribe the displayed size of the rectangular viewport.
// If no value is specified, it default to 100% (auto).
func (svg *SVGImage) DisplayedSize() (width, height Value) {
	w, h := svg.root.width, svg.root.height
	if w.U == 0 {
		w = Value{100, Perc}
	}
	if h.U == 0 {
		h = Value{100, Perc}
	}
	return w, h
}

// ViewBox returns the optional value of the "viewBox" attribute
func (svg *SVGImage) ViewBox() *Rectangle { return svg.root.viewbox }

// Draw draws the parsed SVG image into the given `dst` output,
// with the given `width` and `height`.
// `textRenderer` lays out <text> tags. It may be nil for svg content not
// displaying text, in which case such elements are skipped and reported.
//
// The @media rules of an SVG image are evaluated against its viewport: if
// one tests its size, the image is styled again for width x height.
func (svg *SVGImage) Draw(dst gfxcore.Canvas, width, height Fl, textRenderer TextRenderer) {
	if v := svg.resize.variant(width, height); v != nil {
		v.draw(dst, width, height, textRenderer)
		return
	}
	svg.draw(dst, width, height, textRenderer)
}

func (svg *SVGImage) draw(dst gfxcore.Canvas, width, height Fl, textRenderer TextRenderer) {
	if textRenderer == nil {
		textRenderer = &noTextRenderer{}
	}
	svg.textRenderer = textRenderer
	svg.drawBudget, svg.budgetWarned = max(limits.MinDraws, svg.elements*limits.DrawsPerElement), false
	// Text content is built lazily, since the renderer arrives here rather
	// than at Parse. Materialise the whole tree up front: the anchor pass
	// measures a <text> subtree before any of its children are drawn, so
	// the spans must already exist.
	svg.materialiseText(svg.root)
	svg.drawRoot(dst, svg.root, width, height)
}

// drawRoot draws root, an <svg> element or a pattern tile, in a viewport
// of width x height.
func (svg *SVGImage) drawRoot(dst gfxcore.Canvas, root *svgNode, width, height Fl) {
	var dims drawingDims

	dims.concreteWidth, dims.concreteHeight = width, height

	if vb := root.viewbox; vb != nil {
		dims.ViewportW, dims.ViewportH = vb.Width, vb.Height
	} else {
		dims.ViewportW, dims.ViewportH = width, height
	}

	dims.FontSize = defaultFontSize
	dims.FontSize = root.attributes.fontSize.Resolve(&dims.Dims, dims.FontSize)
	dims.RootFontSize = dims.FontSize

	dims.setupDiagonal()
	svg.drawNode(dst, root, dims, true)
}

// budgetExhausted reports whether the draw budget is spent, warning once.
// The loops instantiating content (markers per vertex) check it too, so
// that they stop with it.
func (svg *SVGImage) budgetExhausted() bool {
	if svg.drawBudget > 0 {
		return false
	}
	if !svg.budgetWarned {
		logger.Warnf("SVG: more than %d elements drawn for %d in the document, ignoring the rest",
			max(limits.MinDraws, svg.elements*limits.DrawsPerElement), svg.elements)
		svg.budgetWarned = true
	}
	return true
}

// if paint is false, only the path operations are executed, not the actual filling or drawing
// moreover, no new graphic stack is created
func (svg *SVGImage) drawNode(dst gfxcore.Canvas, node *svgNode, dims drawingDims, paint bool) {
	if svg.budgetExhausted() {
		return
	}
	svg.drawBudget--
	dims.FontSize = node.attributes.fontSize.Resolve(&dims.Dims, dims.FontSize)

	paintTask := func() {
		// What applies to the node as a whole, with its children and its
		// markers: its opacity, its mask and the way it is mixed with what
		// is under it. The node is then drawn in a group, put on the canvas
		// at the end.
		var composite gfxcore.Composite

		// apply filters
		if filters := svg.definitions.filters[node.filterID]; filters != nil {
			composite.Blend = applyFilters(dst, filters, node, dims)
		}

		// apply transform attribute
		applyTransformWithOrigin(dst, node.attributes.transforms, dims,
			node.attributes.hasTransformOrigin,
			node.attributes.transformOriginX,
			node.attributes.transformOriginY)

		if mode, ok := gfxcore.ParseBlendMode(node.attributes.blendMode); ok {
			composite.Blend = mode
		}
		opacity := node.attributes.opacity
		ma, masked := svg.definitions.masks[node.maskID]
		grouped := paint && (0 <= opacity && opacity < 1 || masked || composite.Blend != gfxcore.BlendNormal)

		var originalDst1 gfxcore.Canvas
		if grouped {
			var x, y, width, height Fl = 0, 0, dims.ViewportW, dims.ViewportH
			if box, ok := node.resolveBoundingBox(dims, true); ok {
				x, y, width, height = box.X, box.Y, box.Width, box.Height
			}
			originalDst1 = dst
			dst = dst.NewGroup(x, y, width, height)
		}

		// clip
		if cp, has := svg.definitions.clipPaths[node.clipPathID]; has {
			if svg.enterRef("clip:"+node.clipPathID, "clip-path "+node.clipPathID) {
				svg.applyClipPath(dst, cp, node, dims)
				svg.leaveRef("clip:" + node.clipPathID)
			}
		}

		// The mask covers the node once it is clipped.
		if masked && paint {
			if svg.enterRef("mask:"+node.maskID, "mask "+node.maskID) {
				composite.Mask, composite.MaskMode = svg.drawMask(dst, ma, node, dims)
				svg.leaveRef("mask:" + node.maskID)
			}
		}

		// Text anchoring is the text renderer's business; drawNode only
		// brackets the draw with it.
		textPass, dst2 := svg.textRenderer.Begin(
			&Element{node: node, attrs: &node.attributes, dims: dims}, dst)
		dst = dst2
		// manage display and visibility
		display := node.attributes.display
		visible := node.attributes.visible

		// draw the node itself : it is done in three steps
		// 	1) resolve paint options and apply it
		// 	2) apply the path operation
		// 	3) conclude by calling Paint

		doFill, doStroke := svg.applyPainters(dst, node, dims)

		var vertices []vertex
		if visible && node.graphicContent != nil {
			vertices = node.graphicContent.draw(dst, &node.attributes, svg, dims)
		}

		// then recurse
		if display {
			for _, child := range node.children {
				svg.drawNode(dst, child, dims, paint)

				textPass.AbsorbChild(&Element{node: child, attrs: &child.attributes, dims: dims})
			}
		}

		dst = textPass.End(dst)

		// do the actual painting :
		// paint by filling and stroking the given node onto the graphic target
		if paint && !textPass.IsText() {
			dst.Paint(newPaintOp(doFill, doStroke, node.isFillEvenOdd))
		}

		// draw markers
		if len(vertices) != 0 {
			svg.drawMarkers(dst, vertices, node, dims, paint)
		}

		// put the group on the canvas it was taken from; a node with no
		// opacity at all is not seen
		if grouped {
			if opacity != 0 {
				if opacity < 1 {
					composite.Opacity = opacity
				}
				originalDst1.DrawGroup(dst, composite)
			}
			dst = originalDst1 // actually not used
		}
	}

	if paint {
		dst.OnNewStack(paintTask)
	} else {
		paintTask()
	}
}

// vertices are the resolved vertices computed when drawing the shape
func (svg *SVGImage) drawMarkers(dst gfxcore.Canvas, vertices []vertex, node *svgNode, dims drawingDims, paint bool) {
	const (
		start uint8 = iota
		mid
		end
	)
	commonMarker := svg.definitions.markers[node.markerID]

	// [start, mid, end] defautling to the common marker
	markers := [3]*marker{commonMarker, commonMarker, commonMarker}

	if marker := svg.definitions.markers[node.markerStartID]; marker != nil {
		markers[start] = marker
	}
	if marker := svg.definitions.markers[node.markerMidID]; marker != nil {
		markers[mid] = marker
	}
	if marker := svg.definitions.markers[node.markerEndID]; marker != nil {
		markers[end] = marker
	}

	for i, vertex := range vertices {
		if svg.budgetExhausted() {
			return
		}
		position := mid
		if i == 0 {
			position = start
		} else if i == len(vertices)-1 {
			position = end
		}

		marker := markers[position]
		if marker == nil {
			continue
		}

		// calculate position, scale and clipping
		var (
			clipBox        Rectangle
			scaleX, scaleY Fl
		)
		translateX, translateY := dims.point(marker.refX, marker.refY)
		markerWidth, markerHeight := dims.point(marker.markerWidth, marker.markerHeight)
		if vb := marker.viewbox; vb != nil {
			scaleX, scaleY, _, _ = marker.preserveAspectRatio.resolveTransforms(markerWidth, markerHeight, marker.viewbox, &point{translateX, translateY})

			clipViewbox := *vb
			if marker.viewbox != nil {
				clipViewbox = *marker.viewbox
			}

			xPosition, yPosition := marker.preserveAspectRatio.xPosition, marker.preserveAspectRatio.yPosition

			if xPosition == "mid" {
				clipViewbox.X += (clipViewbox.Width - markerWidth/scaleX) / 2
			} else if xPosition == "max" {
				clipViewbox.X += clipViewbox.Width - markerWidth/scaleX
			}

			if yPosition == "mid" {
				clipViewbox.Y += (clipViewbox.Height - markerHeight/scaleY) / 2
			} else if yPosition == "max" {
				clipViewbox.Y += clipViewbox.Height - markerHeight/scaleY
			}

			clipBox = Rectangle{clipViewbox.X, clipViewbox.Y, markerWidth / scaleX, markerHeight / scaleY}
		} else {
			scaleX, scaleY = 1, 1
			clipBox = Rectangle{0, 0, markerWidth, markerHeight}
		}

		// scale
		if !marker.isUnitsUserSpace {
			scale := dims.length(node.attributes.strokeWidth)
			scaleX *= scale
			scaleY *= scale
		}

		// override angle
		angle := vertex.angle
		nodeAngle := marker.orient
		if !nodeAngle.auto {
			angle = nodeAngle.angle * math.Pi / 180 // convert from degrees to radians
		} else if nodeAngle.startReverse && position == start {
			angle += math.Pi
		}

		// a marker whose content is itself marked can recurse; skip
		// re-entrant instances (keyed by identity: markers have no
		// stable id here)
		refKey := fmt.Sprintf("marker:%p", marker)
		if !svg.enterRef(refKey, "marker") {
			continue
		}

		// draw marker path; the marker transform is
		// translate(vertex) · rotate(angle) · scale · translate(-ref),
		// matching the SVG spec (and resvg's marker resolution)
		for _, child := range marker.children {
			if svg.budgetExhausted() {
				break
			}
			dst.OnNewStack(func() {
				dst.State().Transform(matrix.Translation(vertex.x, vertex.y))
				if angle != 0 {
					dst.State().Transform(matrix.Rotation(angle))
				}
				dst.State().Transform(matrix.Scaling(scaleX, scaleY))
				dst.State().Transform(matrix.Translation(-translateX, -translateY))

				overflow := marker.overflow
				if overflow == "hidden" || overflow == "scroll" {
					dst.Rectangle(clipBox.X, clipBox.Y, clipBox.Width, clipBox.Height)
					dst.State().Clip(false)
				}

				svg.drawNode(dst, child, dims, paint)
			})
		}
		svg.leaveRef(refKey)
	}
}

// compute scale and translation needed to preserve ratio.
// [translate] is optional :
// for marker tags, [translate] should be the resolved refX and refY values;
// otherwise, it should be nil.
func (pr preserveAspectRatio) resolveTransforms(width, height Fl, viewbox *Rectangle, translate *point) (scaleX, scaleY, translateX, translateY Fl) {
	if viewbox == nil {
		return 1, 1, 0, 0
	}

	viewboxWidth, viewboxHeight := viewbox.Width, viewbox.Height

	scaleX, scaleY = 1, 1
	if viewboxWidth != 0 {
		scaleX = width / viewboxWidth
	}
	if viewboxHeight != 0 {
		scaleY = height / viewboxHeight
	}

	if !pr.none {
		if pr.slice {
			scaleX = max(scaleX, scaleY)
		} else {
			scaleX = min(scaleX, scaleY)
		}
		scaleY = scaleX
	}

	if translate != nil {
		translateX, translateY = translate.x, translate.y
	} else {
		if pr.xPosition == "mid" {
			translateX = (width - viewboxWidth*scaleX) / 2
		} else if pr.xPosition == "max" {
			translateX = width - viewboxWidth*scaleX
		}

		if pr.yPosition == "mid" {
			translateY += (height - viewboxHeight*scaleY) / 2
		} else if pr.yPosition == "max" {
			translateY += height - viewboxHeight*scaleY
		}
	}

	translateX -= viewbox.X * scaleX
	translateY -= viewbox.Y * scaleY

	return
}

// resolve units and compose the transforms
func aggregateTransforms(transforms []transform, dims *drawingDims, diagonal Fl) matrix.Transform {
	// aggregate the transformations
	mat := matrix.Identity()
	for _, transform := range transforms {
		transform.applyTo(&mat, dims, diagonal)
	}
	return mat
}

// applyTransformWithOrigin applies the user-coordinate transforms,
// optionally pivoted around (originX, originY) per CSS transform-origin /
// SVG 2. The pivot is encoded as Translate(origin) · transforms ·
// Translate(-origin), which aggregates with the user's transforms before
// hitting the backend so it's a single Transform call.
func applyTransformWithOrigin(dst gfxcore.Canvas, transforms []transform, dims drawingDims, hasOrigin bool, originX, originY Fl) {
	if len(transforms) == 0 && !hasOrigin {
		return
	}

	mat := aggregateTransforms(transforms, &dims, dims.diagonal)
	if hasOrigin && (originX != 0 || originY != 0) {
		// Compose: Translate(origin) · mat · Translate(-origin).
		shifted := matrix.Translation(originX, originY)
		shifted.RightMultBy(mat)
		shifted.RightMultBy(matrix.Translation(-originX, -originY))
		mat = shifted
	}
	if mat.Determinant() != 0 {
		dst.State().Transform(mat)
	}
}

// applyFilters applies the filters that move the node, and returns the blend
// mode of the one that mixes it.
func applyFilters(dst gfxcore.Canvas, filters []filter, node *svgNode, dims drawingDims) (blend gfxcore.BlendMode) {
	for _, filter := range filters {
		switch filter := filter.(type) {
		case filterOffset:
			var dx, dy Fl
			if filter.isUnitsBBox {
				bbox, _ := node.resolveBoundingBox(dims, true)
				dx = filter.dx.Resolve(&dims.Dims, 1) * bbox.Width
				dy = filter.dy.Resolve(&dims.Dims, 1) * bbox.Height
			} else {
				dx, dy = dims.point(filter.dx, filter.dy)
			}
			dst.State().Transform(matrix.New(1, 0, 0, 1, dx, dy))
		case filterBlend:
			blend, _ = gfxcore.ParseBlendMode(string(filter))
		}
	}
	return blend
}

// enterRef marks a clip-path, mask or marker reference as being applied, and
// reports whether it may proceed: a reference already active is a cycle,
// ignored per resvg's behavior.
//
// key identifies the reference, label names it in the warning. They differ
// for markers, which have no stable id and so are keyed by pointer identity —
// an address is meaningless to a reader and, being nondeterministic, made
// otherwise identical runs produce different logs.
func (svg *SVGImage) enterRef(key, label string) bool {
	if svg.activeRefs[key] {
		logger.Warnf("ignoring recursive reference %s", label)
		return false
	}
	if len(svg.activeRefs) >= limits.ReferenceDepth {
		logger.Warnf("SVG: references nested more than %d deep, ignoring %s", limits.ReferenceDepth, label)
		return false
	}
	if svg.activeRefs == nil {
		svg.activeRefs = map[string]bool{}
	}
	svg.activeRefs[key] = true
	return true
}

func (svg *SVGImage) leaveRef(key string) { delete(svg.activeRefs, key) }

func (svg *SVGImage) applyClipPath(dst gfxcore.Canvas, clipPath *clipPath, node *svgNode, dims drawingDims) {
	oldCtm := dst.State().GetTransform()

	if clipPath.isUnitsBBox {
		x, y := dims.point(node.attributes.x, node.attributes.y)
		width, height := dims.point(node.attributes.width, node.attributes.height)
		dst.State().Transform(matrix.New(width, 0, 0, height, x, y))
	}

	svg.drawNode(dst, &clipPath.svgNode, dims, false)

	// clip-rule applies per child of the <clipPath> (inherited, so a
	// rule set on the <clipPath> or above reaches them). The backend
	// clips the union of the children with one rule, so a uniform rule
	// is honored exactly; genuinely mixed rules fall back to the first
	// child's, with a warning.
	evenOdd := clipPath.isClipUseEvenOdd
	if children := clipPath.svgNode.children; len(children) != 0 {
		evenOdd = children[0].attributes.isClipEvenOdd
		for _, child := range children[1:] {
			if child.attributes.isClipEvenOdd != evenOdd {
				logger.Warn("mixed clip-rule values in <clipPath> are not supported; using the first child's rule")
				break
			}
		}
	}

	// At least set the clipping area to an empty path, so that it’s
	// totally clipped when the clipping path is empty.
	dst.Rectangle(0, 0, 0, 0)
	dst.State().Clip(evenOdd)
	newCtm := dst.State().GetTransform()
	if err := newCtm.Invert(); err == nil {
		dst.State().Transform(matrix.Mul(oldCtm, newCtm))
	}
}

// drawMask draws the mask of a node in a group of dst, and returns it with
// the way it masks: what [gfxcore.Composite] takes.
func (svg *SVGImage) drawMask(dst gfxcore.Canvas, mask mask, node *svgNode, dims drawingDims) (gfxcore.Canvas, gfxcore.MaskMode) {
	bbox, hasBBox := node.maskBox(dims)
	if (mask.isUnitsBBox || mask.isContentUnitsBBox) && !(hasBBox && bbox.Width > 0 && bbox.Height > 0) {
		// Units of a bounding box that does not exist: the element is not
		// drawn. A mask with nothing in it hides everything.
		return dst.NewGroup(0, 0, 0, 0), gfxcore.MaskAlpha
	}

	// the region the mask is cut to
	var x, y, width, height Fl
	if mask.isUnitsBBox {
		x = bbox.X + fraction(mask.x)*bbox.Width
		y = bbox.Y + fraction(mask.y)*bbox.Height
		width = fraction(mask.width) * bbox.Width
		height = fraction(mask.height) * bbox.Height
	} else {
		x = mask.x.Resolve(&dims.Dims, dims.ViewportW)
		y = mask.y.Resolve(&dims.Dims, dims.ViewportH)
		width = mask.width.Resolve(&dims.Dims, dims.ViewportW)
		height = mask.height.Resolve(&dims.Dims, dims.ViewportH)
	}

	alpha := dst.NewGroup(x, y, width, height)
	alpha.OnNewStack(func() {
		alpha.Rectangle(x, y, width, height)
		alpha.State().Clip(false)
		if mask.isContentUnitsBBox {
			alpha.State().Transform(matrix.New(bbox.Width, 0, 0, bbox.Height, bbox.X, bbox.Y))
		}
		// the node of the mask is drawn like a group: a mask may itself be
		// masked, clipped, or partly transparent
		content := mask.svgNode
		content.x, content.y, content.width, content.height = Value{}, Value{}, Value{}, Value{}
		svg.drawNode(alpha, &content, dims, true)
	})

	if mask.isAlpha {
		return alpha, gfxcore.MaskAlpha
	}
	return alpha, gfxcore.MaskLuminance
}

// fraction returns a value in units of a bounding box: a number, or a
// percentage of 1.
func fraction(v Value) Fl {
	if v.U == Perc {
		return v.V / 100
	}
	return v.V
}

// maskBox returns the bounding box masks and clip paths are relative to: the
// one of the content of the node, without its stroke, or of its children for
// a group.
func (node *svgNode) maskBox(dims drawingDims) (Rectangle, bool) {
	if box, ok := node.resolveBoundingBox(dims, false); ok {
		return box, true
	}
	out, found := emptyBbox, false
	for _, child := range node.children {
		if box, ok := child.maskBox(dims); ok {
			out.union(box)
			found = true
		}
	}
	return out, found
}

// Image is an image referenced by an <image> element. Images are loaded
// and drawn by the host, through [ImageLoader]: this package knows no image
// format.
type Image interface {
	// IntrinsicSize returns the natural size of the image and its
	// width / height ratio, each nil when the image has none.
	// fontSize resolves the font relative units an image may be sized in.
	IntrinsicSize(fontSize Fl) (width, height, ratio pr.MaybeFloat)

	// Draw draws the image on dst, in the rectangle (0, 0, width, height).
	// rendering is the value of the image-rendering property. text is the
	// renderer of the document the image is in, which an image made of
	// text (another SVG) needs; it may be ignored.
	Draw(dst gfxcore.Canvas, width, height Fl, rendering string, text TextRenderer)
}

// Resource is an external resource, as returned by a [Fetcher].
type Resource = utils.Resource

// Fetcher fetches an external resource: a stylesheet, or another SVG
// document referenced by a <use> element. It is supplied by the host, and
// is the policy deciding what may be read.
type Fetcher = utils.Fetcher

// ImageLoader is used to resolve and process image url found in SVG files.
type ImageLoader = func(url string) (Image, error)

// Parse parsed the given SVG source data. Warnings are
// logged for unsupported elements.
// An error is returned for invalid documents.
// `baseURL` is used as base path for url resources.
// `urlFetcher` is required to handle linked SVG documents like in <use> tags.
// `imageLoader` is required to handle inner images.
//
// The document is an SVG image: its @media rules are evaluated against its
// own viewport, as in browsers (see [ParseNode]).
func Parse(svg io.Reader, baseURL string, imageLoader ImageLoader, urlFetcher Fetcher) (*SVGImage, error) {
	root, err := html.Parse(svg)
	if err != nil {
		return nil, err
	}

	return ParseNode(root, baseURL, imageLoader, urlFetcher, "", nil)
}

// ParseNode is the same as Parse but works with an already parsed
// svg input.
// inheritedColor is an optional CSS color string from the HTML parent context,
// used to resolve "currentColor" SVG attribute values.
//
// host is the device of the document an inline <svg> belongs to, against
// which its @media rules are evaluated. nil means an SVG image, whose
// @media rules are evaluated against its own viewport: first at its
// intrinsic size, then, if a rule tests the size, at the size given to
// [SVGImage.Draw]. root is then kept, and must not be modified.
func ParseNode(root *html.Node, baseURL string, imageLoader ImageLoader, urlFetcher Fetcher, inheritedColor string, host *Device) (*SVGImage, error) {
	if host == nil && urlFetcher != nil {
		// an SVG image may be parsed again at draw time: fetch resources once
		urlFetcher = (&memoFetcher{fetcher: urlFetcher}).fetch
	}
	out, tree, err := parseNode(root, baseURL, imageLoader, urlFetcher, inheritedColor, host)
	if err != nil {
		return nil, err
	}
	if host == nil && len(tree.sizeQueries) != 0 {
		out.resize = &resizer{
			root: root, baseURL: baseURL, imageLoader: imageLoader, urlFetcher: urlFetcher,
			inheritedColor: inheritedColor, device: tree.device,
			original: newVariant(out, tree),
		}
	}
	return out, nil
}

// parseNode parses an SVG for device, nil meaning the intrinsic one.
func parseNode(root *html.Node, baseURL string, imageLoader ImageLoader, urlFetcher Fetcher, inheritedColor string, device *Device) (*SVGImage, *svgContext, error) {
	tree, err := newSVGContext(root, baseURL, urlFetcher, inheritedColor, device)
	if err != nil {
		return nil, nil, err
	}

	tree.imageLoader = imageLoader

	var out SVGImage
	out.definitions = newDefinitions()
	// Build the drawable items by parsing attributes
	out.root, err = tree.processNode(tree.root, out.definitions)
	if err != nil {
		return nil, nil, err
	}
	// what Draw may draw is relative to the document (see limits.DrawsPerElement)
	out.elements = tree.elements + tree.useBudget.nodes

	return &out, tree, nil
}

// svgNode is a node in a drawable SVG tree
type svgNode struct {
	graphicContent drawable
	children       []*svgNode
	attributes
}

// drawingDims stores the configuration to use
// when drawing
// Dims carries everything a CSS length may resolve against. It is exported
// because intrinsic sizing happens outside the drawing pass, where only some
// of it is known — see [Value.Resolve].
type Dims struct {
	// FontSize is the element's computed font size (em, ex).
	FontSize Fl

	// RootFontSize is the <svg> element's own font size, the reference for
	// rem. It is set once and never updated as the cascade descends, which
	// is what makes rem differ from em.
	RootFontSize Fl

	// ViewportW, ViewportH are the SVG viewport: either the root viewBox
	// width and height, or the size requested by the caller when no viewBox
	// is provided. Viewport units resolve against them.
	ViewportW, ViewportH Fl

	// diagonal caches norm(ViewportW, ViewportH)/sqrt(2), the reference a
	// bare percentage length resolves against per SVG's "normalized
	// diagonal" rule. Kept here because it derives from the viewport alone.
	diagonal Fl
}

// SetViewport records the viewport and refreshes the cached diagonal.
func (d *Dims) SetViewport(w, h Fl) {
	d.ViewportW, d.ViewportH = w, h
	d.diagonal = Fl(math.Hypot(float64(w), float64(h)) / math.Sqrt2)
}

// Length resolves a length whose percentages refer to the normalized
// diagonal, which is the default for lengths with no dominant axis.
func (d *Dims) Length(v Value) Fl { return v.Resolve(d, d.diagonal) }

// Point resolves an (x, y) or (width, height) couple, each percentage
// against its own axis.
func (d *Dims) Point(xv, yv Value) (x, y Fl) {
	return xv.Resolve(d, d.ViewportW), yv.Resolve(d, d.ViewportH)
}

type drawingDims struct {
	Dims

	// width and height as requested by the user
	// when calling Draw.
	concreteWidth, concreteHeight Fl

	// cached value of norm(concreteWidth, concreteHeight) / sqrt(2)
	normalizedDiagonal Fl
}

// update `innerDiagonal` and `normalizedDiagonal`
func (dims *drawingDims) setupDiagonal() {
	dims.SetViewport(dims.ViewportW, dims.ViewportH)
	dims.normalizedDiagonal = Fl(math.Hypot(float64(dims.concreteWidth), float64(dims.concreteHeight)) / math.Sqrt2)
}

// point and length forward to the exported helpers on Dims; element
// renderers outside this package use those directly.
func (dims drawingDims) point(xv, yv Value) (x, y Fl) { return dims.Dims.Point(xv, yv) }

func (dims drawingDims) length(length Value) Fl { return dims.Dims.Length(length) }

// box is a shared type for dimensions
// found in several SVG elements, which may be expressed with units
type box struct {
	x, y, width, height Value
}

// attributes stores the SVG attributes
// shared by all node types in the final rendering tree
type attributes struct {
	viewbox *Rectangle

	transforms []transform
	// transformOriginX/Y is the pivot for `transforms` (CSS
	// transform-origin / SVG 2). When hasTransformOrigin is true,
	// applyTransform composes Translate(origin) · transforms ·
	// Translate(-origin). The flag distinguishes "no origin set"
	// from "origin set to (0, 0)".
	transformOriginX, transformOriginY Fl
	hasTransformOrigin                 bool

	clipPathID, maskID, filterID string
	// blendMode is the value of mix-blend-mode, empty for normal.
	blendMode                                         string
	markerID, markerStartID, markerMidID, markerEndID string

	dashArray []Value

	fill, stroke painter // fill default to black, stroke to nothing

	box

	fontSize      Value
	strokeWidth   Value // default to 1px
	strokeOptions gfxcore.StrokeOptions

	dashOffset Value

	opacity, strokeOpacity, fillOpacity Fl // default to 1

	isFillEvenOdd bool
	// isClipEvenOdd is the (inherited) clip-rule, used when this node
	// is a child of a <clipPath>
	isClipEvenOdd    bool
	display, visible bool
}

func (tree *svgContext) processNode(node *cascadedNode, defs definitions) (*svgNode, error) {
	var children []*svgNode
	for _, c := range node.children {
		child, err := tree.processNode(c, defs)
		if err != nil {
			// a broken element (unloadable image, malformed numbers, ...)
			// only invalidates its own subtree, not the document
			// (matching resvg); an error on the root stays fatal.
			logger.Warnf("skipping invalid element <%s>: %s", c.tag, err)
			continue
		}
		if child == nil {
			continue // do not add useless node to the tree
		}
		children = append(children, child)
	}

	// actual processing of the node, with the following cases
	//	- node used as definition, extracted from the svg tree
	//	- graphic element to display -> processGraphicNode

	id := node.attrs["id"]
	switch node.tag {
	case "filter":
		filters, err := newFilter(node)
		if err != nil {
			return nil, err
		}
		defs.filters[id] = filters
	case "clipPath":
		cp, err := newClipPath(node, children)
		if err != nil {
			return nil, err
		}
		defs.clipPaths[id] = cp
	case "mask":
		ma, err := newMask(node, children)
		if err != nil {
			return nil, err
		}
		defs.masks[id] = ma
	case "marker":
		ma, err := newMarker(node, children)
		if err != nil {
			return nil, err
		}
		defs.markers[id] = ma
	case "linearGradient", "radialGradient":
		grad, err := newGradient(node)
		if err != nil {
			return nil, err
		}
		defs.paintServers[id] = grad
	case "pattern":
		pat, err := newPattern(node, children)
		if err != nil {
			return nil, err
		}
		defs.paintServers[id] = pat
	case "use": // special case
		resolved, err := tree.resolveUse(node, defs)
		if err != nil {
			return nil, err
		}
		return resolved, nil
	case "defs", "symbol":
		// children has been processed and registred (defs), or are
		// rendered only via <use> (symbol). In either case, the node
		// itself is not drawn directly. Per WP commit 1043ffce.
	default:
		out, err := tree.processGraphicNode(node, children)
		if err != nil {
			return nil, err
		}
		// register node with id
		if id != "" {
			defs.nodes[id] = out
		}
		return out, nil
	}

	return nil, nil
}

// process a node to be displayed by building its content
func (tree *svgContext) processGraphicNode(node *cascadedNode, children []*svgNode) (*svgNode, error) {
	out := svgNode{children: children}

	var (
		err    error
		isText bool
	)
	switch node.tag {
	case "circle", "ellipse":
		out.graphicContent, err = newEllipse(node, tree)
	case "image":
		out.graphicContent, err = newImage(node, tree)
	case "line":
		out.graphicContent, err = newLine(node, tree)
	case "path":
		out.graphicContent, err = newPath(node, tree)
	case "polyline":
		out.graphicContent, err = newPolyline(node, tree)
	case "polygon":
		out.graphicContent, err = newPolygon(node, tree)
	case "rect":
		out.graphicContent, err = newRect(node, tree)
	case "svg":
		out.graphicContent, err = newSvg(node, tree)
	case "a", "text", "textPath", "tspan":
		out.graphicContent = &pendingText{node: node}
		isText = true
	}

	if err != nil {
		return nil, fmt.Errorf("invalid element %s: %s", node.tag, err)
	}

	if isText {
		err = node.attrs.parseCommonAttributesForText(node.props, &out.attributes)
	} else {
		err = node.attrs.parseCommonAttributes(node.props, &out.attributes)
	}

	if err != nil {
		return nil, err
	}

	return &out, nil
}

// parseBox reads the x, y, width and height attributes. An invalid value is
// ignored, with a warning, as for any presentation attribute (SVG 2 §6.8):
// the default applies. "auto" (SVG 2, for width and height) is the default.
func (na nodeAttributes) parseBox(out *box) error {
	parse := func(name string) Value {
		attr := na[name]
		if strings.EqualFold(strings.TrimSpace(attr), "auto") {
			return Value{}
		}
		v, err := parseValue(attr)
		if err != nil {
			logger.Warnf("SVG: ignoring invalid %s attribute %q: %s", name, attr, err)
			return Value{}
		}
		return v
	}
	out.x, out.y = parse("x"), parse("y")
	out.width, out.height = parse("width"), parse("height")
	return nil
}

func (na nodeAttributes) parseCommonAttributes(props pr.Properties, out *attributes) error {
	err := na.parseBox(&out.box)
	if err != nil {
		return err
	}
	err = na.parseSharedAttributes(props, out)
	return err
}

func (na nodeAttributes) parseSharedAttributes(props pr.Properties, out *attributes) error {
	var err error
	out.viewbox, err = na.viewBox()
	if err != nil {
		return err
	}

	out.fontSize, err = na.fontSize()
	if err != nil {
		return err
	}
	out.strokeWidth = lengthOf(props, PStrokeWidth)
	if err != nil {
		return err
	}

	out.opacity, err = parseOpacity(na["opacity"])
	if err != nil {
		return err
	}
	out.strokeOpacity = floatOf(props, PStrokeOpacity)
	out.fillOpacity = floatOf(props, PFillOpacity)

	out.transforms, err = parseTransform(na["transform"])
	if err != nil {
		return err
	}
	if origin := strings.TrimSpace(na["transform-origin"]); origin != "" {
		vs, perr := parseOriginValues(origin)
		if perr != nil {
			return perr
		}
		// Per CSS Transforms / SVG 2: one value defaults Y to "center"
		// (50%); we lack box dimensions here, so fall back to 0 — most
		// real authoring uses the two-value form.
		if len(vs) >= 1 {
			out.transformOriginX = vs[0].V
			out.hasTransformOrigin = true
		}
		if len(vs) >= 2 {
			out.transformOriginY = vs[1].V
		}
	}

	out.stroke, err = newPainter(stringOf(props, PStroke))
	if err != nil {
		return err
	}
	out.fill, err = newPainter(stringOf(props, PFill))
	if err != nil {
		return err
	}
	out.isFillEvenOdd = stringOf(props, PFillRule) == "evenodd"
	out.isClipEvenOdd = stringOf(props, PClipRule) == "evenodd"

	out.dashOffset = lengthOf(props, PStrokeDashoffset)
	// Read from the typed store: the engine already parsed this list, and
	// re-parsing the serialized form is where the separators were lost —
	// "4 2 1" round-trips to "421" once whitespace is removed.
	out.dashArray = dashArrayOf(props)

	out.strokeOptions.LineCap = lineCapOf(props)
	out.strokeOptions.LineJoin = lineJoinOf(props)
	out.strokeOptions.MiterLimit = floatOf(props, PStrokeMiterlimit)

	out.filterID = referenceOf(props, PFilter)
	out.clipPathID = referenceOf(props, PClipPath)
	out.maskID = referenceOf(props, PMask)
	if mode := KeywordOf(props, PMixBlendMode); mode != "normal" {
		out.blendMode = mode
	}

	out.markerID = parseURLFragment(na["marker"])
	out.markerStartID = referenceOf(props, PMarkerStart)
	out.markerMidID = referenceOf(props, PMarkerMid)
	out.markerEndID = referenceOf(props, PMarkerEnd)

	out.display = na.display()
	out.visible = na.visible()
	return nil
}

// does not parse "x" and "y" fields
func (na nodeAttributes) parseCommonAttributesForText(props pr.Properties, out *attributes) error {
	var err error
	out.width, err = parseValue(na["width"])
	if err != nil {
		return err
	}
	out.height, err = parseValue(na["height"])
	if err != nil {
		return err
	}

	err = na.parseSharedAttributes(props, out)
	return err
}

// originKeywords are the positional keywords transform-origin accepts
// alongside lengths, as percentages of the reference box (CSS Transforms).
var originKeywords = map[string]Value{
	"left": {U: Perc, V: 0}, "top": {U: Perc, V: 0},
	"center": {U: Perc, V: 50},
	"right":  {U: Perc, V: 100}, "bottom": {U: Perc, V: 100},
}

// parseOriginValues parses a transform-origin value list, which may mix
// lengths, percentages and positional keywords. parseValue handles lengths
// only and rejects keywords, so they are resolved here first.
func parseOriginValues(s string) ([]Value, error) {
	fields := strings.Fields(s)
	out := make([]Value, 0, len(fields))
	for _, f := range fields {
		if v, isKeyword := originKeywords[strings.ToLower(f)]; isKeyword {
			out = append(out, v)
			continue
		}
		v, err := parseValue(f)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// pendingText defers building a <text>/<tspan> renderer until the image is
// drawn, because the TextRenderer is supplied to Draw, not to Parse. It is
// also what keeps this package from naming the text implementation.
type pendingText struct {
	node  *cascadedNode
	built drawable // nil until materialise runs
}

// materialise asks the renderer for the real content, once.
func (p *pendingText) materialise(r TextRenderer) drawable {
	if p.built != nil {
		return p.built
	}
	d, err := r.NewSpan(p.node.text, p.node.attrs, p.node.props, p.node.tag == "text")
	switch {
	case err != nil:
		// Report it. Building text moved from parse time to draw time, so
		// this error no longer surfaces through processNode's message — and
		// an element dropped without a word is the failure mode this suite
		// exists to eliminate.
		logger.Warnf("skipping invalid element <%s>: %s", p.node.tag, err)
		p.built = noContent{}
	case d == nil:
		p.built = noContent{}
	default:
		p.built = external{d}
	}
	return p.built
}

// noContent draws nothing, for a text element with no renderer configured.
type noContent struct{}

func (noContent) draw(gfxcore.Canvas, *attributes, *SVGImage, drawingDims) []vertex { return nil }
func (noContent) boundingBox(*attributes, drawingDims) (Rectangle, bool) {
	return Rectangle{}, false
}

func (p *pendingText) draw(dst gfxcore.Canvas, a *attributes, img *SVGImage, d drawingDims) []vertex {
	return p.materialise(img.renderer()).draw(dst, a, img, d)
}

func (p *pendingText) boundingBox(a *attributes, d drawingDims) (Rectangle, bool) {
	if p.built == nil {
		return Rectangle{}, false
	}
	return p.built.boundingBox(a, d)
}

// materialiseText builds every deferred text span in the tree.
func (svg *SVGImage) materialiseText(node *svgNode) {
	if node == nil {
		return
	}
	if pending, ok := node.graphicContent.(*pendingText); ok {
		pending.materialise(svg.renderer())
	}
	for _, child := range node.children {
		svg.materialiseText(child)
	}
}

// renderer returns the configured text renderer, defaulting to the one that
// draws nothing.
func (svg *SVGImage) renderer() TextRenderer {
	if svg.textRenderer == nil {
		svg.textRenderer = &noTextRenderer{}
	}
	return svg.textRenderer
}

// dashArrayOf returns the validated stroke-dasharray, or nil for "none".
func dashArrayOf(props pr.Properties) []Value {
	dims, ok := props[PStrokeDasharray].(pr.Values)
	if !ok || len(dims) == 0 {
		return nil
	}
	out := make([]Value, len(dims))
	for i, d := range dims {
		out[i] = Value{U: d.Unit, V: Fl(d.Value)}
	}
	return out
}
