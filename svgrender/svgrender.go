// Package svgrender draws SVG documents on images: it joins the SVG library,
// aaoo.svg-go, and the draw engine, aaoo.drawengine-go.
//
// Text is not drawn unless a text renderer is given, with [Options.Text] or
// for the whole program with [RegisterText]. Importing the package withtext
// for its side effect registers one:
//
//	import _ "github.com/anaelorlinski/aaoo.svg-go/svgrender/withtext"
//
// A program that draws no text then carries no text layout code.
package svgrender

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"math"
	"sync"
	"sync/atomic"

	pr "github.com/anaelorlinski/aaoo.cssengine-go/properties"
	"github.com/anaelorlinski/aaoo.drawengine-go/canvas"
	"github.com/anaelorlinski/aaoo.drawengine-go/gfx"
	"github.com/anaelorlinski/aaoo.drawengine-go/renderers/rasterizer"
	"github.com/anaelorlinski/aaoo.gfxcore-go"
	"github.com/anaelorlinski/aaoo.gfxcore-go/matrix"
	"github.com/anaelorlinski/aaoo.svg-go/svg"
	"golang.org/x/net/html"
)

// Options tunes [Parse].
type Options struct {
	// BaseURL resolves the relative references of the document.
	BaseURL string

	// CurrentColor is the CSS color `currentColor` stands for, black when
	// it is empty.
	CurrentColor string

	// Fetcher fetches what the document refers to: images, stylesheets,
	// other documents. It is the policy deciding what the document may
	// read. Nil is [DataFetcher]: nothing but data: URLs.
	Fetcher svg.Fetcher

	// Text returns the renderer of the <text> elements of a document; it
	// is called once per drawing. Nil is the renderer of [RegisterText].
	Text func() svg.TextRenderer
}

var registeredText atomic.Pointer[func() svg.TextRenderer]

// RegisterText sets the text renderer of the documents parsed without
// [Options.Text], for the whole program. It is meant to be called from the
// init function of a package giving one, as withtext does. newRenderer is
// called once per drawing, possibly from several goroutines.
func RegisterText(newRenderer func() svg.TextRenderer) {
	if newRenderer == nil {
		registeredText.Store(nil)
		return
	}
	registeredText.Store(&newRenderer)
}

// textLock is held while a document is drawn with a text renderer: fonts
// are not safe for concurrent use, neither when text is laid out nor when
// its glyphs are drawn.
var textLock sync.Mutex

// Exclusive runs f while no document is being drawn with a text renderer.
// A package giving a text renderer changes its fonts in it.
func Exclusive(f func()) {
	textLock.Lock()
	defer textLock.Unlock()
	f()
}

// Image is a parsed SVG document. It is not modified by drawing, and may be
// drawn several times.
type Image struct {
	doc  *svg.SVGImage
	text func() svg.TextRenderer
}

// Parse parses the SVG document read from r.
func Parse(r io.Reader, opts Options) (*Image, error) {
	fetcher := opts.Fetcher
	if fetcher == nil {
		fetcher = DataFetcher
	}
	budget := maxNestedImages
	l := &loader{fetcher: fetcher, text: opts.Text, budget: &budget}
	return l.parse(r, opts.BaseURL, opts.CurrentColor)
}

// newText returns a text renderer for one drawing, or nil for none.
func newText(given func() svg.TextRenderer) svg.TextRenderer {
	if given != nil {
		return given()
	}
	if registered := registeredText.Load(); registered != nil {
		return (*registered)()
	}
	return nil
}

// ViewBox returns the viewBox attribute of the document, and false when it
// has none.
func (im *Image) ViewBox() (x, y, width, height float64, ok bool) {
	vb := im.doc.ViewBox()
	if vb == nil {
		return 0, 0, 0, 0, false
	}
	return float64(vb.X), float64(vb.Y), float64(vb.Width), float64(vb.Height), true
}

// defaultFontSize resolves the font relative units of the size of a
// document, which has no text around it to take a font size from.
const defaultFontSize = 16

// IntrinsicSize returns the size the document says it has, in pixels, and
// its width / height ratio. Each is 0 when the document does not say: a
// width or a height may be missing, or given as a percentage.
func (im *Image) IntrinsicSize() (width, height, ratio float64) {
	w, h, r := intrinsicSize(im.doc, defaultFontSize)
	return float64(pr.MaybeFloatToFloat(w)), float64(pr.MaybeFloatToFloat(h)), float64(pr.MaybeFloatToFloat(r))
}

// intrinsicSize returns the size of a document for a font size, nil for what
// it does not say.
func intrinsicSize(doc *svg.SVGImage, fontSize svg.Fl) (width, height, ratio pr.MaybeFloat) {
	w, h := doc.DisplayedSize()

	// Intrinsic sizing happens before there is a viewport, so only the
	// font-relative units can resolve here. Percentages are skipped; a
	// viewport-relative intrinsic size is not meaningful either, and
	// resolves against a zero viewport.
	dims := svg.Dims{FontSize: fontSize, RootFontSize: fontSize}
	if w.U != svg.Perc {
		width = pr.Float(w.Resolve(&dims, 0))
	}
	if h.U != svg.Perc {
		height = pr.Float(h.Resolve(&dims, 0))
	}

	if width == nil || height == nil {
		if vb := doc.ViewBox(); vb != nil && vb.Width != 0 && vb.Height != 0 {
			ratio = pr.Float(vb.Width / vb.Height)
			if pr.Is(width) {
				height = width.V() / ratio.V()
			} else if pr.Is(height) {
				width = height.V() * ratio.V()
			}
		}
	} else if pr.Is(width) && pr.Is(height) {
		ratio = width.V() / height.V()
	}
	return width, height, ratio
}

// Draw draws the document on dst, in the rectangle of size (width, height)
// whose top left corner is at (x, y), in pixels from the top left corner of
// dst. The document is fitted in the rectangle as its preserveAspectRatio
// attribute says. opacity is 1 for an opaque drawing.
//
// The document is drawn over what dst holds.
//
// Documents may be drawn from several goroutines. With a text renderer they
// are drawn one at a time.
func (im *Image) Draw(dst draw.Image, x, y, width, height, opacity float64) error {
	bounds := dst.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return errors.New("svgrender: empty destination image")
	}
	if !(width > 0 && height > 0) || math.IsInf(width, 0) || math.IsInf(height, 0) {
		return fmt.Errorf("svgrender: invalid size %g x %g", width, height)
	}

	c := gfx.NewCanvas(float64(w), float64(h))
	// The document is drawn from the top left, with y going down; the draw
	// engine has its origin at the bottom left.
	c.State().Transform(matrix.New(1, 0, 0, -1, 0, gfxcore.Fl(h)))
	if x != 0 || y != 0 {
		c.State().Transform(matrix.Translation(gfxcore.Fl(x), gfxcore.Fl(y)))
	}
	text := newText(im.text)
	if text != nil {
		textLock.Lock()
		defer textLock.Unlock()
	}
	if opacity <= 0 {
		// nothing is seen
	} else if opacity < 1 {
		group := c.NewGroup(0, 0, gfxcore.Fl(width), gfxcore.Fl(height))
		im.doc.Draw(group, gfxcore.Fl(width), gfxcore.Fl(height), text)
		c.DrawGroup(group, gfxcore.Composite{Opacity: gfxcore.Fl(opacity)})
	} else {
		im.doc.Draw(c, gfxcore.Fl(width), gfxcore.Fl(height), text)
	}

	layer := image.NewRGBA(image.Rect(0, 0, w, h))
	ras := rasterizer.FromImage(layer, canvas.DPMM(1.0), canvas.DefaultColorSpace)
	c.TdCanvas().RenderTo(ras)
	ras.Close()
	draw.Draw(dst, bounds, layer, image.Point{}, draw.Over)
	return nil
}

// Render parses the document read from r and draws it on a new image of the
// given width, in pixels. The height follows from the ratio of the document,
// and is the width when it has none.
func Render(r io.Reader, width int, opts Options) (*image.RGBA, error) {
	if width < 1 {
		return nil, fmt.Errorf("svgrender: invalid width %d", width)
	}
	im, err := Parse(r, opts)
	if err != nil {
		return nil, err
	}
	height := im.HeightFor(width)
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	if err := im.Draw(out, 0, 0, float64(width), float64(height), 1); err != nil {
		return nil, err
	}
	return out, nil
}

// HeightFor returns the height, in pixels, of the document drawn at a width:
// the one its ratio gives, or the width when it has none.
func (im *Image) HeightFor(width int) int {
	w, h, ratio := im.IntrinsicSize()
	aspect := 1.0
	if ratio != 0 {
		aspect = ratio
	} else if w != 0 && h != 0 {
		aspect = w / h
	}
	if aspect <= 0 || math.IsNaN(aspect) || math.IsInf(aspect, 0) {
		aspect = 1
	}
	return max(1, int(math.Round(float64(width)/aspect)))
}

// parse parses a document with the images it refers to.
func (l *loader) parse(r io.Reader, baseURL, currentColor string) (*Image, error) {
	root, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parsing SVG: %w", err)
	}
	inner := *l
	inner.path = append(inner.path[:len(inner.path):len(inner.path)], baseURL)
	doc, err := svg.ParseNode(root, baseURL, inner.load, l.fetcher, currentColor, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing SVG: %w", err)
	}
	return &Image{doc: doc, text: l.text}, nil
}
