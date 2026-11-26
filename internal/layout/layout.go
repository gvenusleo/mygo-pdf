// Package layout computes page geometry without UI or PDFium dependencies.
package layout

import (
	"sort"

	"github.com/gvenusleo/mygo-pdf/internal/render"
)

// Geometry indexes page dimensions and their cumulative extents.
// Incremental geometries share measurements when copied.
type Geometry struct {
	sizes           []render.PageSize
	heights, widths []float64
	widest, tallest float32
	lazy            *sizeIndex
}

func NewGeometry(sizes []render.PageSize) Geometry {
	if len(sizes) == 0 {
		return Geometry{}
	}
	g := Geometry{sizes: sizes, heights: make([]float64, len(sizes)+1), widths: make([]float64, len(sizes)+1)}
	for i, size := range sizes {
		g.widest = max(g.widest, size.Width)
		g.tallest = max(g.tallest, size.Height)
		g.heights[i+1] = g.heights[i] + float64(size.Height)
		g.widths[i+1] = g.widths[i] + float64(size.Width)
	}
	return g
}

func NewIncrementalGeometry(count int, estimate render.PageSize) Geometry {
	index := newSizeIndex(count, estimate)
	return Geometry{sizes: index.sizes, lazy: index}
}

func (g Geometry) Count() int { return len(g.sizes) }

func (g Geometry) Incremental() bool { return g.lazy != nil }

func (g Geometry) Exact(page int) bool {
	return g.lazy == nil || g.sizes[page].Width > 0
}

func (g Geometry) Measured() int {
	if g.lazy == nil {
		return g.Count()
	}
	return g.lazy.nodes[1].count
}

func (g Geometry) Size(page int) render.PageSize {
	if g.Exact(page) {
		return g.sizes[page]
	}
	return g.lazy.estimate
}

// SetSize adds a measurement to an incremental geometry.
func (g Geometry) SetSize(page int, size render.PageSize) {
	g.lazy.set(page, size)
}

func (g Geometry) prefix(end int, odd bool) float64 {
	if g.lazy != nil {
		return g.lazy.prefix(end, odd)
	}
	if odd {
		return g.widths[end]
	}
	return g.heights[end]
}

type Viewport struct {
	Width, Height, Zoom, Gap float32
	Rotation                 int
	FitPage                  bool
}

type Layout struct {
	Geometry      Geometry
	Scale         float64
	Width, Height float32
	Rotation      int
	gap           float64
}

type Rect struct{ X, Y, W, H float32 }

type Position struct {
	Page     int
	Fraction float32
	Edge     float32
}

func (g Geometry) Fit(vp Viewport) Layout {
	if g.Count() == 0 {
		return Layout{}
	}
	width, height, gap := float64(vp.Width), float64(vp.Height), float64(vp.Gap)
	odd := vp.Rotation%2 != 0
	widest, tallest := g.widest, g.tallest
	if g.lazy != nil {
		totals := g.lazy.aggregate(1, g.Count())
		widest, tallest = totals.widest, totals.tallest
	}
	if odd {
		widest, tallest = tallest, widest
	}
	scale := max(1, width-2*gap) / float64(widest)
	if vp.FitPage && height > 0 {
		scale = min(scale, max(1, height-2*gap)/float64(tallest))
	}
	scale *= float64(vp.Zoom)
	return Layout{Geometry: g, Scale: scale, gap: gap, Rotation: vp.Rotation,
		Width:  float32(max(width, float64(widest)*scale+2*gap)),
		Height: float32(g.prefix(g.Count(), odd)*scale + float64(g.Count()+1)*gap)}
}

func (l Layout) Size(index int) render.PageSize {
	size := l.Geometry.Size(index)
	if l.Rotation%2 != 0 {
		size.Width, size.Height = size.Height, size.Width
	}
	return size
}

func (l Layout) Page(index int) Rect {
	size := l.Size(index)
	width := float32(float64(size.Width) * l.Scale)
	return Rect{X: (l.Width - width) / 2, Y: float32(l.Top(index)), W: width, H: float32(float64(size.Height) * l.Scale)}
}

func (l Layout) Top(index int) float64 {
	return l.Geometry.prefix(index, l.Rotation%2 != 0)*l.Scale + float64(index+1)*l.gap
}

func (l Layout) Bottom(index int) float64 {
	return l.Geometry.prefix(index+1, l.Rotation%2 != 0)*l.Scale + float64(index+1)*l.gap
}

func (l Layout) FirstPage(y float32) int {
	if g := l.Geometry.lazy; g != nil {
		return g.firstPage(float64(y), l.Scale, l.gap, l.Rotation%2 != 0)
	}
	count := l.Geometry.Count()
	return min(count-1, sort.Search(count, func(i int) bool {
		return l.Bottom(i) > float64(y)
	}))
}

func (l Layout) Anchor(y float32) Position {
	if l.Geometry.Count() == 0 {
		return Position{Page: -1}
	}
	index := l.FirstPage(y)
	position := float64(y)
	top, bottom := l.Top(index), l.Bottom(index)
	if position < top {
		if index > 0 {
			previous := l.Bottom(index - 1)
			if position-previous <= top-position {
				return Position{Page: index - 1, Fraction: 1, Edge: float32(position - previous)}
			}
		}
		return Position{Page: index, Edge: float32(position - top)}
	}
	if position >= bottom {
		return Position{Page: index, Fraction: 1, Edge: float32(position - bottom)}
	}
	return Position{Page: index, Fraction: float32((position - top) / (bottom - top))}
}

func (l Layout) Offset(anchor Position) float32 {
	if anchor.Page < 0 || l.Geometry.Count() == 0 {
		return 0
	}
	index := min(anchor.Page, l.Geometry.Count()-1)
	top := l.Top(index)
	return float32(top + float64(anchor.Fraction)*(l.Bottom(index)-top) + float64(anchor.Edge))
}

func (l Layout) VisiblePages(y, height float32) (first, last int) {
	first = l.FirstPage(y)
	last = l.FirstPage(y + height)
	if last > first && l.Top(last) >= float64(y)+float64(height) {
		last--
	}
	return first, last
}
