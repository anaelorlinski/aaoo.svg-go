// Package limits gathers the bounds the SVG library puts on its input,
// besides those of the CSS engine (aaoo.cssengine-go/limits).
//
// Documents may be untrusted. Every construct whose cost can grow faster
// than the size of its source is bounded here, in one place, so that the
// bounds can be reviewed together. Going over a bound is never a crash:
// the construct is cut, with a warning. The values are generous: real
// documents stay far below them.
//
// Bounds on amplification (work per element) are preferred to fixed
// totals, so that large documents are never cut: see [DrawsPerElement].
package limits

// <use>: each instantiates a copy of its target, which may itself hold
// <use> elements, and may fetch another document.
const (
	// UseElements bounds the elements instantiated through <use> in a
	// document and the documents it references.
	UseElements = 100_000
	// UseDocuments bounds the documents fetched for <use>; UseDepth the
	// nesting of documents referencing documents.
	UseDocuments = 64
	UseDepth     = 16
)

// Drawing: clip paths, masks, markers and patterns are drawn for each
// element referencing them, and may reference others, so a chain of them
// is exponential in the document size.
const (
	// ReferenceDepth bounds the nesting of such references while drawing:
	// a clip path whose content uses a mask whose content uses a pattern…
	// (real documents nest 1 to 3 deep).
	ReferenceDepth = 16
	// DrawsPerElement bounds the elements drawn by one Draw, relative to
	// the elements of the document, with a floor of MinDraws: a document
	// of n elements may draw max(MinDraws, n × DrawsPerElement) of them.
	// The bound follows the document, so only amplification is cut.
	DrawsPerElement = 100
	MinDraws        = 1_000_000
	// PatternTileDraws is what a pattern tile costs, in draws: a group and
	// a deferred tile on the backend, about 20 plain draws (6 µs).
	PatternTileDraws = 20
)

const (
	// SVGVariants bounds the sizes an SVG image keeps styles for, when its
	// @media rules test its size (one parse each).
	SVGVariants = 8
)
