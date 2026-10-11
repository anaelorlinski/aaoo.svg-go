// Package oksvg draws SVG documents on images with the functions of
// github.com/srwiley/oksvg that a program drawing icons calls, so that it can
// move to this module by changing two imports:
//
//	"github.com/srwiley/oksvg"   ->  "github.com/anaelorlinski/aaoo.svg-go/svgrender/oksvg"
//	"github.com/srwiley/rasterx" ->  "github.com/anaelorlinski/aaoo.svg-go/svgrender/rasterx"
//
// The usual sequence is unchanged:
//
//	icon, err := oksvg.ReadIconStream(r)
//	icon.SetTarget(0, 0, w, h)
//	icon.Draw(rasterx.NewDasher(w, h, rasterx.NewScannerGV(w, h, img, img.Bounds())), 1)
//
// The drawing is the one of the package svgrender, which draws far more of
// SVG. What differs from oksvg:
//
//   - a document is fitted in its target as its preserveAspectRatio
//     attribute says, where oksvg always stretches it;
//   - what the document cannot have is reported through the logger of
//     aaoo.svg-go in every mode but [StrictErrorMode];
//   - the paths of the document are not exposed: only the functions below
//     exist;
//   - text is drawn when a text renderer is registered, see svgrender.
package oksvg

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/draw"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	svglogger "github.com/anaelorlinski/aaoo.svg-go/logger"
	"github.com/anaelorlinski/aaoo.svg-go/svgrender"
	"github.com/anaelorlinski/aaoo.svg-go/svgrender/rasterx"
)

// ErrorMode says what happens when a document has something that cannot be
// drawn.
type ErrorMode uint8

const (
	// IgnoreErrorMode draws what can be drawn. It is the default.
	IgnoreErrorMode ErrorMode = iota

	// WarnErrorMode is the same: what cannot be drawn is reported through
	// the logger of aaoo.svg-go in both modes.
	WarnErrorMode

	// StrictErrorMode makes reading fail when the document has something
	// that cannot be drawn.
	StrictErrorMode
)

// SvgIcon is a parsed SVG document.
type SvgIcon struct {
	// ViewBox is the viewBox of the document, or its size when it has
	// none.
	ViewBox struct{ X, Y, W, H float64 }

	image *svgrender.Image
	// the rectangle the document is drawn in, see SetTarget
	x, y, w, h float64
}

// ReadIconStream reads the icon from the stream. Without an error mode,
// [IgnoreErrorMode] is used.
func ReadIconStream(stream io.Reader, errMode ...ErrorMode) (*SvgIcon, error) {
	return read(stream, svgrender.Options{}, errMode)
}

// ReadReplacingCurrentColor is [ReadIconStream], with the CSS color
// currentColor stands for.
func ReadReplacingCurrentColor(stream io.Reader, currentColor string, errMode ...ErrorMode) (*SvgIcon, error) {
	return read(stream, svgrender.Options{CurrentColor: currentColor}, errMode)
}

// ReadIcon reads the icon from the named file. The document may refer to
// other files, relative to its own.
func ReadIcon(iconFile string, errMode ...ErrorMode) (*SvgIcon, error) {
	content, err := os.ReadFile(iconFile)
	if err != nil {
		return nil, err
	}
	opts := svgrender.Options{Fetcher: svgrender.FileFetcher}
	if abs, err := filepath.Abs(iconFile); err == nil {
		opts.BaseURL = "file://" + filepath.ToSlash(abs)
	}
	return read(bytes.NewReader(content), opts, errMode)
}

func read(stream io.Reader, opts svgrender.Options, errMode []ErrorMode) (*SvgIcon, error) {
	mode := IgnoreErrorMode
	if len(errMode) != 0 {
		mode = errMode[0]
	}

	var (
		im  *svgrender.Image
		err error
	)
	if mode == StrictErrorMode {
		var warning string
		warning, im, err = parseStrictly(stream, opts)
		if err == nil && warning != "" {
			return nil, errors.New(warning)
		}
	} else {
		im, err = svgrender.Parse(stream, opts)
	}
	if err != nil {
		return nil, err
	}

	icon := &SvgIcon{image: im}
	if x, y, w, h, ok := im.ViewBox(); ok {
		icon.ViewBox.X, icon.ViewBox.Y, icon.ViewBox.W, icon.ViewBox.H = x, y, w, h
	} else {
		icon.ViewBox.W, icon.ViewBox.H, _ = im.IntrinsicSize()
	}
	icon.w, icon.h = icon.ViewBox.W, icon.ViewBox.H
	return icon, nil
}

// strict makes the strict parses run one at a time: they borrow the logger
// of the SVG library, which is the one of the whole program.
var strict sync.Mutex

// parseStrictly parses a document and returns the first warning it gave.
// Warnings of documents parsed by other goroutines in the meantime are taken
// for its own.
func parseStrictly(stream io.Reader, opts svgrender.Options) (warning string, im *svgrender.Image, err error) {
	strict.Lock()
	defer strict.Unlock()

	var first firstWarning
	previous := svglogger.Swap(slog.New(&first))
	defer svglogger.Set(previous)

	im, err = svgrender.Parse(stream, opts)
	return first.message(), im, err
}

// firstWarning is a log handler keeping the first warning it is given.
type firstWarning struct {
	mu  sync.Mutex
	msg string
}

func (f *firstWarning) message() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.msg
}

func (f *firstWarning) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (f *firstWarning) Handle(_ context.Context, r slog.Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.msg == "" {
		f.msg = r.Message
	}
	return nil
}

func (f *firstWarning) WithAttrs([]slog.Attr) slog.Handler { return f }
func (f *firstWarning) WithGroup(string) slog.Handler      { return f }

// SetTarget sets the rectangle of the image the icon is drawn in. Without
// it, the icon is drawn at the origin, at the size of its viewBox.
func (s *SvgIcon) SetTarget(x, y, w, h float64) {
	s.x, s.y, s.w, s.h = x, y, w, h
}

// Draw draws the icon on the image of r, over what it holds, in the
// rectangle of [SvgIcon.SetTarget]. opacity is 1 for an opaque drawing.
// An icon with no size, or a target with no image, draws nothing.
func (s *SvgIcon) Draw(r *rasterx.Dasher, opacity float64) {
	if r == nil || r.Scanner == nil || r.Scanner.Dest == nil {
		return
	}
	dest := r.Scanner.Dest
	bounds := dest.Bounds()
	clip := r.Scanner.Targ.Intersect(bounds)
	if clip.Empty() {
		return
	}
	// The coordinates of the icon are those of the image, whose origin is
	// not always its top left corner.
	x, y := s.x-float64(bounds.Min.X), s.y-float64(bounds.Min.Y)
	if clip == bounds {
		_ = s.image.Draw(dest, x, y, s.w, s.h, opacity)
		return
	}
	// only a part of the image may be drawn on
	layer := image.NewRGBA(bounds)
	if s.image.Draw(layer, x, y, s.w, s.h, opacity) == nil {
		draw.Draw(dest, clip, layer, clip.Min, draw.Over)
	}
}
