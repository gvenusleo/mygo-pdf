package pdf

import (
	"fmt"
	"testing"
)

func TestTextSelectionHighlightShape(t *testing.T) {
	for _, rotation := range []int{0, 90} {
		t.Run(fmt.Sprint(rotation), func(t *testing.T) {
			v, view, updates := newTestView(t, 400, 600)
			const stream = "BT /F1 40 Tf 55 320 Td (The) Tj 95 0 Td (next) Tj -95 -65 Td (The next) Tj ET\n"
			data := encodePDF([]string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
				fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 430] /Rotate %d /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", rotation),
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream),
			})
			v.LoadBytes(t.Context(), data, OpenDocument)
			await(t, updates, view, func() bool { return v.metadata[0].data != nil })
			page, ok := view.Find("Page 1")
			if !ok {
				t.Fatal("the page is not visible")
			}
			bounds := v.layout.page(0)
			point := func(x, y float32) (float32, float32) {
				if rotation == 90 {
					return page.X + y/430*bounds.W, page.Y + x/300*bounds.H
				}
				return page.X + x/300*bounds.W, page.Y + (430-y)/430*bounds.H
			}
			drag := func(x1, y1, x2, y2 float32) {
				view.Press(point(x1, y1))
				view.Move(point(x2, y2))
				view.Release(point(x2, y2))
			}
			before := view.Image()
			check := func(x, y float32, selected bool) {
				t.Helper()
				px, py := point(x, y)
				if changed := view.Image().RGBAAt(int(px), int(py)) != before.RGBAAt(int(px), int(py)); changed != selected {
					t.Errorf("highlight at PDF position (%v, %v) = %t, want %t", x, y, changed, selected)
				}
			}
			// Begin at the e in The to check that the highlight honors a partial word.
			drag(112, 330, 220, 330)
			check(62, 330, false)
			check(135, 330, true)  // Space between the words.
			check(163, 345, true)  // Font ascent above the short lowercase glyph.
			check(163, 314, true)  // Font descent below the baseline.
			check(135, 266, false) // The next line is still outside the selection.
			drag(66, 330, 204, 265)
			check(135, 330, true)
			check(135, 297, false) // Line spacing remains clear.
			check(135, 266, true)
		})
	}
}
