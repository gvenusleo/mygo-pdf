package render

import (
	"image"
	"testing"
)

func TestPageResolution(t *testing.T) {
	for _, test := range []struct {
		name          string
		resolution    PageResolution
		width, height float32
		want          image.Point
	}{
		{"display scale", FullPageResolution(2), 600, 800, image.Pt(1200, 1600)},
		{"full-page limit", FullPageResolution(2), 2000, 2000, image.Pt(2000, 2000)},
		{"preview at fit scale", PreviewResolution(2), 1200, 1600, image.Pt(600, 800)},
		{"preview below fit scale", PreviewResolution(0.5), 600, 800, image.Pt(600, 800)},
		{"preview limit", PreviewResolution(2), 4000, 4000, image.Pt(1000, 1000)},
		{"fallback budget", FallbackResolution(2, 2_000_000), 4000, 4000, image.Pt(500, 500)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.resolution.Size(test.width, test.height); got != test.want {
				t.Errorf("raster size = %v, want %v", got, test.want)
			}
		})
	}
}
