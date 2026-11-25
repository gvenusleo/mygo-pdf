package pdf

import (
	"image"
	"math"
	"slices"
	"sort"

	"github.com/egoist/mygo/ui"
	"github.com/gvenusleo/mygo-pdf/internal/render"
)

type pageLayout struct {
	pages         []ui.Rect
	width, height float32
}

type readingPosition struct {
	page     int
	fraction float32
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
	viewport, zoom, gap := vp.width, vp.zoom, vp.gap
	var widest, tallest float32
	for _, size := range sizes {
		widest = max(widest, size.Width)
		tallest = max(tallest, size.Height)
	}
	if widest == 0 {
		return pageLayout{}
	}
	width := max(1, viewport-2*gap)
	scale := width / widest
	if vp.fit == FitPage && vp.height > 0 {
		scale = min(scale, max(1, vp.height-2*gap)/tallest)
	}
	scale *= zoom
	l := pageLayout{pages: make([]ui.Rect, len(sizes)), width: max(viewport, widest*scale+2*gap)}
	y := float64(gap)
	for i, size := range sizes {
		w, h := size.Width*scale, size.Height*scale
		l.pages[i] = ui.Rect{X: (l.width - w) / 2, Y: float32(y), W: w, H: h}
		y += float64(h) + float64(gap)
	}
	l.height = float32(y)
	return l
}

func (l pageLayout) raster(doc uint64, page int, scale float64) rasterKey {
	return l.rasterAt(doc, page, scale, render.MaxPixels)
}

func (l pageLayout) preview(doc uint64, page int, zoom float32) rasterKey {
	return l.rasterAt(doc, page, min(1, 1/float64(zoom)), 1_000_000)
}

func (l pageLayout) fallback(doc uint64, page int, zoom float32, budget int) rasterKey {
	key := l.rasterAt(doc, page, min(1, 1/float64(zoom)), min(1_000_000, budget/8))
	key.Metadata = min(render.PageMetadata, budget/2)
	return key
}

func (l pageLayout) rasterAt(doc uint64, page int, scale float64, pixelLimit int) rasterKey {
	r := l.pages[page]
	return rasterKey{Document: doc, Page: page, Size: render.RasterSize(float64(r.W)*scale, float64(r.H)*scale, pixelLimit), Metadata: render.PageMetadata}
}

func (l pageLayout) firstPage(y float32) int {
	return min(len(l.pages)-1, sort.Search(len(l.pages), func(i int) bool {
		return l.pages[i].Y+l.pages[i].H > y
	}))
}

func (l pageLayout) anchor(y float32) readingPosition {
	if len(l.pages) == 0 {
		return readingPosition{}
	}
	i := l.firstPage(y)
	page := l.pages[i]
	return readingPosition{page: i, fraction: min(1, max(0, (y-page.Y)/page.H))}
}

func (l pageLayout) offset(anchor readingPosition) float32 {
	if len(l.pages) == 0 {
		return 0
	}
	page := l.pages[min(anchor.page, len(l.pages)-1)]
	if anchor.page == 0 && anchor.fraction == 0 {
		return 0
	}
	return page.Y + anchor.fraction*page.H
}

func (v *Viewer) anchor() readingPosition { return v.layout.anchor(v.scroll.Y) }

func (v *Viewer) measure(width, height, gap float32) bool {
	if width <= 0 || height <= 0 {
		return false
	}
	changed := !v.measured || v.width != width || v.height != height || v.gap != gap
	if changed {
		anchor := v.anchor()
		v.measured, v.width, v.height, v.gap = true, width, height, gap
		v.layout = newFittedLayout(v.pages, v.loadViewport())
		v.scroll.Y = min(v.layout.offset(anchor), max(0, v.layout.height-height))
		v.scroll.X = min(v.scroll.X, max(0, v.layout.width-width))
	}
	return changed
}

func (v *Viewer) setZoom(zoom, x, y float32) {
	zoom = max(0.5, min(4, zoom))
	if zoom == v.zoom || len(v.pages) == 0 {
		return
	}
	anchor := v.layout.anchor(v.scroll.Y + y)
	page := v.layout.pages[anchor.page]
	fraction := (v.scroll.X + x - page.X) / page.W
	v.zoom = zoom
	v.layout = newFittedLayout(v.pages, v.loadViewport())
	page = v.layout.pages[anchor.page]
	v.scroll.X = max(0, min(page.X+fraction*page.W-x, v.layout.width-v.width))
	v.scroll.Y = max(0, min(v.layout.offset(anchor)-y, v.layout.height-v.height))
}

func (v *Viewer) visiblePages() (first, last int) {
	return v.layout.visiblePages(v.scroll.Y, v.height)
}

func (l pageLayout) visiblePages(y, height float32) (first, last int) {
	first = l.firstPage(y)
	last = l.firstPage(y + height)
	if last > first && l.pages[last].Y >= y+height {
		last--
	}
	return first, last
}

// Reserve room for prefetching so visible pages can keep their fallback images.
const visibleBudget = render.BitmapBudget * 3 / 4

func (v *Viewer) requestVisible(first, last int) {
	pageBudget := visibleBudget / (last - first + 1)
	var fullBytes int
	for i := first; i <= last; i++ {
		size := v.layout.raster(v.document, i, v.scale).Size
		fullBytes += size.X*size.Y*4 + render.PageMetadata
	}
	tileAll := fullBytes > visibleBudget
	var requests, refinements []rasterKey
	for i := first; i <= last; i++ {
		key := v.layout.raster(v.document, i, v.scale)
		entry := v.cache[i]
		if tileAll || v.needsTiles(i, key) {
			if entry.frame != nil && entry.frame.key.Document == v.document && entry.frame.frameBytes() <= pageBudget {
				v.recency.MoveToFront(entry.recency)
				continue
			}
			key = v.layout.fallback(v.document, i, v.zoom, pageBudget)
		}
		if entry.frame == nil {
			requests = v.requestPage(requests, key)
		} else {
			refinements = v.requestPage(refinements, key)
		}
	}
	requests = append(requests, v.requestTiles(first, last, tileAll)...)
	requests = append(requests, refinements...)
	if next := last + 1; next < len(v.pages) && v.cache[next].frame == nil {
		requests = v.requestPage(requests, v.layout.preview(v.document, next, v.zoom))
	}
	v.setWanted(requests)
}

func (v *Viewer) requestTiles(first, last int, all bool) []rasterKey {
	v.tileKeys = v.visibleTiles(first, last, all)
	for key := range v.tiles {
		if !slices.Contains(v.tileKeys, key) {
			delete(v.tiles, key)
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

func (v *Viewer) needsTiles(page int, full rasterKey) bool {
	r := v.layout.pages[page]
	return float64(full.Size.X)+1 < float64(r.W)*v.scale || float64(full.Size.Y)+1 < float64(r.H)*v.scale
}

const tileSide = 512

func (v *Viewer) visibleTiles(first, last int, all bool) []rasterKey {
	var keys []rasterKey
	const maxTiles = render.TileBudget / (tileSide * tileSide * 4)
	area := float64(v.width) * float64(v.height)
	// Allow for tiles that cross the viewport edge when budgeting large viewports.
	scale := min(v.scale, math.Sqrt(float64(render.TileBudget)*0.6/(4*max(1, area))))
	for i := first; i <= last; i++ {
		if !all && !v.needsTiles(i, v.layout.raster(v.document, i, v.scale)) {
			continue
		}
		r := v.layout.pages[i]
		// Bound PDFium's signed full-page dimensions independently of the tile size.
		pageScale := min(scale, float64(1<<24)/float64(max(r.W, r.H)))
		size := image.Pt(max(1, int(math.Ceil(float64(r.W)*pageScale))), max(1, int(math.Ceil(float64(r.H)*pageScale))))
		x, y := float64(size.X)/float64(r.W), float64(size.Y)/float64(r.H)
		left, top := max(0, int(float64(v.scroll.X-r.X)*x)), max(0, int(float64(v.scroll.Y-r.Y)*y))
		right := min(size.X, int(math.Ceil(float64(v.scroll.X+v.width-r.X)*x)))
		bottom := min(size.Y, int(math.Ceil(float64(v.scroll.Y+v.height-r.Y)*y)))
		if left >= right || top >= bottom {
			continue
		}
		for row := top / tileSide * tileSide; row < bottom; row += tileSide {
			for col := left / tileSide * tileSide; col < right; col += tileSide {
				box := image.Rect(col, row, min(size.X, col+tileSide), min(size.Y, row+tileSide))
				keys = append(keys, rasterKey{Document: v.document, Page: i, Size: size, Tile: box})
				if len(keys) == maxTiles {
					return keys
				}
			}
		}
	}
	return keys
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
