package svg

import (
	"io"
	"log"

	"github.com/anaelorlinski/aaoo.cssengine-go/parser"
	"github.com/anaelorlinski/aaoo.gfxcore-go"
	"github.com/anaelorlinski/aaoo.gfxcore-go/matrix"
)

var outputLog = log.New(io.Discard, "svg_test: ", 0)

type fl = gfxcore.Fl

var _ gfxcore.Canvas = outputPage{}

type outputPage struct{}

func (outputPage) GetBoundingBox() (left, top, right, bottom fl) {
	outputLog.Println("GetBoundingBox")
	return 0, 0, 10, 10
}

func (outputPage) SetBoundingBox(left, top, right, bottom fl) {
	outputLog.Println("SetBoundingBox")
}

func (outputPage) OnNewStack(f func()) {
	outputLog.Println("OnNewStack")
	f()
}

func (outputPage) Rectangle(x fl, y fl, width fl, height fl) {
	outputLog.Println("Rectangle")
}

func (outputPage) Clip(evenOdd bool) {
	outputLog.Println("Clip")
}

func (outputPage) SetAlpha(alpha fl, stroke bool) {
	outputLog.Println("SetAlpha")
}

func (outputPage) SetColorRgba(color parser.RGBA, stroke bool) {
	outputLog.Println("SetColorRgba")
}

func (outputPage) SetLineWidth(width fl) {
	outputLog.Println("SetLineWidth")
}

func (outputPage) SetDash(dashes []fl, offset fl) {
	outputLog.Println("SetDash")
}

func (outputPage) Paint(gfxcore.PaintOp) {
	outputLog.Println("Paint")
}

func (outputPage) Transform(mt matrix.Transform) {
	outputLog.Println("Transform")
}

func (outputPage) GetTransform() matrix.Transform {
	outputLog.Println("GetTransform")
	return matrix.Identity()
}

func (outputPage) MoveTo(x fl, y fl) {
	outputLog.Println("MoveTo")
}

func (outputPage) LineTo(x fl, y fl) {
	outputLog.Println("LineTo")
}

func (outputPage) CubicTo(x1, y1, x2, y2, x3, y3 fl) {
	outputLog.Println("CubicTo")
}

func (outputPage) ClosePath() {
	outputLog.Println("ClosePath")
}

func (outputPage) SetTextPaint(gfxcore.PaintOp) {
	outputLog.Println("SetTextPaint")
}

func (outputPage) SetBlendingMode(mode string) {
	outputLog.Println("SetTextPaint")
}

func (outputPage) DrawText(text []gfxcore.TextDrawing) {
	outputLog.Println("DrawText", text)
}

func (outputPage) NewGroup(x, y, width, height Fl) gfxcore.Canvas {
	outputLog.Println("NewGroup")
	return outputPage{}
}

func (outputPage) DrawRasterImage(img gfxcore.RasterImage, width, height fl) {
	outputLog.Println("DrawRasterImage")
}

func (outputPage) SetAlphaMask(mask gfxcore.Canvas) {
	outputLog.Println("SetAlphaMask")
}

func (outputPage) DrawGradient(gradient gfxcore.GradientLayout, width, height fl) {
	outputLog.Println("DrawGradient")
}

func (outputPage) DrawWithOpacity(opacity fl, group gfxcore.Canvas) {
	outputLog.Println("DrawGroup")
}

func (outputPage) SetStrokeOptions(gfxcore.StrokeOptions) {
	outputLog.Println("SetStrokeOptions")
}

func (outputPage) SetColorPattern(gfxcore.Canvas, fl, fl, matrix.Transform, bool) {
	outputLog.Println("SetColorPattern")
}

func (outputPage) State() gfxcore.GraphicState {
	return outputPage{}
}
