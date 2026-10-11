// Package canvasrenderer binds the conformance harness to a concrete SVG
// renderer: svgrender, on the draw engine.
//
// It is deliberately the only package here that knows about a rasterising
// backend: svgtests itself stays backend-free.
package canvasrenderer

import (
	"bytes"
	"context"
	"image"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	csslogger "github.com/anaelorlinski/aaoo.cssengine-go/logger"
	svglogger "github.com/anaelorlinski/aaoo.svg-go/logger"
	"github.com/anaelorlinski/aaoo.svg-go/svg"
	"github.com/anaelorlinski/aaoo.svg-go/svgrender"
	"github.com/anaelorlinski/aaoo.svg-go/svgrender/textlayout"
	"github.com/anaelorlinski/aaoo.svg-go/svgtext"
	"github.com/anaelorlinski/aaoo.textengine-go/face"
	"github.com/anaelorlinski/aaoo.textengine-go/layout"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/fontscan"
)

// New returns a renderer satisfying svgtests.Renderer.
//
// fontsDir must point at the suite's own fonts. The text tests reference
// specific families (Noto Sans, Amiri, MPLUS1p...), so without them every
// text case is judged against a fallback font and the numbers say nothing
// about text support.
func New(fontsDir string) Renderer { return Renderer{fontsDir: fontsDir} }

// Renderer renders SVG with svgrender, capturing the warnings emitted during
// parse and render so that silently-wrong output can be told apart from
// output the renderer knew it could not produce.
type Renderer struct{ fontsDir string }

func (r Renderer) Render(src []byte, path string, width int) (image.Image, []string, error) {
	// the warnings of the SVG library and of the CSS engine
	var warns warnings
	defer svglogger.Set(svglogger.Swap(slog.New(&warns)))
	defer csslogger.Set(csslogger.Swap(slog.New(&warns)))

	// one text engine per document: nothing is kept from one to the next
	engine := newEngine(r.fontsDir)
	text := textlayout.NewEngine(engine)

	baseURL := ""
	if abs, err := filepath.Abs(path); err == nil && path != "" {
		baseURL = "file://" + filepath.ToSlash(abs)
	}
	img, err := svgrender.Render(bytes.NewReader(src), width, svgrender.Options{
		BaseURL: baseURL,
		// no network: the suite is rendered offline, the same every time
		Fetcher: svgrender.FileFetcher,
		Text:    func() svg.TextRenderer { return svgtext.New(text) },
	})
	if err != nil {
		return nil, warns.lines, err
	}
	return img, warns.lines, nil
}

// warnings is a log handler keeping the warnings it is given, one line each.
type warnings struct{ lines []string }

func (w *warnings) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (w *warnings) Handle(_ context.Context, r slog.Record) error {
	if line := strings.TrimSpace(csslogger.Render(r)); line != "" {
		w.lines = append(w.lines, line)
	}
	return nil
}

func (w *warnings) WithAttrs([]slog.Attr) slog.Handler { return w }
func (w *warnings) WithGroup(string) slog.Handler      { return w }

// quietLogger drops the progress messages of fontscan.
type quietLogger struct{}

func (quietLogger) Printf(string, ...any) {}

// newEngine returns a text engine with the fonts of a directory: every .ttf
// and .otf file directly inside it, under the family and the style its own
// name table gives. A family that is not among them is looked for on the
// system, then set in "sans-serif" when it is loaded, otherwise in the first
// loaded family in alphabetical order.
func newEngine(fontsDir string) *layout.Engine {
	engine := layout.NewEngine(fontscan.NewFontMap(quietLogger{}))
	engine.Missing = layout.MissingToSystem
	engine.SystemFallbacks = []string{"Arial", "Helvetica Neue", "DejaVu Sans", "Liberation Sans"}
	if fontsDir == "" {
		return engine
	}
	entries, err := os.ReadDir(fontsDir)
	if err != nil {
		return engine
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".ttf" && ext != ".otf" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(fontsDir, e.Name()))
		if err != nil {
			continue
		}
		ld, err := opentype.NewLoader(bytes.NewReader(data))
		if err != nil {
			continue
		}
		desc, _ := font.Describe(ld, nil)
		if desc.Family == "" {
			continue
		}
		// A file that fails to load is skipped: a partially loaded font set
		// still lets the rest of the suite render.
		if engine.LoadFont(desc.Family, data, face.StyleOf(desc.Aspect)) != nil {
			continue
		}
		name := font.NormalizeFamily(desc.Family)
		switch {
		case name == "sans-serif":
			engine.Fallback = name
		case engine.Fallback != "sans-serif" && (engine.Fallback == "" || name < engine.Fallback):
			engine.Fallback = name
		}
	}
	return engine
}
