package layout

import (
	"testing"

	"github.com/gvenusleo/mygo-pdf/internal/render"
)

func TestIncrementalGeometryMatchesEager(t *testing.T) {
	sizes := []render.PageSize{{Width: 720, Height: 480}, {Width: 200, Height: 530}, {Width: 310, Height: 200},
		{Width: 190, Height: 650}, {Width: 220, Height: 560}}
	g := NewIncrementalGeometry(len(sizes), sizes[0])
	estimated := make([]render.PageSize, len(sizes))
	for i := range estimated {
		estimated[i] = sizes[0]
	}
	for _, index := range []int{4, 1, 3, 0, 2} {
		g.SetSize(index, sizes[index])
		estimated[index] = sizes[index]
		eager := NewGeometry(estimated)
		for rotation := range 4 {
			vp := Viewport{Width: 600, Height: 400, Gap: 12, Zoom: 2, Rotation: rotation}
			got := g.Fit(vp)
			want := eager.Fit(vp)
			if got.Height != want.Height || got.Scale != want.Scale {
				t.Fatalf("lazy extents differ after measuring page %d at rotation %d: %+v vs %+v", index, rotation, got, want)
			}
			for page := range sizes {
				if got.Page(page) != want.Page(page) {
					t.Fatalf("layout differs for page %d: %v vs %v", page, got.Page(page), want.Page(page))
				}
				for _, y := range []float32{0, float32(want.Top(page)) + 1, float32(want.Bottom(page)) + 1} {
					if got.FirstPage(y) != want.FirstPage(y) {
						t.Fatalf("page lookup at %f differs: %d vs %d", y, got.FirstPage(y), want.FirstPage(y))
					}
				}
			}
		}
	}
}
