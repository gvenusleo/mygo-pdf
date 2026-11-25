package pdf

import (
	"cmp"
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/gvenusleo/mygo-pdf/internal/pdfium"
)

type semanticLinkKey struct {
	document    uint64
	page, index int
}

func (v *Viewer) linkBounds(page ui.Rect, link pageLink) (ui.Rect, bool) {
	r := link.Bounds
	if v.rotation != 0 {
		r = rotatedRect(r, v.rotation)
	}
	r.X, r.Y, r.W, r.H = r.X*page.W, r.Y*page.H, r.W*page.W, r.H*page.H
	x, y := r.X+page.X, r.Y+page.Y
	return ui.Rect(r), r.W > 0 && r.H > 0 && x < v.scroll.X+v.width && x+r.W > v.scroll.X &&
		y < v.scroll.Y+v.height && y+r.H > v.scroll.Y
}

func (v *Viewer) hasSemanticLink(key semanticLinkKey, first, last int) bool {
	if key.document != v.document || key.page < first || key.page > last || v.cache[key.page].frame == nil {
		return false
	}
	data := v.metadata[key.page].data
	if data == nil || key.index >= len(data.Links) {
		return false
	}
	_, visible := v.linkBounds(v.layout.page(key.page), data.Links[key.index])
	return visible
}

func (v *Viewer) pageSemantics(c *ui.Context, page int, bounds ui.Rect, data *geometry) {
	if data.Text != "" {
		ui.Box(c.Key("text")).Absolute().Size(bounds.W, bounds.H).PassThrough().Role(ui.RoleText).Label(data.Text)
	}
	for i, link := range data.Links {
		if link.URL != "" && v.options.OnLink == nil || link.URL == "" && link.Page < 0 {
			continue
		}
		r, visible := v.linkBounds(bounds, link)
		if !visible {
			continue
		}
		label := cmp.Or(link.Label, link.URL)
		if label == "" {
			label = fmt.Sprintf(cmp.Or(v.options.Labels.Page, "Page %d"), link.Page+1)
		}
		key := semanticLinkKey{v.document, page, i}
		element := ui.Box(c.Key(key)).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).
			Role(ui.RoleLink).Label(label).Focusable().FocusRing(true).PassThrough().OnClick(func() {
			if key.document == v.document {
				v.openLink(link)
			}
		})
		if v.focusedLink.document == 0 && element.Focused() {
			v.focusedLink = key
		}
	}
}

func (v *Viewer) findHighlight(c *ui.Context, page int, bounds ui.Rect) {
	if match := v.findMatch; match != nil && match.Page == page {
		for _, box := range match.Boxes {
			v.paintHighlight(c, box, bounds, c.Theme().Accent.Alpha(0.35))
		}
	}
}

func (v *Viewer) paintHighlight(c *ui.Context, box pdfium.Rect, bounds ui.Rect, color ui.Color) {
	highlightRect(c, ui.Rect(rotatedRect(box, v.rotation)), bounds, color)
}
