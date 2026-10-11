package svgrender

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"slices"

	pr "github.com/anaelorlinski/aaoo.cssengine-go/properties"
	"github.com/anaelorlinski/aaoo.gfxcore-go"
	"github.com/anaelorlinski/aaoo.svg-go/svg"
)

// An image may hold images, which may hold images: both the nesting and the
// number of images loaded for one document are bounded.
const (
	maxImageDepth   = 8
	maxNestedImages = 256
)

// loader loads the images of a document, and of the documents it holds.
type loader struct {
	fetcher svg.Fetcher
	text    func() svg.TextRenderer

	// path holds the URLs of the documents being loaded, the outermost
	// first; budget what is left of maxNestedImages.
	path   []string
	budget *int
}

// load loads the image an <image> element refers to.
func (l *loader) load(url string) (svg.Image, error) {
	if slices.Contains(l.path, url) {
		return nil, fmt.Errorf("recursive image reference: %s", shorten(url))
	}
	if len(l.path) > maxImageDepth {
		return nil, fmt.Errorf("image %s nested more than %d deep", shorten(url), maxImageDepth)
	}
	if *l.budget--; *l.budget < 0 {
		return nil, fmt.Errorf("image %s: more than %d images loaded by a document", shorten(url), maxNestedImages)
	}

	resource, err := l.fetcher(url)
	if err != nil {
		return nil, fmt.Errorf("failed to load image at %q (%s)", shorten(url), err)
	}
	content, err := io.ReadAll(resource.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to load image at %q (%s)", shorten(url), err)
	}

	// an SVG document, when the resource says so
	var errSVG error
	if resource.MimeType == "image/svg+xml" {
		var nested *Image
		if nested, errSVG = l.parse(bytes.NewReader(content), url, ""); errSVG == nil {
			return nestedImage{nested}, nil
		}
	}

	config, format, errRaster := image.DecodeConfig(bytes.NewReader(content))
	if errRaster == nil {
		id := fnv.New32()
		id.Write([]byte(url))
		return rasterImage{
			content: content,
			mime:    "image/" + format,
			id:      int(id.Sum32()),
			width:   svg.Fl(config.Width),
			height:  svg.Fl(config.Height),
		}, nil
	}
	if errSVG != nil {
		return nil, fmt.Errorf("failed to load image at %q (%s)", shorten(url), errSVG)
	}

	// last chance: an SVG document under another type
	nested, err := l.parse(bytes.NewReader(content), url, "")
	if err != nil {
		return nil, fmt.Errorf("failed to load image at %q (%s)", shorten(url), errRaster)
	}
	return nestedImage{nested}, nil
}

// rasterImage is a bitmap, kept as it was encoded: the canvas decodes it.
type rasterImage struct {
	content       []byte
	mime          string
	id            int
	width, height svg.Fl
}

func (r rasterImage) IntrinsicSize(svg.Fl) (width, height, ratio pr.MaybeFloat) {
	ratio = pr.Inf
	if r.height != 0 {
		ratio = pr.Float(r.width / r.height)
	}
	return pr.Float(r.width), pr.Float(r.height), ratio
}

func (r rasterImage) Draw(dst gfxcore.Canvas, width, height svg.Fl, rendering string, _ svg.TextRenderer) {
	if !(width > 0 && height > 0 && r.width > 0 && r.height > 0) {
		return
	}
	dst.DrawRasterImage(gfxcore.RasterImage{
		Content:   bytes.NewReader(r.content),
		MimeType:  r.mime,
		Rendering: rendering,
		ID:        r.id,
	}, width, height)
}

// nestedImage is an SVG document drawn as the image of another.
type nestedImage struct{ im *Image }

func (n nestedImage) IntrinsicSize(fontSize svg.Fl) (width, height, ratio pr.MaybeFloat) {
	return intrinsicSize(n.im.doc, fontSize)
}

func (n nestedImage) Draw(dst gfxcore.Canvas, width, height svg.Fl, _ string, _ svg.TextRenderer) {
	// a text renderer keeps the cursor of the document it draws: the image
	// gets its own
	n.im.doc.Draw(dst, width, height, newText(n.im.text))
}
