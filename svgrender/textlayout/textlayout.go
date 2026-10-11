// Package textlayout lays the text of SVG documents out with the layout
// engine of aaoo.textengine-go, for the canvas of the draw engine.
//
// A program gives its own layout engine, with its fonts, to [NewEngine]. The
// package withtext does it with the fonts of the system, for the whole
// program.
package textlayout

import (
	"github.com/anaelorlinski/aaoo.drawengine-go/gfx/typeset"
	"github.com/anaelorlinski/aaoo.gfxcore-go"
	"github.com/anaelorlinski/aaoo.svg-go/svgtext"
	"github.com/anaelorlinski/aaoo.textengine-go/layout"
)

// Engine lays the text of SVG documents out with a layout engine: it is a
// [svgtext.Engine]. It is not safe for concurrent use, like the layout
// engine.
type Engine struct {
	engine  *layout.Engine
	fonts   *typeset.Fonts
	hyphens map[layout.HyphenDictKey]layout.Hyphener
}

var _ svgtext.Engine = (*Engine)(nil)

// NewEngine returns the text engine of SVG documents drawn with the fonts of
// engine.
func NewEngine(engine *layout.Engine) *Engine {
	return &Engine{
		engine:  engine,
		fonts:   typeset.NewFonts(engine),
		hyphens: map[layout.HyphenDictKey]layout.Hyphener{},
	}
}

func (e *Engine) Shape(text []rune, style *layout.Style) svgtext.Line {
	return svgLine{
		line:  e.engine.SplitFirstLine(e.hyphens, text, style, layout.Inf, true, false, true),
		fonts: e.fonts,
	}
}

type svgLine struct {
	line  layout.FirstLine
	fonts *typeset.Fonts
}

func (l svgLine) Width() float32  { return l.line.Width }
func (l svgLine) Height() float32 { return l.line.Height }

func (l svgLine) Drawing(scaleX, x, y, angle float32) gfxcore.TextDrawing {
	line := l.line.Layout
	text := line.Text()
	if line.Style == nil || isAllSpaces(text) {
		return gfxcore.TextDrawing{}
	}
	line.ApplyJustification()
	return l.fonts.Drawing(line.Runs, text, line.Style, scaleX, x, y, angle)
}

// isAllSpaces reports whether the text is made of spaces only.
func isAllSpaces(text []rune) bool {
	if len(text) == 0 {
		return false
	}
	for _, r := range text {
		if r != ' ' {
			return false
		}
	}
	return true
}
