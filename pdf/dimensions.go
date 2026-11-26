package pdf

import (
	"context"

	"github.com/gvenusleo/mygo-pdf/internal/layout"
	"github.com/gvenusleo/mygo-pdf/internal/pdfium"
)

func prepareLayout(e *engine, ctx context.Context, doc *document, vp loadViewport, g layout.Geometry) (loadPlan, error) {
	if doc.Pages[0].Width > 0 {
		if g.Count() == 0 {
			g = layout.NewGeometry(doc.Pages)
		}
		return vp.plan(doc.ID, fitLayout(g, vp.Viewport)), nil
	}
	if !g.Incremental() {
		index := min(max(0, vp.anchor.Page), len(doc.Pages)-1)
		size, err := e.ReadPageSize(ctx, doc, index, false)
		if err != nil {
			return loadPlan{}, err
		}
		g = layout.NewIncrementalGeometry(len(doc.Pages), size)
		g.SetSize(index, size)
	}
	for {
		fitted := fitLayout(g, vp.Viewport)
		y := fitted.viewportOffset(vp)
		first, last := fitted.VisiblePages(y, vp.Height)
		measured := false
		for page := first; page <= last; page++ {
			if g.Exact(page) {
				continue
			}
			size, err := e.ReadPageSize(ctx, doc, page, false)
			if err != nil {
				return loadPlan{}, err
			}
			g.SetSize(page, size)
			measured = true
		}
		if !measured {
			return vp.plan(doc.ID, fitted), nil
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
	g := v.layout.Geometry
	if g.Exact(page) {
		complete(nil)
	} else if err := v.dimensionErrors[page]; err != nil {
		complete(err)
	} else {
		v.pendingNavigation = &pendingNavigation{page: page, x: v.scroll.X, y: v.scroll.Y, complete: complete}
		first, last := v.visiblePages()
		v.requestDimensions(first, last, v.prefetchPage(first, last))
	}
	v.redraw()
}

func (v *Viewer) requestDimensions(first, last, prefetch int) {
	g := v.layout.Geometry
	if !g.Incremental() {
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
		if !g.Exact(page) && v.dimensionErrors[page] == nil && (len(wanted) == 0 || wanted[0] != page) {
			wanted = append(wanted, page)
		}
	}
	if prefetch < 0 || prefetch >= g.Count() || g.Exact(prefetch) || v.dimensionErrors[prefetch] != nil {
		prefetch = -1
	}
	v.worker.requestSizes(v.document, wanted, prefetch)
}

func (v *Viewer) finishSize(key geometryKey, size pageSize, err error) bool {
	g := v.layout.Geometry
	if key.document != v.document || !g.Incremental() {
		return false
	}
	if g.Exact(key.page) {
		return true
	}
	nav := v.pendingNavigation
	if nav != nil && (nav.x != v.scroll.X || nav.y != v.scroll.Y) {
		v.pendingNavigation, nav = nil, nil
	}
	if err != nil {
		if v.dimensionErrors == nil {
			v.dimensionErrors = make(map[int]error)
		}
		v.dimensionErrors[key.page] = err
	} else if size == g.Size(key.page) {
		g.SetSize(key.page, size)
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

func (v *Viewer) applyDimension(g layout.Geometry, page int, size pageSize) {
	anchor, selected := v.centerAnchor(), v.selectedPage()
	g.SetSize(page, size)
	v.layout = fitLayout(g, v.viewport())
	v.restoreCenter(anchor)
	if selected > 0 {
		v.navigation = pageNavigation{selected, v.scroll.Y}
	}
	v.lastScrollY = v.scroll.Y
}

// The center's canonical page point survives geometry, fit, and rotation changes.
type viewportAnchor struct {
	layout.Position
	point    pdfium.Point
	rotation int
}

func (v *Viewer) centerAnchor() viewportAnchor {
	anchor := v.layout.Anchor(v.scroll.Y + v.height/2)
	if anchor.Page < 0 {
		return viewportAnchor{Position: anchor}
	}
	r := v.layout.page(anchor.Page)
	return viewportAnchor{anchor, pdfium.Point{X: (v.scroll.X + v.width/2 - r.X) / r.W, Y: anchor.Fraction}, v.layout.Rotation}
}

func (v *Viewer) restoreCenter(anchor viewportAnchor) {
	if anchor.Page < 0 {
		return
	}
	page := v.layout.page(anchor.Page)
	point := rotatedPoint(anchor.point, v.layout.Rotation-anchor.rotation)
	anchor.Fraction = point.Y
	v.scroll.X = max(0, min(page.X+point.X*page.W-v.width/2, v.layout.Width-v.width))
	v.scroll.Y = max(0, min(v.layout.Offset(anchor.Position)-v.height/2, v.layout.Height-v.height))
}
