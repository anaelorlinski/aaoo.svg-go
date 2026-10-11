// Package withtext gives the package svgrender, and so the package oksvg, a
// text renderer: importing it makes the <text> elements of the documents
// drawn. It is imported for this side effect:
//
//	import _ "github.com/anaelorlinski/aaoo.svg-go/svgrender/withtext"
//
// Text is set in the fonts given with [LoadFont], then in those of the
// system, which are looked for when a document first needs one.
//
// It brings the text layout engine of aaoo.textengine-go into the program,
// with its shaper: a program drawing no text should not import it.
package withtext

import (
	"github.com/anaelorlinski/aaoo.svg-go/svg"
	"github.com/anaelorlinski/aaoo.svg-go/svgrender"
	"github.com/anaelorlinski/aaoo.svg-go/svgrender/textlayout"
	"github.com/anaelorlinski/aaoo.svg-go/svgtext"
	"github.com/anaelorlinski/aaoo.textengine-go/face"
	"github.com/anaelorlinski/aaoo.textengine-go/layout"
	"github.com/go-text/typesetting/fontscan"
)

// The layout engine is not safe for concurrent use: svgrender draws the
// documents with text one at a time, and the fonts are changed in
// svgrender.Exclusive.
var (
	engine *layout.Engine
	shared *textlayout.Engine
)

// quietLogger drops the progress messages of fontscan.
type quietLogger struct{}

func (quietLogger) Printf(string, ...any) {}

func init() {
	engine = layout.NewEngine(fontscan.NewFontMap(quietLogger{}))
	engine.Missing = layout.MissingToSystem
	engine.SystemFallbacks = []string{"Arial", "Helvetica Neue", "DejaVu Sans", "Liberation Sans"}
	shared = textlayout.NewEngine(engine)

	svgrender.RegisterText(func() svg.TextRenderer { return svgtext.New(shared) })
}

// LoadFont makes a font available to the documents under a family name,
// with the style the caller says it has. The first family loaded is the one
// a text is set in when its own is neither loaded nor installed.
//
// It may be called while documents are drawn.
func LoadFont(family string, content []byte, style face.Style) (err error) {
	svgrender.Exclusive(func() {
		if err = engine.LoadFont(family, content, style); err == nil && engine.Fallback == "" {
			engine.Fallback = family
		}
	})
	return err
}
