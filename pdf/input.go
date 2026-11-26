package pdf

import (
	"math"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/gvenusleo/mygo-pdf/internal/pdfium"
)

type pointerDrag struct {
	down  bool
	moved bool
	x, y  float32
	start textPoint
	link  int
}

func (v *Viewer) zoomCentered(zoom float32) { v.setZoom(zoom, v.width/2, v.height/2) }

func (v *Viewer) viewportInput(services ui.Services) func(ui.InputEvent) bool {
	return func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputZoom:
			return v.pinch(ev, ev.X, ev.Y)
		case ui.InputScroll:
			if v.pendingNavigation != nil && (ev.DX != 0 || ev.DY != 0) {
				v.pendingNavigation = nil
			}
			if ev.Mods&(ui.Cmd|ui.Ctrl) != 0 && ev.DY != 0 {
				v.setZoom(v.zoom*float32(math.Exp(-float64(ev.DY)/160)), ev.X, ev.Y)
				return true
			}
		}
		return v.keyInput(ev, services)
	}
}

func (v *Viewer) pinch(ev ui.InputEvent, x, y float32) bool {
	if ev.Scale <= 0 {
		return false
	}
	v.setZoom(v.zoom*ev.Scale, x, y)
	return true
}

func (v *Viewer) keyInput(ev ui.InputEvent, services ui.Services) bool {
	if ev.Kind == ui.InputCommand && ev.Text == "copy" || ev.Kind == ui.InputKeyDown && ev.Mods&(ui.Cmd|ui.Ctrl) != 0 && ev.Mods&^(ui.Cmd|ui.Ctrl) == 0 && ev.Key == ui.KeyC {
		v.copySelection(services)
		return true
	}
	if ev.Kind != ui.InputKeyDown || ev.Mods&^(ui.Cmd|ui.Ctrl|ui.Shift) != 0 {
		return false
	}
	switch ev.Key {
	case ui.KeyMinus:
		v.zoomCentered(v.zoom / 1.25)
	case ui.KeyEqual:
		v.zoomCentered(v.zoom * 1.25)
	case ui.Key0:
		v.zoomCentered(1)
	default:
		return false
	}
	return true
}

func (v *Viewer) copySelection(services ui.Services) {
	if v.hasSelection {
		v.worker.submitCopy(copyJob{ctx: v.docContext, document: v.document, selected: v.selection, apply: services.WriteClipboard})
	}
}

func (v *Viewer) pageInput(document uint64, page int, data *geometry, bounds ui.Rect) func(ui.InputEvent) bool {
	return func(ev ui.InputEvent) bool {
		if document != v.document {
			return false
		}
		switch ev.Kind {
		case ui.InputZoom:
			return v.pinch(ev, bounds.X+ev.X-v.scroll.X, bounds.Y+ev.Y-v.scroll.Y)
		case ui.InputPointerDown:
			if ev.Button != 0 {
				return false
			}
			v.hasSelection = false
			point := rotatedPoint(pdfium.Point{X: ev.X / bounds.W, Y: ev.Y / bounds.H}, -v.rotation)
			index := characterAt(data, point.X, point.Y, false)
			v.pointer = pointerDrag{down: true, x: ev.X, y: ev.Y, start: textPoint{Page: page, Index: index}, link: linkAt(data, point.X, point.Y)}
			return true
		case ui.InputPointerMove:
			if v.pointer.down && ev.Button == 0 {
				v.extendSelection(ev, bounds)
				return true
			}
		case ui.InputPointerUp:
			if !v.pointer.down || ev.Button != 0 {
				return false
			}
			if !ev.Canceled {
				v.extendSelection(ev, bounds)
				point := rotatedPoint(pdfium.Point{X: ev.X / bounds.W, Y: ev.Y / bounds.H}, -v.rotation)
				if link := v.pointer.link; !v.pointer.moved && link >= 0 && link == linkAt(data, point.X, point.Y) {
					v.openLink(data.Links[link])
				}
			}
			v.pointer = pointerDrag{}
			return true
		}
		return false
	}
}

func (v *Viewer) extendSelection(ev ui.InputEvent, bounds ui.Rect) {
	drag := &v.pointer
	if !drag.moved && math.Hypot(float64(ev.X-drag.x), float64(ev.Y-drag.y)) < 3 {
		return
	}
	drag.moved = true
	if drag.start.Index < 0 {
		return
	}
	x, y := bounds.X+ev.X, bounds.Y+ev.Y
	page := v.layout.FirstPage(y)
	data := v.metadata[page].data
	if data == nil {
		return
	}
	r := v.layout.page(page)
	point := rotatedPoint(pdfium.Point{X: (x - r.X) / r.W, Y: (y - r.Y) / r.H}, -v.rotation)
	if index := characterAt(data, point.X, point.Y, true); index >= 0 {
		first, last := drag.start, textPoint{Page: page, Index: index}
		if first.Page > last.Page || first.Page == last.Page && first.Index > last.Index {
			first, last = last, first
		}
		v.selection, v.hasSelection = textRange{First: first, Last: last}, true
	}
}

func (v *Viewer) openLink(link pageLink) {
	if link.URL != "" && v.options.OnLink != nil {
		v.options.OnLink(link.URL)
		return
	}
	if link.Page >= 0 && link.Page < len(v.pages) {
		v.navigateToWhenSized(link.Page, func() { v.goToDestination(link.Page, link.Target) })
	}
}

func (v *Viewer) goToDestination(index int, dest pdfium.Destination) {
	if dest.Mode == 0 {
		v.GoToPage(index + 1)
		return
	}
	dest = rotatedDestination(dest, v.rotation)
	position := dest.Position
	if dest.Mode == pdfium.DestinationXYZ && (!dest.HasX || !dest.HasY) {
		current := v.layout.page(v.currentPage())
		if !dest.HasX {
			position.X = max(0, min(1, (v.scroll.X-current.X)/current.W))
		}
		if !dest.HasY {
			position.Y = max(0, min(1, (v.scroll.Y-current.Y)/current.H))
		}
	}
	vp := v.viewport()
	vp.Zoom = 1
	baseScale := fitLayout(v.layout.Geometry, vp).Scale
	size := v.layout.Size(index)
	area := dest.Area
	width := float64(size.Width) * float64(area.W)
	height := float64(size.Height) * float64(area.H)
	availableWidth := float64(max(1, v.width-2*v.gap))
	availableHeight := float64(max(1, v.height-2*v.gap))
	scale := v.layout.Scale
	switch dest.Mode {
	case pdfium.DestinationXYZ:
		if dest.Scale > 0 {
			scale = float64(dest.Scale)
		}
	case pdfium.DestinationFit, pdfium.DestinationFitB, pdfium.DestinationFitR:
		scale = min(availableWidth/width, availableHeight/height)
	case pdfium.DestinationFitH, pdfium.DestinationFitBH:
		scale = availableWidth / width
	case pdfium.DestinationFitV, pdfium.DestinationFitBV:
		scale = availableHeight / height
	}
	if dest.Mode != pdfium.DestinationXYZ {
		if !dest.HasX {
			position.X = area.X
		}
		if !dest.HasY {
			position.Y = area.Y
		}
	}
	v.zoom = max(0.5, min(4, float32(scale/baseScale)))
	vp.Zoom = v.zoom
	v.layout = fitLayout(v.layout.Geometry, vp)
	page := v.layout.page(index)
	x := page.X + position.X*page.W
	y := page.Y + position.Y*page.H
	if dest.Mode == pdfium.DestinationFit || dest.Mode == pdfium.DestinationFitB || dest.Mode == pdfium.DestinationFitR {
		x -= max(0, v.width-area.W*page.W) / 2
		y -= max(0, v.height-area.H*page.H) / 2
	}
	// An edge coordinate can put the whole target outside the viewport.
	if x >= page.X+page.W || x+v.width <= page.X {
		x = page.X
	}
	if y >= page.Y+page.H || y+v.height <= page.Y {
		y = page.Y
	}
	v.scroll.X = max(0, min(x, v.layout.Width-v.width))
	v.navigateTo(index+1, y)
}

func linkAt(data *geometry, x, y float32) int {
	return slices.IndexFunc(data.Links, func(link pageLink) bool { return link.Bounds.Contains(x, y) })
}

func characterAt(data *geometry, x, y float32, nearest bool) int {
	index, distance := -1, float32(math.MaxFloat32)
	for i, box := range data.Chars {
		if box.W == 0 || box.H == 0 {
			continue
		}
		if box.Contains(x, y) {
			return i
		}
		if nearest {
			dx := max(box.X-x, x-box.X-box.W, 0)
			dy := max(box.Y-y, y-box.Y-box.H, 0)
			if d := dx*dx + dy*dy; d < distance {
				index, distance = i, d
			}
		}
	}
	return index
}
