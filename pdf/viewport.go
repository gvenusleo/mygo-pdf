package pdf

import (
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/gvenusleo/mygo-pdf/internal/layout"
)

type pageLayout struct{ layout.Layout }

func fitLayout(g layout.Geometry, vp layout.Viewport) pageLayout {
	return pageLayout{g.Fit(vp)}
}

func (v *Viewer) viewport() layout.Viewport {
	return layout.Viewport{
		Width: v.width, Height: v.height, Zoom: v.zoom, Gap: v.gap, FitPage: v.fit == FitPage, Rotation: v.rotation,
	}
}

func (l pageLayout) page(index int) ui.Rect {
	return ui.Rect(l.Page(index))
}

// A page can be selected even when the viewport cannot scroll far enough to center it.
type pageNavigation struct {
	page int
	y    float32
}

func (v *Viewer) navigateTo(page int, y float32) {
	v.scroll.Y = max(0, min(y, v.layout.Height-v.height))
	v.navigation = pageNavigation{page: page, y: v.scroll.Y}
}

func (v *Viewer) selectedPage() int {
	if v.scroll.Y == v.navigation.y {
		return v.navigation.page
	}
	return 0
}

func (v *Viewer) anchor() layout.Position {
	if v.scroll.Y == 0 {
		return layout.Position{Page: -1}
	}
	return v.layout.Anchor(v.scroll.Y)
}

func (v *Viewer) measure(width, height, gap float32) bool {
	if width <= 0 || height <= 0 {
		return false
	}
	changed := !v.measured || v.width != width || v.height != height || v.gap != gap
	if changed {
		anchor := v.anchor()
		v.measured, v.width, v.height, v.gap = true, width, height, gap
		v.layout = fitLayout(v.layout.Geometry, v.viewport())
		v.scroll.Y = max(0, min(v.layout.Offset(anchor), v.layout.Height-height))
		v.scroll.X = min(v.scroll.X, max(0, v.layout.Width-width))
	}
	return changed
}

func (v *Viewer) setZoom(zoom, x, y float32) {
	zoom = max(0.5, min(4, zoom))
	if zoom == v.zoom || math.IsNaN(float64(zoom)) || len(v.pages) == 0 {
		return
	}
	anchor := v.layout.Anchor(v.scroll.Y + y)
	index := max(0, anchor.Page)
	page := v.layout.page(index)
	fraction := (v.scroll.X + x - page.X) / page.W
	v.zoom = zoom
	v.layout = fitLayout(v.layout.Geometry, v.viewport())
	page = v.layout.page(index)
	v.scroll.X = max(0, min(page.X+fraction*page.W-x, v.layout.Width-v.width))
	v.scroll.Y = max(0, min(v.layout.Offset(anchor)-y, v.layout.Height-v.height))
}

func (v *Viewer) visiblePages() (first, last int) {
	return v.layout.VisiblePages(v.scroll.Y, v.height)
}
