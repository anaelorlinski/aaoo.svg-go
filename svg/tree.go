package svg

import (
	"bytes"
	"errors"
	"io"
	"maps"
	"regexp"
	"sort"
	"strings"

	pa "github.com/anaelorlinski/aaoo.cssengine-go/parser"
	pr "github.com/anaelorlinski/aaoo.cssengine-go/properties"
	"github.com/anaelorlinski/aaoo.cssengine-go/stylesheet"
	"github.com/anaelorlinski/aaoo.cssengine-go/validation"
	"github.com/anaelorlinski/aaoo.svg-go/internal/utils"
	"github.com/anaelorlinski/aaoo.svg-go/logger"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// convert from html nodes to an intermediate svg tree

// svgContext is an intermediated representation of an SVG file,
// where CSS has been applied, and text has been processed
type svgContext struct {
	root *cascadedNode // with tag svg

	baseURL     string
	imageLoader ImageLoader
	urlFetcher  Fetcher

	// the medium @media rules are evaluated against
	device Device
	// the media queries testing the size of device evaluated by the
	// stylesheets of the document and of the documents it references
	// (see [stylesheet.Report.SizeQueries]): the styles applied depend on
	// the size of device when there are some
	sizeQueries [][]stylesheet.MediaQuery
	queriesOf   map[*svgContext]bool // the documents whose sizeQueries were added

	// to handle use tags
	defs map[string]*cascadedNode
	// ID of the current <use> target being resolved,
	// to prevent infinite recursion
	inUseIDs utils.Set
	// bounds the <use> expansion of the document and of the documents it
	// references through <use>, which share it
	useBudget *useBudget
	// the documents being referenced by external <use>, outermost first
	// (this document last), to refuse cycles
	useChain []string
	// the elements of the document
	elements int

	// cache
	pathParser pathParser
}

// isCSSWideKeyword reports whether value is a CSS-wide keyword.
func isCSSWideKeyword(value string) bool {
	switch strings.ToLower(value) {
	case "inherit", "initial", "unset", "revert", "revert-layer":
		return true
	}
	return false
}

// findSVGRoot returns the root <svg> element of the document, which is not
// always its first element.
func findSVGRoot(root *html.Node) (*utils.HTMLNode, error) {
	iter := utils.NewHtmlIterator(root, atom.Svg)
	if iter.HasNext() {
		return iter.Next(), nil
	}
	// accept a namespace-prefixed root (<svg:svg xmlns:svg="...">),
	// which x/net/html keeps as a literal "svg:svg" element
	all := utils.NewHtmlIterator(root)
	for all.HasNext() {
		if n := all.Next(); strings.HasSuffix(n.Data, ":svg") {
			return n, nil
		}
	}
	return nil, errors.New("missing <svg> element")
}

// intrinsicDevice returns the medium of an SVG image before it is drawn:
// its viewport at the size given by the width, height and viewBox
// attributes of its root (in absolute or font-relative units), as browsers
// evaluate the @media rules of an SVG image against its own viewport.
// Without a size, the default object size of CSS (300×150) applies, its
// height following the viewBox ratio if any.
func intrinsicDevice(svgRoot *utils.HTMLNode) Device {
	attr := func(name string) string {
		for _, a := range svgRoot.Attr {
			if strings.EqualFold(a.Key, name) {
				return a.Val
			}
		}
		return ""
	}
	dims := Dims{FontSize: defaultFontSize, RootFontSize: defaultFontSize}
	length := func(name string) (Fl, bool) {
		v, err := parseValue(attr(name))
		if err != nil || v.U == 0 || v.U == Perc {
			return 0, false
		}
		l := v.Resolve(&dims, 0)
		return l, l > 0
	}
	width, hasWidth := length("width")
	height, hasHeight := length("height")
	var ratio Fl // width / height
	if vb, err := parseViewbox(attr("viewBox")); err == nil && vb.Width > 0 && vb.Height > 0 {
		ratio = vb.Width / vb.Height
	}
	switch {
	case hasWidth && hasHeight:
	case hasWidth && ratio != 0:
		height = width / ratio
	case hasHeight && ratio != 0:
		width = height * ratio
	default:
		if !hasWidth {
			width = 300
		}
		if !hasHeight {
			height = 150
			if ratio != 0 {
				height = width / ratio
			}
		}
	}
	return Device{Width: width, Height: height}
}

// newSVGContextReader parses the html [rootText]
// and call newSVGContext
func newSVGContextReader(rootText io.Reader, baseURL string, urlFetcher Fetcher, device *Device) (*svgContext, error) {
	root, err := html.Parse(rootText)
	if err != nil {
		return nil, err
	}

	return newSVGContext(root, baseURL, urlFetcher, "", device)
}

// newSVGContext converts from the html representation to an internal,
// simplified form, suitable for post-processing.
//
// The stylesheets are processed and applied, the values
// of the CSS properties begin stored as attributes.
//
// Inheritable attributes are cascaded and 'inherit' special values are resolved.
// inheritedColor is an optional CSS color string (e.g. "#EB646F") inherited from
// the HTML parent context. When non-empty, it is used to resolve "currentColor"
// values in SVG attributes like fill and stroke.
//
// device is the medium @media rules are evaluated against; nil means the
// viewport of the SVG at its intrinsic size (see [intrinsicDevice]).
func newSVGContext(root *html.Node, baseURL string, urlFetcher Fetcher, inheritedColor string, device *Device) (*svgContext, error) {
	svgRoot, err := findSVGRoot(root)
	if err != nil {
		return nil, err
	}

	var out svgContext
	if device != nil {
		out.device = *device
	} else {
		out.device = intrinsicDevice(svgRoot)
	}

	stylesheets, trefs, mediaQueries := fetchStyleAndTextRefs(svgRoot, out.device)
	styleRules, report := parseStylesheets(stylesheets, baseURL, urlFetcher, out.device)
	out.sizeQueries = append(mediaQueries, report.SizeQueries...)

	// build the SVG tree and apply style attribute
	out.baseURL = baseURL
	out.urlFetcher = urlFetcher
	out.defs = make(map[string]*cascadedNode)
	out.inUseIDs = make(utils.Set)
	out.useBudget = new(useBudget)
	if baseURL != "" {
		out.useChain = []string{withoutFragment(baseURL)}
	}

	// may return nil to discard the node; parentAttrs are the cascaded
	// attributes of the parent
	var buildTree func(node *html.Node, parentAttrs nodeAttributes) *cascadedNode

	buildTree = func(node *html.Node, parentAttrs nodeAttributes) *cascadedNode {
		// text is handled by the parent
		// style elements are no longer useful
		if node.Type != html.ElementNode || node.DataAtom == atom.Style {
			return nil
		}
		out.elements++

		// the attributes and the style rules of the node, then the cascade
		specified, _ := newNodeAttributes(node.Attr)
		specified.applyStyle(baseURL, (*html.Node)(node), &styleRules)
		cascaded, attrs, props := cascadeAttributes(specified, parentAttrs)

		nodeSVG := &cascadedNode{
			tag:       node.Data,
			text:      (*utils.HTMLNode)(node).GetChildrenText(),
			attrs:     attrs,
			props:     props,
			specified: specified,
			cascaded:  cascaded,
		}

		// recurse
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if childSVG := buildTree(child, cascaded); childSVG != nil {
				nodeSVG.children = append(nodeSVG.children, childSVG)
			}
		}

		// Fix text in text tags
		if node.Data == "text" || node.Data == "textPath" || node.Data == "a" {
			handleText(nodeSVG, true, true, trefs)
		}

		if ID := attrs["id"]; ID != "" {
			out.defs[ID] = nodeSVG
		}

		return nodeSVG
	}

	// If an inherited color is provided from the HTML context,
	// seed the parent attributes so that currentColor can resolve.
	var initialAttrs nodeAttributes
	if inheritedColor != "" {
		initialAttrs = nodeAttributes{"color": inheritedColor}
	}

	out.root = buildTree((*html.Node)(svgRoot), initialAttrs)

	out.inheritDefs()

	return &out, nil
}

// cascadeAttributes returns the attributes of a node from its specified
// ones (its attributes and style rules) and the cascaded attributes of its
// parent: the inherited attributes are added and the CSS-wide keywords
// resolved. cascaded, which its children inherit, keeps currentColor, a
// keyword computed as itself (CSS Color 4); attrs has it replaced by the
// color of the node, and props are the typed values of attrs.
//
// It runs when the tree is built, and again for the instances of a <use>,
// which inherit from the <use> (SVG 2 §5.6.2).
func cascadeAttributes(specified, parent nodeAttributes) (cascaded, attrs nodeAttributes, props pr.Properties) {
	cascaded = make(nodeAttributes, len(specified)+len(parent))
	for key, value := range specified {
		cascaded[key] = value
	}
	for key, value := range parent {
		if !notInheritedAttributes.Has(key) {
			if _, isSet := cascaded[key]; !isSet {
				cascaded[key] = value
			}
		}
	}

	// The CSS-wide keywords: 'inherit' takes the parent's value, 'initial'
	// the property's default (the attribute is deleted, so per-property
	// defaults apply: fill="initial" is black, not an empty paint), and
	// 'unset' is 'inherit' for inherited properties, 'initial' for the
	// others. A missing parent value is the default.
	for key, value := range cascaded {
		switch strings.ToLower(value) {
		case "unset", "revert", "revert-layer":
			// SVG has one origin (author) and no layers here: revert rolls
			// back to the user agent, which has nothing
			if notInheritedAttributes.Has(key) {
				delete(cascaded, key)
				continue
			}
			fallthrough
		case "inherit":
			if parentVal, has := parent[key]; has && parentVal != "" && !isCSSWideKeyword(parentVal) {
				cascaded[key] = parentVal
			} else {
				delete(cascaded, key)
			}
		case "initial":
			delete(cascaded, key)
		}
	}

	// currentColor in color is the color of the parent (resolved there);
	// in the other properties, the color of the node, possibly inside a
	// color function (color-mix(), contrast-color())
	if c, has := cascaded["color"]; has {
		cascaded["color"] = withCurrentColor(c, parent["color"])
	}
	attrs = cascaded // shared until a currentColor is replaced
	copied := false
	for key := range colorAttributes {
		if v, has := cascaded[key]; has {
			if w := withCurrentColor(v, cascaded["color"]); w != v {
				if !copied {
					attrs, copied = maps.Clone(cascaded), true
				}
				attrs[key] = w
			}
		}
	}

	// Values reaching the node through the cascade or a <style> rule are
	// validated the same way the presentation attributes were.
	props = pr.Properties{}
	syncProps(attrs, props)
	return cascaded, attrs, props
}

// Handle inheritance of different defined elements lists.
//
// Iteration is over sorted ids, not the map itself: inheritElement breaks
// reference cycles by deleting the href it has consumed, so for a cyclic
// chain (A href B, B href A) the surviving attributes depend on which node is
// entered first. Ranging a map made that depend on Go's randomised iteration
// order, so the same document rendered differently between runs — caught by
// svgtests on paint-servers/linearGradient/recursive-xlink-href-3.
func (tree *svgContext) inheritDefs() {
	ids := make([]string, 0, len(tree.defs))
	for id := range tree.defs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		element := tree.defs[id]
		if t := element.tag; t == "linearGradient" || t == "radialGradient" || t == "pattern" {
			tree.inheritElement(element)
		}
	}
}

// Recursively handle inheritance of defined element.
func (tree *svgContext) inheritElement(node *cascadedNode) {
	href := node.attrs["href"]
	url, err := utils.SafeUrljoin(tree.baseURL, href, true)
	if err != nil {
		return
	}
	delete(node.attrs, "href")

	parent := tree.defs[parseURLFragment(url)]
	if parent == nil {
		return
	}
	tree.inheritElement(parent)
	for key, value := range parent.attrs {
		if _, in := node.attrs[key]; !in {
			node.attrs[key] = value
			if node.specified != nil { // kept for a <use> instance
				node.specified[key] = value
			}
		}
	}
	// The inherited attributes are presentation attributes too, so the typed
	// store has to follow them.
	syncProps(node.attrs, node.props)
	// Inherit children too if the node has none of its own. Per SVG
	// spec, <linearGradient href="#parent">  should inherit its
	// parent's <stop> children when it has none — without this, the
	// gradient ends up with zero stops and falls back to opaque black.
	// The "no own children" guard preserves the spec rule that
	// children declared on the referencing element override the
	// referenced element's children rather than appending to them.
	if len(node.children) == 0 {
		node.children = parent.children
	}
}

// cascadedNode is a node in an SVG document.
// we use this intermediate representation to
// ease the cascading of the properties and the text handling
type cascadedNode struct {
	// size is the number of elements of the tree, once computed (see
	// [cascadedNode.subtreeSize]); 0 before
	size int

	tag  string
	text []byte
	// attrs holds every attribute as written. XML attributes (d, points,
	// viewBox, cx…) are only ever strings, so they stay here.
	attrs nodeAttributes
	// props holds the CSS properties this package registered, already
	// validated and typed by the engine. Reading them from here is what
	// removes the last of the parse-serialize-reparse round trip: the value
	// is parsed once, by the CSS engine, instead of again at every read.
	props    pr.Properties
	children []*cascadedNode

	// specified holds the attributes and style rules of the node, before
	// the cascade, and cascaded the attributes its children inherit (see
	// [cascadeAttributes]): a <use> instance is cascaded again from them
	specified, cascaded nodeAttributes
}

// returns a copy
// attrs is deepcopied, but the children and text are shallow copied
func (c *cascadedNode) copy() cascadedNode {
	out := *c
	out.attrs = make(nodeAttributes, len(c.attrs))
	for k, v := range c.attrs {
		out.attrs[k] = v
	}
	// The typed store is copied too: a <use> cascades its own presentation
	// attributes onto the copy, and that must not reach back into the
	// definition every other <use> of the same id shares.
	out.props = make(pr.Properties, len(c.props))
	for k, v := range c.props {
		out.props[k] = v
	}
	return out
}

// nodeAttributes stores the raw attributes value of a node
//
// attributes will be updated in the post processing
// step due to the cascade
type nodeAttributes map[string]string

func newNodeAttributes(attrs []html.Attribute) (nodeAttributes, pr.Properties) {
	out := make(nodeAttributes, len(attrs))
	props := pr.Properties{}
	for _, attr := range attrs {
		// A presentation attribute naming a property registered with the CSS
		// engine is validated by it. An invalid one is dropped, so the
		// inherited or initial value applies — per SVG, an unparseable
		// presentation attribute is in error and ignored. Previously it was
		// kept as a string and silently became zero further down.
		//
		// Attributes that are not CSS properties (d, points, viewBox, cx…)
		// are untouched: they were never the CSS engine's business.
		value, ok := validatePresentationAttr(attr.Key, attr.Val)
		if !ok {
			continue
		}
		if value != nil {
			props[pr.PropertyByName(attr.Key)] = value
		}
		out[attr.Key] = attr.Val
	}
	return out, props
}

// validatePresentationAttr validates a presentation attribute, returning the
// typed value when the CSS engine produced one. ok is false when the value is
// invalid and the attribute should be dropped. Unregistered names always pass
// with a nil value: they are XML attributes, not CSS properties.
func validatePresentationAttr(name, value string) (_ pr.CssProperty, ok bool) {
	prop, isRegistered := pr.RegisteredProperty(name)
	if !isRegistered {
		return nil, true
	}
	tokens := pa.RemoveWhitespace(pa.Tokenize([]byte(value), true))
	if len(tokens) == 0 {
		return nil, true
	}
	if isCSSWideKeyword(value) {
		return nil, true
	}
	v, err := validation.ValidateKnown(prop, tokens, "")
	if err != nil || v == nil {
		logger.Warnf("ignoring invalid %s attribute: %q", name, value)
		return nil, false
	}
	typed, _ := v.(pr.CssProperty)
	return typed, true
}

func (na nodeAttributes) viewBox() (*Rectangle, error) {
	if attrValue := na["viewBox"]; attrValue != "" {
		v, err := parseViewbox(attrValue)
		return &v, err
	}
	return nil, nil
}

func (na nodeAttributes) fontSize() (Value, error) {
	attrValue, has := na["font-size"]
	if !has {
		attrValue = "1em"
	}
	return parseValue(attrValue)
}

func (na nodeAttributes) aspectRatio() preserveAspectRatio {
	preserveAspectRatio := "xMidYMid"
	if s, has := na["preserveAspectRatio"]; has {
		preserveAspectRatio = s
	}
	return parsePreserveAspectRatio(preserveAspectRatio)
}

// default to black

func (na nodeAttributes) display() bool {
	attrValue := na["display"]
	return attrValue != "none"
}

func (na nodeAttributes) visible() bool {
	attrValue := na["visibility"]
	visible := attrValue != "hidden"
	return na.display() && visible
}

func (na nodeAttributes) spacePreserve() bool {
	return na["space"] == "preserve"
}

// walk the tree to extract content needed to build the SVG tree
// The stylesheets of <style> elements whose media attribute does not match
// device are left out; sizeQueries are the media attributes testing the
// size of device (see [stylesheet.Report.SizeQueries]).
func fetchStyleAndTextRefs(root *utils.HTMLNode, device Device) (stylesheets [][]byte, trefs map[string][]byte, sizeQueries [][]stylesheet.MediaQuery) {
	trefs = make(map[string][]byte)
	iter := root.Iter()
	for iter.HasNext() {
		node := iter.Next()
		if css, media := handleStyleElement(node); len(css) != 0 {
			if media != "" {
				query := stylesheet.ParseMediaQueryString(media)
				if stylesheet.DependsOnSize(query) {
					sizeQueries = append(sizeQueries, query)
				}
				if !stylesheet.EvaluateMediaQuery(query, device) {
					continue
				}
			}
			stylesheets = append(stylesheets, css)
			continue
		}

		// register text refs
		if id := node.Get("id"); id != "" {
			trefs[id] = node.GetChildrenText()
		}
	}
	return stylesheets, trefs, sizeQueries
}

var (
	replacerPreserve   = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ")
	replacerNoPreserve = strings.NewReplacer("\n", "", "\r", "", "\t", " ")
)

// replace newlines by spaces, and merge spaces if not preserved.
// Per SVG 1.1 spec §10.15 ("White space handling"), with the default
// xml:space="default" treatment, all newline/tab characters are
// stripped, then runs of multiple spaces are collapsed into a single
// space. With xml:space="preserve" we only translate \n/\t/\r to a
// single space and leave other whitespace runs intact.
func processWhitespace(text []byte, preserveSpace bool) []byte {
	if preserveSpace {
		return []byte(replacerPreserve.Replace(string(text)))
	}
	out := []byte(replacerNoPreserve.Replace(string(text)))
	// Collapse consecutive spaces.
	collapsed := make([]byte, 0, len(out))
	prevSpace := false
	for _, c := range out {
		if c == ' ' {
			if !prevSpace {
				collapsed = append(collapsed, c)
			}
			prevSpace = true
		} else {
			collapsed = append(collapsed, c)
			prevSpace = false
		}
	}
	return collapsed
}

// handle text node by fixing whitespaces and flattening tails,
// updating node 'children' and 'text'
func handleText(node *cascadedNode, trailingSpace, textRoot bool, trefs map[string][]byte) bool {
	preserve := node.attrs.spacePreserve()
	node.text = processWhitespace(node.text, preserve)
	if trailingSpace && !preserve {
		node.text = bytes.TrimLeft(node.text, " ")
	}

	if len(node.text) != 0 {
		trailingSpace = bytes.HasSuffix(node.text, []byte{' '})
	}

	var newChildren []*cascadedNode
	for _, child := range node.children {
		if child.tag == "tref" {
			// Retrieve the referenced node and get its flattened text
			// and remove the node children.
			id := parseURLFragment(child.attrs["href"])
			node.text = append(node.text, trefs[id]...)
			continue
		}

		trailingSpace = handleText(child, trailingSpace, false, trefs)

		newChildren = append(newChildren, child)
	}

	if textRoot && len(newChildren) == 0 && !preserve {
		node.text = bytes.TrimRight(node.text, " ")
	}

	node.children = newChildren

	return trailingSpace
}

// these attributes are not cascaded
var notInheritedAttributes = utils.NewSet(
	"clip",
	"clip-path",
	"filter",
	"height",
	"id",
	"mask",
	"opacity",
	"overflow",
	"rotate",
	"stop-color",
	"stop-opacity",
	"style",
	"transform",
	"transform-origin",
	"viewBox",
	"width",
	"x",
	"y",
	"dx",
	"dy",
	"href",
	// geometry and structure: attributes of their element only
	"class", "display", "d", "points", "pathLength",
	"cx", "cy", "r", "rx", "ry", "x1", "y1", "x2", "y2", "fx", "fy", "fr",
	"offset", "preserveAspectRatio",
	"gradientUnits", "gradientTransform", "spreadMethod",
	"patternUnits", "patternContentUnits", "patternTransform",
	"markerWidth", "markerHeight", "markerUnits", "refX", "refY", "orient",
	"clipPathUnits", "maskUnits", "maskContentUnits", "filterUnits", "primitiveUnits",
	"textLength", "lengthAdjust", "startOffset", "method", "spacing", "side",
)

var currentColorIdent = regexp.MustCompile(`(?i)[#\w-]*currentcolor[\w-]*`)

// withCurrentColor returns value with the currentColor keywords replaced
// by current (black when empty).
func withCurrentColor(value, current string) string {
	if !strings.Contains(strings.ToLower(value), "currentcolor") {
		return value
	}
	if current == "" {
		current = "black"
	}
	return currentColorIdent.ReplaceAllStringFunc(value, func(m string) string {
		if strings.EqualFold(m, "currentcolor") {
			return current
		}
		return m // an other identifier, or a #fragment
	})
}

var colorAttributes = utils.NewSet(
	"fill",
	"flood-color",
	"lighting-color",
	"stop-color",
	"stroke",
)

// The initial values below come from the property registration in
// properties.go rather than being repeated here. SVG's defaults are part of
// its property definitions, and keeping a second copy in these accessors is
// exactly the duplication T9 is removing: the two drifted silently before,
// since nothing checked they agreed.

// initialString returns a registered property's initial value as written.
func initialString(p pr.KnownProp) string {
	if s, ok := pr.InitialValue(p).(pr.String); ok {
		return string(s)
	}
	return ""
}

// initialNumber returns a registered property's initial numeric value.
func initialNumber(p pr.KnownProp) pr.Float {
	if f, ok := pr.InitialValue(p).(pr.Float); ok {
		return f
	}
	return 0
}

// initialLength returns a registered property's initial length.
func initialLength(p pr.KnownProp) Value {
	if d, ok := pr.InitialValue(p).(pr.TaggedDim); ok {
		return Value{U: Px, V: Fl(d.Value)}
	}
	return Value{}
}

// syncProps re-validates the attributes after the cascade has run, so the
// typed store matches the strings. Inheritance, currentColor substitution and
// style rules all rewrite attrs after newNodeAttributes has seen them.
func syncProps(attrs nodeAttributes, props pr.Properties) {
	for name, raw := range attrs {
		prop, isRegistered := pr.RegisteredProperty(name)
		if !isRegistered {
			continue
		}
		if v, ok := validatePresentationAttr(name, raw); ok && v != nil {
			props[prop] = v
		} else {
			delete(props, prop)
		}
	}
}
