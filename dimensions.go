package pdf

import (
	"context"
	"math/bits"

	"github.com/gvenusleo/mygo-pdf/internal/pdfium"
)

type sizeTotals struct {
	count           int
	width, height   float64
	widest, tallest float32
}

func addSizes(a, b sizeTotals) sizeTotals {
	return sizeTotals{a.count + b.count, a.width + b.width, a.height + b.height,
		max(a.widest, b.widest), max(a.tallest, b.tallest)}
}

// Unknown leaves use the opening page size until their measurements arrive.
type sizeIndex struct {
	estimate pageSize
	sizes    []pageSize
	nodes    []sizeTotals
	leaves   int
	failed   map[int]error
}

func newSizeIndex(count int, estimate pageSize) *sizeIndex {
	leaves := 1 << bits.Len(uint(count-1))
	return &sizeIndex{estimate: estimate, sizes: make([]pageSize, count),
		nodes: make([]sizeTotals, 2*leaves), leaves: leaves}
}

func (g *sizeIndex) set(index int, size pageSize) {
	g.sizes[index] = size
	i := g.leaves + index
	g.nodes[i] = sizeTotals{1, float64(size.Width), float64(size.Height), size.Width, size.Height}
	for i /= 2; i > 0; i /= 2 {
		g.nodes[i] = addSizes(g.nodes[2*i], g.nodes[2*i+1])
	}
}

func (g *sizeIndex) aggregate(node, count int) sizeTotals {
	value := g.nodes[node]
	unknown := count - value.count
	if unknown > 0 {
		value.width += float64(unknown) * float64(g.estimate.Width)
		value.height += float64(unknown) * float64(g.estimate.Height)
		value.widest = max(value.widest, g.estimate.Width)
		value.tallest = max(value.tallest, g.estimate.Height)
	}
	return value
}

func (t sizeTotals) extent(rotated bool) float64 {
	if rotated {
		return t.width
	}
	return t.height
}

func (g *sizeIndex) prefix(end int, rotated bool) float64 {
	var sum float64
	for i, span := g.leaves+end, 1; i > 1; i, span = i/2, span*2 {
		if i%2 == 1 {
			sum += g.aggregate(i-1, span).extent(rotated)
		}
	}
	if end == g.leaves {
		return g.aggregate(1, end).extent(rotated)
	}
	return sum
}

func (g *sizeIndex) firstPage(y, scale, gap float64, rotated bool) int {
	node, first, span := 1, 0, g.leaves
	for span > 1 {
		span /= 2
		leftCount := min(span, len(g.sizes)-first)
		bottom := scale*g.aggregate(node*2, leftCount).extent(rotated) + gap*float64(leftCount)
		node *= 2
		if y >= bottom && first+leftCount < len(g.sizes) {
			node++
			first += leftCount
			y -= bottom
		}
	}
	return first
}

func (g pageGeometry) exact(page int) bool {
	return g.lazy == nil || g.lazy.sizes[page].Width > 0
}

func (g pageGeometry) size(page int) pageSize {
	if g.lazy == nil {
		return g.sizes[page]
	}
	if size := g.lazy.sizes[page]; size.Width > 0 {
		return size
	}
	return g.lazy.estimate
}

func prepareLayout(e *engine, ctx context.Context, doc *document, vp loadViewport, g pageGeometry) (loadPlan, error) {
	if doc.Pages[0].Width > 0 {
		if len(g.sizes) == 0 {
			return vp.plan(doc.ID, newFittedLayout(doc.Pages, vp)), nil
		}
		return vp.plan(doc.ID, g.fit(vp)), nil
	}
	if g.lazy == nil {
		index := min(max(0, vp.anchor.page), len(doc.Pages)-1)
		size, err := e.ReadPageSize(ctx, doc, index, false)
		if err != nil {
			return loadPlan{}, err
		}
		g = pageGeometry{sizes: doc.Pages, lazy: newSizeIndex(len(doc.Pages), size)}
		g.lazy.set(index, size)
	}
	for {
		layout := g.fit(vp)
		y := layout.viewportOffset(vp)
		first, last := layout.visiblePages(y, vp.height)
		measured := false
		for page := first; page <= last; page++ {
			if g.exact(page) {
				continue
			}
			size, err := e.ReadPageSize(ctx, doc, page, false)
			if err != nil {
				return loadPlan{}, err
			}
			g.lazy.set(page, size)
			measured = true
		}
		if !measured {
			return vp.plan(doc.ID, layout), nil
		}
	}
}

type pendingNavigation struct {
	page     int
	x, y     float32
	complete func(error)
	find     bool
}

func (v *Viewer) navigateToWhenSized(page int, commit func()) {
	v.interactionErr = nil
	v.navigateWhenSized(page, func(err error) {
		if err != nil {
			v.interactionErr = err
			return
		}
		commit()
	})
}

func (v *Viewer) navigateWhenSized(page int, complete func(error)) {
	v.pendingNavigation = nil
	g := v.layout.geometry
	if g.exact(page) {
		complete(nil)
	} else if err := g.lazy.failed[page]; err != nil {
		complete(err)
	} else {
		v.pendingNavigation = &pendingNavigation{page: page, x: v.scroll.X, y: v.scroll.Y, complete: complete}
		first, last := v.visiblePages()
		v.requestDimensions(first, last, v.prefetchPage(first, last))
	}
	v.redraw()
}

func (v *Viewer) requestDimensions(first, last, prefetch int) {
	g := v.layout.geometry
	if g.lazy == nil {
		return
	}
	var wanted []int
	if nav := v.pendingNavigation; nav != nil {
		if nav.x != v.scroll.X || nav.y != v.scroll.Y {
			v.pendingNavigation = nil
		} else {
			wanted = append(wanted, nav.page)
		}
	}
	for page := first; page <= last; page++ {
		if !g.exact(page) && g.lazy.failed[page] == nil && (len(wanted) == 0 || wanted[0] != page) {
			wanted = append(wanted, page)
		}
	}
	if prefetch < 0 || prefetch >= len(g.sizes) || g.exact(prefetch) || g.lazy.failed[prefetch] != nil {
		prefetch = -1
	}
	v.worker.requestSizes(v.document, wanted, prefetch)
}

func (v *Viewer) finishSize(key geometryKey, size pageSize, err error) bool {
	g := v.layout.geometry
	if key.document != v.document || g.lazy == nil {
		return false
	}
	if g.exact(key.page) {
		return true
	}
	nav := v.pendingNavigation
	if nav != nil && (nav.x != v.scroll.X || nav.y != v.scroll.Y) {
		v.pendingNavigation, nav = nil, nil
	}
	if err != nil {
		if g.lazy.failed == nil {
			g.lazy.failed = make(map[int]error)
		}
		g.lazy.failed[key.page] = err
	} else if size == g.lazy.estimate {
		g.lazy.set(key.page, size)
		first, last := v.visiblePages()
		if (nav == nil || nav.page != key.page) && (key.page < first || key.page > last) && key.page != v.prefetchPage(first, last) {
			return true
		}
	} else {
		v.applyDimension(g, key.page, size)
	}
	if nav != nil {
		nav.x, nav.y = v.scroll.X, v.scroll.Y
		if nav.page == key.page {
			v.pendingNavigation = nil
			nav.complete(err)
		}
	}
	v.redraw()
	return true
}

func (v *Viewer) applyDimension(g pageGeometry, page int, size pageSize) {
	anchor, selected := v.centerAnchor(), v.selectedPage()
	g.lazy.set(page, size)
	v.layout = g.fit(v.loadViewport())
	v.restoreCenter(anchor)
	if selected > 0 {
		v.navigation = pageNavigation{selected, v.scroll.Y}
	}
	v.lastScrollY = v.scroll.Y
}

// The center's canonical page point survives geometry, fit, and rotation changes.
type viewportAnchor struct {
	readingPosition
	point    pdfium.Point
	rotation int
}

func (v *Viewer) centerAnchor() viewportAnchor {
	anchor := v.layout.anchor(v.scroll.Y + v.height/2)
	if anchor.page < 0 {
		return viewportAnchor{readingPosition: anchor}
	}
	r := v.layout.page(anchor.page)
	return viewportAnchor{anchor, pdfium.Point{X: (v.scroll.X + v.width/2 - r.X) / r.W, Y: anchor.fraction}, v.layout.rotation}
}

func (v *Viewer) restoreCenter(anchor viewportAnchor) {
	if anchor.page < 0 {
		return
	}
	page := v.layout.page(anchor.page)
	point := rotatedPoint(anchor.point, v.layout.rotation-anchor.rotation)
	anchor.fraction = point.Y
	v.scroll.X = max(0, min(page.X+point.X*page.W-v.width/2, v.layout.width-v.width))
	v.scroll.Y = max(0, min(v.layout.offset(anchor.readingPosition)-v.height/2, v.layout.height-v.height))
}
