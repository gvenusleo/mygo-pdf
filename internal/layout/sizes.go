package layout

import (
	"math/bits"

	"github.com/gvenusleo/mygo-pdf/internal/render"
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
	estimate render.PageSize
	sizes    []render.PageSize
	nodes    []sizeTotals
	leaves   int
}

func newSizeIndex(count int, estimate render.PageSize) *sizeIndex {
	leaves := 1 << bits.Len(uint(count-1))
	return &sizeIndex{estimate: estimate, sizes: make([]render.PageSize, count),
		nodes: make([]sizeTotals, 2*leaves), leaves: leaves}
}

func (g *sizeIndex) set(index int, size render.PageSize) {
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
