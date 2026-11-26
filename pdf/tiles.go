package pdf

import (
	"image"
	"math"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/gvenusleo/mygo-pdf/internal/render"
)

func (v *Viewer) requestTiles(first, last int, all bool) []rasterKey {
	v.tileKeys = v.visibleTiles(first, last, all)
	for key, entry := range v.tiles {
		if slices.Contains(v.tileKeys, key) {
			continue
		}
		distance := v.tileDistance(key)
		if entry.frame == nil || distance > tileSide {
			v.removeTile(key)
		}
	}
	var requests []rasterKey
	for _, key := range v.tileKeys {
		entry := v.tiles[key]
		if entry.failed != key && entry.frame == nil {
			requests = append(requests, key)
		}
	}
	return requests
}

func (v *Viewer) tileDistance(key rasterKey) int {
	if key.Document != v.document || key.Rotation != v.rotation || key.Page >= len(v.pages) {
		return math.MaxInt
	}
	page := v.layout.page(key.Page)
	x, y := float64(key.Size.X)/float64(page.W), float64(key.Size.Y)/float64(page.H)
	left, top := float64(v.scroll.X-page.X)*x, float64(v.scroll.Y-page.Y)*y
	right, bottom := left+float64(v.width)*x, top+float64(v.height)*y
	distance := max(0, left-float64(key.Tile.Max.X), float64(key.Tile.Min.X)-right,
		top-float64(key.Tile.Max.Y), float64(key.Tile.Min.Y)-bottom)
	return int(math.Ceil(distance))
}

func (v *Viewer) removeTile(key rasterKey) {
	if frame := v.tiles[key].frame; frame != nil {
		v.tileBytes -= frame.frameBytes()
	}
	delete(v.tiles, key)
}

func (v *Viewer) trimTiles() {
	for v.tileBytes > render.TileBudget {
		var farthest rasterKey
		maxDistance := 0
		for key, entry := range v.tiles {
			if entry.frame == nil || slices.Contains(v.tileKeys, key) {
				continue
			}
			if distance := v.tileDistance(key); distance >= maxDistance {
				maxDistance, farthest = distance, key
			}
		}
		if farthest.Tile.Empty() {
			// visibleTiles bounds the total size of the visible keys.
			return
		}
		v.removeTile(farthest)
	}
}

func (v *Viewer) needsTiles(page int, full rasterKey) bool {
	r := v.layout.page(page)
	return float64(full.Size.X)+1 < float64(r.W)*v.scale || float64(full.Size.Y)+1 < float64(r.H)*v.scale
}

const tileSide = 512

func (v *Viewer) tileRasterSize(page ui.Rect) image.Point {
	area := float64(v.width) * float64(v.height)
	// Allow for tiles that cross the viewport edge and keep PDFium dimensions bounded.
	scale := min(v.scale, math.Sqrt(float64(render.TileBudget)*0.6/(4*max(1, area))),
		float64(1<<24)/float64(max(page.W, page.H)))
	return image.Pt(max(1, int(math.Ceil(float64(page.W)*scale))), max(1, int(math.Ceil(float64(page.H)*scale))))
}

func (v *Viewer) visibleTiles(first, last int, all bool) []rasterKey {
	var keys []rasterKey
	const maxTiles = render.TileBudget / (tileSide * tileSide * 4)
	for i := first; i <= last; i++ {
		if !v.layout.Geometry.Exact(i) {
			continue
		}
		if !all && !v.needsTiles(i, v.layout.rasterKey(v.document, i, render.FullPageResolution(v.scale))) {
			continue
		}
		r := v.layout.page(i)
		size := v.tileRasterSize(r)
		visible := v.tileViewport(r, size)
		if visible.Empty() {
			continue
		}
		for row := visible.Min.Y / tileSide * tileSide; row < visible.Max.Y; row += tileSide {
			for col := visible.Min.X / tileSide * tileSide; col < visible.Max.X; col += tileSide {
				box := image.Rect(col, row, min(size.X, col+tileSide), min(size.Y, row+tileSide))
				keys = append(keys, rasterKey{Document: v.document, Page: i, Rotation: v.rotation, Size: size, Tile: box})
				if len(keys) == maxTiles {
					return keys
				}
			}
		}
	}
	return keys
}

func (v *Viewer) tileViewport(r ui.Rect, size image.Point) image.Rectangle {
	x, y := float64(size.X)/float64(r.W), float64(size.Y)/float64(r.H)
	left, top := max(0, int(float64(v.scroll.X-r.X)*x)), max(0, int(float64(v.scroll.Y-r.Y)*y))
	right := min(size.X, int(math.Ceil(float64(v.scroll.X+v.width-r.X)*x)))
	bottom := min(size.Y, int(math.Ceil(float64(v.scroll.Y+v.height-r.Y)*y)))
	if left >= right || top >= bottom {
		return image.Rectangle{}
	}
	return image.Rect(left, top, right, bottom)
}
