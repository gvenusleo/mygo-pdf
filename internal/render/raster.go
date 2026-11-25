package render

import (
	"image"
	"math"
)

const (
	BitmapBudget = 24 << 20
	TileBudget   = 40 << 20
	PageMetadata = 1 << 20
	MaxPixels    = 4_000_000
	maxSide      = 4096
)

// Key identifies the raster and metadata for one document generation.
type Key struct {
	Document uint64
	Page     int
	Size     image.Point
	Tile     image.Rectangle
	Metadata int
}

func (key Key) Region() image.Rectangle {
	if key.Tile.Empty() {
		return image.Rectangle{Max: key.Size}
	}
	return key.Tile
}

func RasterSize(width, height float64, pixelLimit int) image.Point {
	factor := min(1, maxSide/max(width, height), math.Sqrt(float64(pixelLimit)/(width*height)))
	w := max(1, int(width*factor))
	h := max(1, int(height*factor))
	if w*h > pixelLimit {
		if w > h {
			w = pixelLimit / h
		} else {
			h = pixelLimit / w
		}
	}
	return image.Pt(w, h)
}
