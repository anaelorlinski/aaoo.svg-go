// Package rasterx holds the two functions of github.com/srwiley/rasterx a
// program drawing icons with oksvg calls, so that it can move to the oksvg
// package of this module by changing its imports only.
//
// It is not a rasterizer: the values it makes only say which image to draw
// on. See the package oksvg.
package rasterx

import (
	"image"
	"image/draw"
)

// ScannerGV says which image to draw on, and which part of it.
type ScannerGV struct {
	// Dest is the image drawn on.
	Dest draw.Image
	// Targ is the part of Dest that may be drawn on.
	Targ image.Rectangle
}

// NewScannerGV returns a target drawing on dest, within targ. The width and
// the height are those of the image; they are not used.
func NewScannerGV(width, height int, dest draw.Image, targ image.Rectangle) *ScannerGV {
	return &ScannerGV{Dest: dest, Targ: targ}
}

// Dasher is what an icon is drawn with.
type Dasher struct {
	Scanner *ScannerGV
}

// NewDasher returns a Dasher drawing on the target of scanner. The width and
// the height are those of the image; they are not used.
func NewDasher(width, height int, scanner *ScannerGV) *Dasher {
	return &Dasher{Scanner: scanner}
}
