package pdf

import (
	"image"
	"math"
	"slices"
	"sort"

	"github.com/egoist/mygo/ui"
	"github.com/gvenusleo/mygo-pdf/internal/render"
)

type pageGeometry struct {
	sizes           []pageSize
	heights, widths []float64
	widest, tallest float32
	lazy            *sizeIndex
}

type pageLayout struct {
	geometry      pageGeometry
	scale, gap    float64
	width, height float32
	rotation      int
}

type readingPosition struct {
	page     int
	fraction float32
	edge     float32
}

// A page can be selected even when the viewport cannot scroll far enough to center it.
type pageNavigation struct {
	page int
	y    float32
}

func (v *Viewer) navigateTo(page int, y float32) {
	v.scroll.Y = max(0, min(y, v.layout.height-v.height))
	v.navigation = pageNavigation{page: page, y: v.scroll.Y}
}

func (v *Viewer) selectedPage() int {
	if v.scroll.Y == v.navigation.y {
		return v.navigation.page
	}
	return 0
}

func newFittedLayout(sizes []pageSize, vp loadViewport) pageLayout {
	if len(sizes) == 0 {
		return pageLayout{}
	}
	g := pageGeometry{sizes: sizes, heights: make([]float64, len(sizes)+1), widths: make([]float64, len(sizes)+1)}
	for i, size := range sizes {
		g.widest = max(g.widest, size.Width)
		g.tallest = max(g.tallest, size.Height)
		g.heights[i+1] = g.heights[i] + float64(size.Height)
		g.widths[i+1] = g.widths[i] + float64(size.Width)
	}
	return g.fit(vp)
}

func (g pageGeometry) fit(vp loadViewport) pageLayout {
	if len(g.sizes) == 0 {
		return pageLayout{}
	}
	width, height, gap := float64(vp.width), float64(vp.height), float64(vp.gap)
	odd := vp.rotation%2 != 0
	widest, tallest := g.widest, g.tallest
	if g.lazy != nil {
		totals := g.lazy.aggregate(1, len(g.sizes))
		widest, tallest = totals.widest, totals.tallest
	}
	if odd {
		widest, tallest = tallest, widest
	}
	scale := max(1, width-2*gap) / float64(widest)
	if vp.fit == FitPage && height > 0 {
		scale = min(scale, max(1, height-2*gap)/float64(tallest))
	}
	scale *= float64(vp.zoom)
	return pageLayout{geometry: g, scale: scale, gap: gap, rotation: vp.rotation,
		width:  float32(max(width, float64(widest)*scale+2*gap)),
		height: float32(g.prefix(len(g.sizes), odd)*scale + float64(len(g.sizes)+1)*gap)}
}

func (g pageGeometry) prefix(end int, odd bool) float64 {
	if g.lazy != nil {
		return g.lazy.prefix(end, odd)
	}
	if odd {
		return g.widths[end]
	}
	return g.heights[end]
}

func (l pageLayout) size(index int) pageSize {
	size := l.geometry.size(index)
	if l.rotation%2 != 0 {
		size.Width, size.Height = size.Height, size.Width
	}
	return size
}

func (l pageLayout) page(index int) ui.Rect {
	size := l.size(index)
	width := float32(float64(size.Width) * l.scale)
	return ui.Rect{X: (l.width - width) / 2, Y: float32(l.top(index)), W: width, H: float32(float64(size.Height) * l.scale)}
}

func (l pageLayout) top(index int) float64 {
	return l.geometry.prefix(index, l.rotation%2 != 0)*l.scale + float64(index+1)*l.gap
}

func (l pageLayout) bottom(index int) float64 {
	return l.geometry.prefix(index+1, l.rotation%2 != 0)*l.scale + float64(index+1)*l.gap
}

func (l pageLayout) raster(doc uint64, page int, scale float64) rasterKey {
	return l.rasterAt(doc, page, scale, render.MaxPixels)
}

func (l pageLayout) preview(doc uint64, page int, zoom float32) rasterKey {
	return l.rasterAt(doc, page, min(1, 1/float64(zoom)), 1_000_000)
}

func (l pageLayout) fallback(doc uint64, page int, zoom float32, budget int) rasterKey {
	return l.rasterAt(doc, page, min(1, 1/float64(zoom)), min(1_000_000, budget/8))
}

func (l pageLayout) rasterAt(doc uint64, page int, scale float64, pixelLimit int) rasterKey {
	r := l.page(page)
	return rasterKey{Document: doc, Page: page, Rotation: l.rotation, Size: render.RasterSize(float64(r.W)*scale, float64(r.H)*scale, pixelLimit)}
}

func (l pageLayout) firstPage(y float32) int {
	if g := l.geometry.lazy; g != nil {
		return g.firstPage(float64(y), l.scale, l.gap, l.rotation%2 != 0)
	}
	count := len(l.geometry.sizes)
	return min(count-1, sort.Search(count, func(i int) bool {
		return l.bottom(i) > float64(y)
	}))
}

func (l pageLayout) anchor(y float32) readingPosition {
	if len(l.geometry.sizes) == 0 {
		return readingPosition{page: -1}
	}
	index := l.firstPage(y)
	position := float64(y)
	top, bottom := l.top(index), l.bottom(index)
	if position < top {
		if index > 0 {
			previous := l.bottom(index - 1)
			if position-previous <= top-position {
				return readingPosition{page: index - 1, fraction: 1, edge: float32(position - previous)}
			}
		}
		return readingPosition{page: index, edge: float32(position - top)}
	}
	if position >= bottom {
		return readingPosition{page: index, fraction: 1, edge: float32(position - bottom)}
	}
	return readingPosition{page: index, fraction: float32((position - top) / (bottom - top))}
}

func (l pageLayout) offset(anchor readingPosition) float32 {
	if anchor.page < 0 || len(l.geometry.sizes) == 0 {
		return 0
	}
	index := min(anchor.page, len(l.geometry.sizes)-1)
	top := l.top(index)
	return float32(top + float64(anchor.fraction)*(l.bottom(index)-top) + float64(anchor.edge))
}

func (v *Viewer) anchor() readingPosition {
	if v.scroll.Y == 0 {
		return readingPosition{page: -1}
	}
	return v.layout.anchor(v.scroll.Y)
}

func (v *Viewer) measure(width, height, gap float32) bool {
	if width <= 0 || height <= 0 {
		return false
	}
	changed := !v.measured || v.width != width || v.height != height || v.gap != gap
	if changed {
		anchor := v.anchor()
		v.measured, v.width, v.height, v.gap = true, width, height, gap
		v.layout = v.layout.geometry.fit(v.loadViewport())
		v.scroll.Y = max(0, min(v.layout.offset(anchor), v.layout.height-height))
		v.scroll.X = min(v.scroll.X, max(0, v.layout.width-width))
	}
	return changed
}

func (v *Viewer) setZoom(zoom, x, y float32) {
	zoom = max(0.5, min(4, zoom))
	if zoom == v.zoom || math.IsNaN(float64(zoom)) || len(v.pages) == 0 {
		return
	}
	anchor := v.layout.anchor(v.scroll.Y + y)
	index := max(0, anchor.page)
	page := v.layout.page(index)
	fraction := (v.scroll.X + x - page.X) / page.W
	v.zoom = zoom
	v.layout = v.layout.geometry.fit(v.loadViewport())
	page = v.layout.page(index)
	v.scroll.X = max(0, min(page.X+fraction*page.W-x, v.layout.width-v.width))
	v.scroll.Y = max(0, min(v.layout.offset(anchor)-y, v.layout.height-v.height))
}

func (v *Viewer) visiblePages() (first, last int) {
	return v.layout.visiblePages(v.scroll.Y, v.height)
}

func (l pageLayout) visiblePages(y, height float32) (first, last int) {
	first = l.firstPage(y)
	last = l.firstPage(y + height)
	if last > first && l.top(last) >= float64(y)+float64(height) {
		last--
	}
	return first, last
}

// Reserve room for prefetching so visible pages can keep their fallback images.
const visibleBudget = render.BitmapBudget * 3 / 4

type pageRasterDemand struct {
	budget  int
	tileAll bool
}

func (v *Viewer) pageRasterDemand(first, last int) pageRasterDemand {
	var fullBytes int
	for i := first; i <= last; i++ {
		size := v.layout.raster(v.document, i, v.scale).Size
		fullBytes += size.X * size.Y * 4
	}
	return pageRasterDemand{budget: visibleBudget / (last - first + 1), tileAll: fullBytes > visibleBudget}
}

func (v *Viewer) visiblePageRaster(page int, demand pageRasterDemand) rasterKey {
	key := v.layout.raster(v.document, page, v.scale)
	if !demand.tileAll && !v.needsTiles(page, key) {
		return key
	}
	if frame := v.cache[page].frame; frame != nil && frame.key.Document == v.document && frame.key.Rotation == v.rotation && frame.frameBytes() <= demand.budget {
		return frame.key
	}
	return v.layout.fallback(v.document, page, v.zoom, demand.budget)
}

func (v *Viewer) pageRasterError(page int, demand pageRasterDemand) error {
	if g := v.layout.geometry.lazy; g != nil && g.failed[page] != nil {
		return g.failed[page]
	}
	entry := v.cache[page]
	if entry.err != nil && entry.failed == v.visiblePageRaster(page, demand) {
		return entry.err
	}
	return nil
}

func (v *Viewer) requestVisible(first, last int) pageRasterDemand {
	if v.scroll.Y != v.lastScrollY {
		v.scrollingUp = v.scroll.Y < v.lastScrollY
		v.lastScrollY = v.scroll.Y
	}
	next := v.prefetchPage(first, last)
	v.requestDimensions(first, last, next)
	demand := v.pageRasterDemand(first, last)
	var requests, refinements []rasterKey
	for i := first; i <= last; i++ {
		if !v.layout.geometry.exact(i) {
			continue
		}
		key := v.visiblePageRaster(i, demand)
		if v.cache[i].frame == nil {
			requests = v.requestPage(requests, key)
		} else {
			refinements = v.requestPage(refinements, key)
		}
	}
	missing := len(requests)
	requests = append(requests, v.requestTiles(first, last, demand.tileAll)...)
	requests = append(requests, refinements...)
	if next >= 0 && next < len(v.pages) && v.layout.geometry.exact(next) && v.cache[next].frame == nil {
		requests = v.requestPage(requests, v.layout.preview(v.document, next, v.zoom))
	}
	v.setRasterDemand(requests, next, missing, v.metadataRequests(first, last))
	return demand
}

func (v *Viewer) prefetchPage(first, last int) int {
	if v.scrollingUp {
		return first - 1
	}
	return last + 1
}

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
		if !v.layout.geometry.exact(i) {
			continue
		}
		if !all && !v.needsTiles(i, v.layout.raster(v.document, i, v.scale)) {
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

func (v *Viewer) requestPage(keys []rasterKey, key rasterKey) []rasterKey {
	entry := v.cache[key.Page]
	if entry.frame != nil {
		v.recency.MoveToFront(entry.recency)
		if entry.frame.key == key {
			return keys
		}
	}
	if entry.failed == key {
		return keys
	}
	return append(keys, key)
}
