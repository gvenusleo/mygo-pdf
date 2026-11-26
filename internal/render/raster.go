package render

import (
	"image"
	"math"
)

const (
	BitmapBudget     = 24 << 20
	TileBudget       = 40 << 20
	MetadataBudget   = 8 << 20
	PageMetadata     = 1 << 20
	maxPagePixels    = 4_000_000
	maxPreviewPixels = 1_000_000
	maxSide          = 4096
)

// PageSize holds the page dimensions in PDF points before viewer rotation.
type PageSize struct{ Width, Height float32 }

// Key identifies a raster for one document generation.
type Key struct {
	Document uint64
	Page     int
	Rotation int
	Size     image.Point
	Tile     image.Rectangle
}

func (key Key) Region() image.Rectangle {
	if key.Tile.Empty() {
		return image.Rectangle{Max: key.Size}
	}
	return key.Tile
}

// VisibleBitmapBudget reserves part of the whole-page cache for prefetching.
const VisibleBitmapBudget = BitmapBudget * 3 / 4

// PageResolution sets the scale and pixel limit for a whole-page bitmap.
type PageResolution struct {
	scale      float64
	pixelLimit int
}

// FullPageResolution applies a display scale and the whole-page pixel limit.
func FullPageResolution(scale float64) PageResolution {
	return PageResolution{scale: scale, pixelLimit: maxPagePixels}
}

// PreviewResolution caps a prefetched page at its fit scale and preview pixel limit.
func PreviewResolution(zoom float32) PageResolution {
	return PageResolution{scale: min(1, 1/float64(zoom)), pixelLimit: maxPreviewPixels}
}

// FallbackResolution also limits a preview to its share of the visible-page budget.
func FallbackResolution(zoom float32, budgetBytes int) PageResolution {
	resolution := PreviewResolution(zoom)
	// Allow half the page budget for an existing RGBA bitmap during replacement.
	resolution.pixelLimit = min(resolution.pixelLimit, budgetBytes/8)
	return resolution
}

// Size returns the raster dimensions for a fitted page.
func (resolution PageResolution) Size(width, height float32) image.Point {
	return rasterSize(float64(width)*resolution.scale, float64(height)*resolution.scale, resolution.pixelLimit)
}

func rasterSize(width, height float64, pixelLimit int) image.Point {
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
