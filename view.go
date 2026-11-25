package pdf

import (
	"cmp"
	"fmt"

	"github.com/egoist/mygo/ui"
)

// View builds the scrollable viewer. Change layoutKey when the parent geometry changes.
// The key must be comparable.
func (v *Viewer) View(c *ui.Context, layoutKey any) {
	t := c.Theme()
	scroll := ui.ScrollBoth(c).Grow(1).FillWidth().MinWidth(0).MinHeight(0).
		TrackScroll(&v.scroll).Label(cmp.Or(v.options.Labels.Document, "PDF document"))
	bounds := v.scroll.Handle.Bounds(c)
	w, h := c.Size()
	layoutChanged := v.windowWidth != w || v.windowHeight != h || v.layoutKey != layoutKey
	v.windowWidth, v.windowHeight, v.layoutKey = w, h, layoutKey
	unmeasured := bounds.W <= 0 || bounds.H <= 0
	firstMeasurement := unmeasured && !v.waitingBounds
	v.waitingBounds = unmeasured
	if v.measure(bounds.W, bounds.H, t.Space(3)) || layoutChanged || firstMeasurement {
		// MyGo builds before layout. Measure again after the first frame or a size change.
		c.AnimationFrame()
	}
	if !layoutChanged && !unmeasured {
		v.resolveRefresh(true)
	}
	if v.selectedPage() == 0 {
		v.navigation = pageNavigation{}
	}
	if len(v.pages) == 0 {
		return
	}
	services := c.Services()
	doc := v.document
	scroll.Focusable().HandleInput(v.viewportInput(services)).ContextMenu(func(m *ui.Menu) {
		if v.document == doc {
			v.contextMenu(m, services)
		}
	})
	first, last := v.visiblePages()
	demand := v.requestVisible(first, last)
	v.notify()
	scroll.Children(func() {
		ui.Box(c).Size(v.layout.width, v.layout.height).Shrink(0).Children(func() {
			for i := first; i <= last; i++ {
				v.pageView(c, i, demand)
			}
		})
	})
}

func (v *Viewer) contextMenu(m *ui.Menu, services ui.Services) {
	labels := v.options.Labels
	if m.Item(cmp.Or(labels.Copy, "Copy")).Shortcut(ui.Cmd, ui.KeyC).Disabled(!v.hasSelection).Chosen() {
		v.copySelection(services)
	}
	m.Separator()
	if m.Item(cmp.Or(labels.ZoomIn, "Zoom in")).Disabled(v.zoom >= 4).Chosen() {
		v.zoomCentered(v.zoom * 1.25)
	}
	if m.Item(cmp.Or(labels.ZoomOut, "Zoom out")).Disabled(v.zoom <= 0.5).Chosen() {
		v.zoomCentered(v.zoom / 1.25)
	}
	if m.Item(cmp.Or(labels.FitWidth, "Fit width")).Disabled(v.zoom == 1 && v.fit == FitWidth).Chosen() {
		v.SetFit(FitWidth)
	}
	if m.Item(cmp.Or(labels.FitPage, "Fit page")).Disabled(v.zoom == 1 && v.fit == FitPage).Chosen() {
		v.SetFit(FitPage)
	}
	if openExternal := v.options.OpenExternal; openExternal != nil &&
		(v.options.CanOpenExternal == nil || v.options.CanOpenExternal()) {
		m.Separator()
		if m.Item(cmp.Or(labels.OpenExternal, "Open in default app")).Chosen() {
			openExternal()
		}
	}
}

func (v *Viewer) pageView(c *ui.Context, index int, demand pageRasterDemand) {
	t := c.Theme()
	r := v.layout.page(index)
	entry := v.cache[index]
	label := fmt.Sprintf(cmp.Or(v.options.Labels.Page, "Page %d"), index+1)
	// Changing the document key releases pointer capture from the previous page.
	key := struct {
		document uint64
		page     int
	}{v.document, index}
	page := ui.Box(c.Key(key)).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Clip().Label(label)
	frame := entry.frame
	if frame == nil {
		if err := v.pageRasterError(index, demand); err != nil {
			page.Background(t.Background)
			page.Center().Children(func() { ui.Text(c, err.Error()).TextColor(t.TextMuted) })
		}
	}
	data := v.metadata[index].data
	if data != nil && frame != nil {
		page.HandleInput(v.pageInput(v.document, index, data, r))
		if x, y, over := page.PointerPosition(); over {
			switch {
			case linkAt(data, x/r.W, y/r.H) >= 0:
				page.Cursor(ui.CursorPointer)
			case characterAt(data, x/r.W, y/r.H, false) >= 0:
				page.Cursor(ui.CursorText)
			}
		}
	}
	page.Children(func() {
		if frame != nil && !v.tilesCoverViewport(index, r) {
			ui.Image(c, frame.bitmap).Absolute().Size(r.W, r.H).AccessHidden(true)
		}
		v.tileViews(c, index, r)
		if data != nil && frame != nil {
			v.highlight(c, index, data, r)
		}
	})
}

func (v *Viewer) tilesCoverViewport(page int, bounds ui.Rect) bool {
	for _, key := range v.tileKeys {
		if key.Page != page {
			continue
		}
		visible := v.tileViewport(bounds, key.Size)
		if visible.Empty() {
			return false
		}
		missing := visible.Dx() * visible.Dy()
		for _, tile := range v.tileKeys {
			if tile.Page == page && v.tiles[tile].frame != nil {
				covered := tile.Tile.Intersect(visible)
				missing -= covered.Dx() * covered.Dy()
			}
		}
		return missing == 0
	}
	return false
}

func (v *Viewer) tileViews(c *ui.Context, page int, bounds ui.Rect) {
	for _, key := range v.tileKeys {
		if key.Page != page || v.tiles[key].frame == nil {
			continue
		}
		x, y := bounds.W/float32(key.Size.X), bounds.H/float32(key.Size.Y)
		left, top := float32(key.Tile.Min.X)*x, float32(key.Tile.Min.Y)*y
		width, height := float32(key.Tile.Max.X)*x-left, float32(key.Tile.Max.Y)*y-top
		ui.Image(c.Key(key.Tile), v.tiles[key].frame.bitmap).Absolute().Left(left).Top(top).
			Size(width, height).PassThrough().AccessHidden(true)
	}
}

func (v *Viewer) highlight(c *ui.Context, page int, data *geometry, bounds ui.Rect) {
	if !v.hasSelection || page < v.selection.First.Page || page > v.selection.Last.Page {
		return
	}
	start, end := 0, len(data.Chars)
	if page == v.selection.First.Page {
		start = min(v.selection.First.Index, end)
	}
	if page == v.selection.Last.Page {
		end = min(end, v.selection.Last.Index+1)
	}
	color := c.Theme().Selection
	var line, previous ui.Rect
	var wasVertical bool
	for i := min(start, end); i < end; i++ {
		rect, vertical := ui.Rect(data.Chars[i]), false
		if i < len(data.Selection) {
			box := data.Selection[i]
			rect, vertical = ui.Rect(box.Rect), box.Vertical
		}
		if rect.W <= 0 || rect.H <= 0 {
			continue
		}
		if line.W > 0 && vertical == wasVertical && sameSelectionLine(previous, rect, bounds, vertical) {
			x, y := min(line.X, rect.X), min(line.Y, rect.Y)
			line = ui.Rect{X: x, Y: y, W: max(line.X+line.W, rect.X+rect.W) - x, H: max(line.Y+line.H, rect.Y+rect.H) - y}
		} else {
			highlightRect(c, line, bounds, color)
			line = rect
		}
		previous, wasVertical = rect, vertical
	}
	highlightRect(c, line, bounds, color)
}

func sameSelectionLine(a, b, page ui.Rect, vertical bool) bool {
	inline, cross := page.W, page.H
	if vertical {
		a = ui.Rect{X: a.Y, Y: a.X, W: a.H, H: a.W}
		b = ui.Rect{X: b.Y, Y: b.X, W: b.H, H: b.W}
		inline, cross = cross, inline
	}
	// Bridge word spacing, but stop at a different line or a wide column gap.
	overlap := min(a.Y+a.H, b.Y+b.H) - max(a.Y, b.Y)
	gap := max(a.X-b.X-b.W, b.X-a.X-a.W)
	return overlap >= min(a.H, b.H)/2 && gap*inline <= max(a.H, b.H)*cross
}

func highlightRect(c *ui.Context, r, bounds ui.Rect, color ui.Color) {
	if r.W > 0 {
		ui.Box(c).Absolute().Left(r.X*bounds.W).Top(r.Y*bounds.H).Size(r.W*bounds.W, r.H*bounds.H).
			Background(color).PassThrough().AccessHidden(true)
	}
}
