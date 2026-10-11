# aaoo.svg-go

An SVG library: it parses SVG documents and draws them on a
`gfxcore.Canvas`.

It does not render by itself. Drawing goes to whatever implements
`gfxcore.Canvas` (see `aaoo.gfxcore-go`); this module depends on no
renderer, no font engine and no image decoder.

## Dependencies

- `aaoo.cssengine-go`: stylesheets, selectors, cascade, property values.
- `aaoo.gfxcore-go`: the canvas it draws on.
- `golang.org/x/net/html`: parsing.

## What the host supplies

| need | how |
|---|---|
| a canvas | `SVGImage.Draw(dst gfxcore.Canvas, ...)` |
| external resources (stylesheets, documents referenced by `<use>`) | a `Fetcher`, which is also the policy deciding what may be read |
| images (`<image>`) | an `ImageLoader` returning an `Image` |
| text (`<text>`) | a `TextRenderer`; without one, text is skipped with a warning. `svgtext` is one |

## Content

- `svg`: the parser and the drawing.
- `svgcss`: the SVG properties, registered into the CSS engine.
- `logger`: the `*slog.Logger` warnings go to.
- `svgtext`: `<text>` and `<tspan>`. It is a **module of its own**
  (`github.com/anaelorlinski/aaoo.svg-go/svgtext`), so that this one needs
  no font code, not even in its `go.mod`. It uses the style of
  `aaoo.textengine-go/layout` and takes the text engine from the host
  through a small interface: the text of an SVG document is then set with
  the same fonts and metrics as the rest of what the host draws.

## Drawing on an image: `svgrender`

`svgrender` is a **module of its own** too
(`github.com/anaelorlinski/aaoo.svg-go/svgrender`). It joins this library
and the draw engine, `aaoo.drawengine-go`, whose `gfx` package is the
canvas documents are drawn on.

| package | |
|---|---|
| `svgrender` | `Parse`, `Image.Draw` in a rectangle of an existing image, `Render`. Images and nested documents; `DataFetcher` and `FileFetcher` |
| `svgrender/textlayout` | the text engine of `svgtext` on a layout engine of `aaoo.textengine-go`, for a program with its own fonts |
| `svgrender/withtext` | imported for its side effect, makes `svgrender` draw `<text>`, with the fonts of the system and those of `LoadFont` |
| `svgrender/oksvg`, `svgrender/rasterx` | the functions of `srwiley/oksvg` and `srwiley/rasterx` a program drawing icons calls |

A program using oksvg changes two imports and nothing else:

```go
import (
	"github.com/anaelorlinski/aaoo.svg-go/svgrender/oksvg"
	"github.com/anaelorlinski/aaoo.svg-go/svgrender/rasterx"
)

icon, err := oksvg.ReadIconStream(r)
icon.SetTarget(0, 0, w, h)
icon.Draw(rasterx.NewDasher(w, h, rasterx.NewScannerGV(w, h, img, img.Bounds())), 1)
```

`rasterx` here is not a rasterizer: its two functions only say which image
to draw on.

Text is opt-in. Without a text renderer, `<text>` elements are skipped with
a warning and the program links no shaper and no line breaking. One import
adds it:

```go
import _ "github.com/anaelorlinski/aaoo.svg-go/svgrender/withtext"
```

With a text renderer, documents are drawn one at a time: fonts are not safe
for concurrent use.

## Status

Extracted from `aaoo.webengine-go`. Conformance against the resvg suite is
measured by `svgrender/svgtests`: 1,722 cases fetched from resvg, each
rendered with `svgrender` and compared with the reference image. The test
fails on a regression against `baseline.csv`, not on the pass rate.

## Origin

The code comes from the `svg` package of
[benoitkugler/webrender](https://github.com/benoitkugler/webrender),
a Go port of WeasyPrint. See `LICENSE`.
