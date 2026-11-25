package pdf

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/gvenusleo/mygo-pdf/internal/pdfium"
)

func TestRotatedSelectionUsesCanonicalTextAndLinks(t *testing.T) {
	v, view, updates := newTestView(t, 900, 700)
	var target string
	v.options.OnLink = func(url string) { target = url }
	v.LoadBytes(t.Context(), samplePDF(2), OpenDocument)
	await(t, updates, view, func() bool { return v.metadata[0].data != nil })
	v.SetFit(FitPage)
	v.SetRotation(1)
	v.GoToPage(1)
	view.Frame()
	await(t, updates, view, func() bool { return v.cache[0].frame != nil && v.cache[0].frame.key.Rotation == 1 })
	data := v.metadata[0].data
	visible, ok := view.Find("Page 1")
	if !ok || len(data.Chars) < 5 {
		t.Fatal("rotated page text is unavailable")
	}
	point := func(box pdfium.Rect) (float32, float32) {
		p := rotatedPoint(pdfium.Point{X: box.X + box.W/2, Y: box.Y + box.H/2}, 1)
		return visible.X + p.X*visible.W, visible.Y + p.Y*visible.H
	}
	x1, y1 := point(data.Chars[0])
	x2, y2 := point(data.Chars[4])
	view.Press(x1, y1)
	view.Move(x2, y2)
	view.Release(x2, y2)
	var copied string
	v.worker.submitCopy(copyJob{ctx: v.docContext, document: v.document, selected: v.selection, apply: func(s string) { copied = s }})
	await(t, updates, view, func() bool { return copied != "" })
	if copied != "Hello" {
		t.Fatalf("rotated selection copied %q", copied)
	}
	for _, link := range data.Links {
		if link.URL != "" {
			view.ClickAt(point(link.Bounds))
			break
		}
	}
	if target != "https://example.com/" {
		t.Fatalf("rotated link did not route through the host: %q", target)
	}
	if _, ok := view.AccessByRole(ui.RoleLink, target); !ok {
		t.Fatal("rotated link is absent from the accessibility tree")
	}
}
