package pdf

import (
	"fmt"
	"math"
	"testing"

	"github.com/gvenusleo/mygo-pdf/internal/pdfium"
)

func TestInternalDestinationModes(t *testing.T) {
	v, view, updates := newTestView(t, 500, 480)
	v.LoadBytes(t.Context(), destinationPDF(), OpenDocument)
	await(t, updates, view, func() bool { return v.metadata[0].data != nil })
	links := v.metadata[0].data.Links
	if len(links) != 11 {
		t.Fatalf("got %d destination links, want 11", len(links))
	}
	w, h := float64(v.width-2*v.gap), float64(v.height-2*v.gap)
	tests := []struct {
		mode  pdfium.DestinationMode
		scale float64
	}{
		{pdfium.DestinationXYZ, 2},
		{pdfium.DestinationFit, min(w/300, h/430)},
		{pdfium.DestinationFitH, w / 300},
		{pdfium.DestinationFitV, h / 430},
		{pdfium.DestinationFitR, min(w/150, h/200)},
		{pdfium.DestinationFitB, min(w/150, h/200)},
		{pdfium.DestinationFitBH, w / 150},
		{pdfium.DestinationFitBV, h / 200},
		{pdfium.DestinationXYZ, w / 390},
		{pdfium.DestinationFitR, min(w/300, h/430)},
		{pdfium.DestinationXYZ, 2},
	}
	for i, tc := range tests {
		v.SetZoom(1)
		v.GoToPage(1)
		view.Frame()
		link := links[i]
		target := 1
		if i == 10 {
			target = 2
			if !link.Target.HasX || link.Target.HasY || math.Abs(float64(link.Target.Position.X-280.0/390)) > 0.001 {
				t.Errorf("rotated and cropped destination = %+v, want only screen X", link.Target)
			}
		}
		if link.Page != target || link.Target.Mode != tc.mode {
			t.Fatalf("link %d: %+v", i, link)
		}
		shown, ok := view.Find("Page 1")
		if !ok {
			t.Fatal("source page is not visible")
		}
		page := v.layout.page(0)
		view.ClickAt(shown.X+(link.Bounds.X+link.Bounds.W/2)*page.W, shown.Y+(link.Bounds.Y+link.Bounds.H/2)*page.H)
		if v.State().Page != target+1 || math.Abs(v.layout.scale-tc.scale) > 0.005 {
			t.Errorf("link %d selected page %d at scale %.4f, want page %d at %.4f", i, v.State().Page, v.layout.scale, target+1, tc.scale)
		}
		if i == 0 {
			page := v.layout.page(1)
			x := min(v.layout.width-v.width, page.X+80*2)
			if math.Abs(float64(v.scroll.X-x)) > 1 || math.Abs(float64(v.scroll.Y-(page.Y+130*2))) > 1 {
				t.Errorf("XYZ scroll = (%v, %v), want PDF coordinates (80, 300)", v.scroll.X, v.scroll.Y)
			}
		}
	}
}

func destinationPDF() []byte {
	stream := "1 0 0 rg 50 80 150 200 re f\n"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /Dests << /named [4 0 R /XYZ null null 0] >> >>",
		"<< /Type /Pages /Count 4 /Kids [3 0 R 4 0 R 5 0 R 6 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 430] /Annots [8 0 R 9 0 R 10 0 R 11 0 R 12 0 R 13 0 R 14 0 R 15 0 R 16 0 R 17 0 R 18 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 430] /Contents 7 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 430] /CropBox [20 20 280 410] /Rotate 90 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 430] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream),
	}
	for i, target := range []string{"[4 0 R /XYZ 80 300 2]", "[4 0 R /Fit]", "[4 0 R /FitH 280]", "[4 0 R /FitV 50]", "[4 0 R /FitR 50 80 200 280]", "[4 0 R /FitB]", "[4 0 R /FitBH 280]", "[4 0 R /FitBV 50]", "/named", "[4 0 R /FitR 100 100 100 100]", "[5 0 R /XYZ null 300 2]"} {
		y := 400 - i*20
		objects = append(objects, fmt.Sprintf("<< /Type /Annot /Subtype /Link /Rect [40 %d 80 %d] /Dest %s >>", y, y+10, target))
	}
	return encodePDF(objects)
}

func TestXYZLinkMakesTargetVisibleWhenOffsetIsOmitted(t *testing.T) {
	v, view, updates := newTestView(t, 500, 500)
	data := encodePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Count 4 /Kids [3 0 R 4 0 R 5 0 R 6 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 800] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 100] /Annots [7 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 800] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 800] >>",
		"<< /Type /Annot /Subtype /Link /Rect [200 30 300 70] /Dest [6 0 R /XYZ null null null] >>",
	})
	id := v.LoadBytes(t.Context(), data, OpenDocument)
	await(t, updates, view, func() bool { return v.document == id })
	v.scroll.Y = v.layout.page(1).Y + v.layout.page(1).H/2 - v.height/2
	view.Frame()
	await(t, updates, view, func() bool { return v.metadata[1].data != nil && v.cache[1].frame != nil })
	shown, ok := view.Find("Page 2")
	if !ok || v.State().Page != 2 {
		t.Fatalf("source page not current, page=%d shown=%v", v.State().Page, shown)
	}
	links := v.metadata[1].data.Links
	if len(links) != 1 {
		t.Fatalf("got %d links on the source page, want one", len(links))
	}
	link := links[0]
	view.ClickAt(shown.X+(link.Bounds.X+link.Bounds.W/2)*shown.W, shown.Y+(link.Bounds.Y+link.Bounds.H/2)*shown.H)
	first, last := v.visiblePages()
	if _, ok := view.Find("Page 4"); v.State().Page != 4 || !ok {
		t.Fatalf("destination selected page %d, visible pages %d..%d, want page 4 visible", v.State().Page, first+1, last+1)
	}
}
