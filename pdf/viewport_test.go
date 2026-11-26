package pdf

import (
	"math"
	"testing"

	"github.com/gvenusleo/mygo-pdf/internal/layout"
)

func TestZoomKeepsPointerAnchor(t *testing.T) {
	for _, tc := range []struct {
		name   string
		offset float32
	}{
		{"inside page", -200},
		{"gap near previous page", 5},
		{"gap near next page", 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := &Viewer{width: 600, height: 450, gap: 20, zoom: 1}
			v.pages = []pageSize{{Width: 550, Height: 850}, {Width: 600, Height: 700}, {Width: 400, Height: 800}}
			v.layout = fitLayout(layout.NewGeometry(v.pages), v.viewport())
			const x, y = 370, 220
			point := float32(v.layout.Bottom(0)) + tc.offset
			v.scroll.Y = point - y
			before := v.layout.Anchor(point)
			page := v.layout.page(before.Page)
			fractionX := (x - page.X) / page.W
			v.setZoom(1.75, x, y)
			after := v.layout.Anchor(v.scroll.Y + y)
			page = v.layout.page(after.Page)
			afterX := (v.scroll.X + x - page.X) / page.W
			if after.Page != before.Page || math.Abs(float64(after.Fraction-before.Fraction)) > 0.001 ||
				math.Abs(float64(after.Edge-before.Edge)) > 0.01 || math.Abs(float64(afterX-fractionX)) > 0.001 {
				t.Fatalf("pointer anchor moved: before %+v at X=%v, after %+v at X=%v", before, fractionX, after, afterX)
			}
		})
	}
}

func BenchmarkZoom10000Pages(b *testing.B) {
	v := &Viewer{width: 900, height: 700, gap: 12, zoom: 1}
	v.pages = make([]pageSize, 10_000)
	for i := range v.pages {
		v.pages[i] = pageSize{Width: 595, Height: float32(700 + i%143)}
	}
	v.layout = fitLayout(layout.NewGeometry(v.pages), v.viewport())
	v.scroll.Y = v.layout.Offset(layout.Position{Page: 5000, Fraction: 0.2})
	b.ReportAllocs()
	for b.Loop() {
		v.setZoom(1.5, 450, 350)
		v.setZoom(1, 450, 350)
	}
	b.ReportMetric(2, "zooms/op")
}
