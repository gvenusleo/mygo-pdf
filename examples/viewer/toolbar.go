package main

import (
	"math"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/gvenusleo/mygo-pdf/pdf"
)

const (
	controlSize = 30
	chromeInset = 12
)

func (d *demo) toolbar(c *ui.Context) {
	s := d.status
	ui.Toolbar(c.Key("viewer toolbar"), func() {
		ui.Toggle(c.Key("outline toggle"), &d.showOutline, "").Label("Outline").Tooltip("Outline").
			Size(controlSize, controlSize).Padding(0).
			Disabled(s.PageCount == 0 && !d.showOutline).
			Children(func() { toolbarIcon(c, outlineIcon) }).OnChange(d.requestOutlineIfVisible)
		ui.Row(c.Key("document name")).Width(0).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(6).PaddingX(2).Children(func() {
			if name := d.documentName(); name != "" {
				toolbarIcon(c, documentIcon)
				ui.Text(c, name).Shrink(1).MinWidth(0).TruncateMiddle().SingleLine().FontWeight(600).Tooltip(name)
			}
		})
		d.pageControls(c.Key("page controls"))
		d.zoomControls(c.Key("zoom controls"))
		ui.Toggle(c.Key("find toggle"), &d.showFind, "").Label("Find in document").Tooltip("Find in document").
			Size(controlSize, controlSize).Padding(0).
			Disabled(s.PageCount == 0 && !d.showFind).
			Children(func() { toolbarIcon(c, searchIcon) }).OnChange(d.findVisibilityChanged)
		ui.MenuButton(c.Key("view options"), "View", d.viewMenu).Height(controlSize).Disabled(s.PageCount == 0)
		ui.PrimaryButton(c.Key("open"), "Open PDF").Height(controlSize).PaddingX(10).OnClick(d.open)
	}).Label("PDF controls").FillWidth().Gap(6).Padding(6, chromeInset)
}

func (d *demo) pageControls(c *ui.Context) {
	s := d.status
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Radius(t.Radius).Border(1, t.Border).Children(func() {
		toolbarButton(c.Key("previous page"), "Previous page", leftIcon).Height(controlSize - 2).
			Disabled(s.Page <= 1).OnClick(func() { d.viewer.GoToPage(s.Page - 1) })
		if !d.pageField.Focused(c) {
			d.pageEntry = pageLabel(s.Page)
		}
		ui.TextInput(c.Key("page number"), &d.pageEntry).Label("Page number").Size(48, controlSize-2).
			PaddingX(2).BorderWidth(0).Background(ui.Transparent).FocusRing(true).FocusRingOffset(-2).
			TextAlign(ui.Center).Bind(&d.pageField).Disabled(s.Page == 0).OnSubmit(func() {
			page, err := strconv.Atoi(d.pageEntry)
			if err != nil || !d.viewer.GoToPage(page) {
				d.pageEntry = pageLabel(s.Page)
			}
		})
		ui.Textf(c, "/ %d", s.PageCount).TextColor(t.TextMuted).SingleLine().PaddingEnd(4)
		toolbarButton(c.Key("next page"), "Next page", rightIcon).Height(controlSize - 2).
			Disabled(s.Page >= s.PageCount).OnClick(func() { d.viewer.GoToPage(s.Page + 1) })
	})
}

func (d *demo) zoomControls(c *ui.Context) {
	s := d.status
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Radius(t.Radius).Border(1, t.Border).Children(func() {
		toolbarButton(c.Key("zoom out"), "Zoom out", minusIcon).Height(controlSize - 2).
			Disabled(s.Page == 0 || s.Zoom <= 0.5).OnClick(func() { d.viewer.SetZoom(s.Zoom / 1.25) })
		ui.Textf(c, "%d%%", int(math.Round(float64(s.Zoom)*100))).Width(39).
			TextAlign(ui.Center).SingleLine()
		toolbarButton(c.Key("zoom in"), "Zoom in", plusIcon).Height(controlSize - 2).
			Disabled(s.Page == 0 || s.Zoom >= 4).OnClick(func() { d.viewer.SetZoom(s.Zoom * 1.25) })
	})
}

func (d *demo) viewMenu(menu *ui.Menu) {
	if menu.Item("Fit width").Radio(d.status.Fit == pdf.FitWidth).Chosen() {
		d.viewer.SetFit(pdf.FitWidth)
	}
	if menu.Item("Fit page").Radio(d.status.Fit == pdf.FitPage).Chosen() {
		d.viewer.SetFit(pdf.FitPage)
	}
	menu.Separator()
	if menu.Item("Rotate clockwise").Chosen() {
		d.viewer.SetRotation(d.status.Rotation + 1)
	}
	if menu.Item("Hide toolbar").Shortcut(ui.Cmd, ui.KeyP).Chosen() {
		d.preview = true
	}
}

func toolbarButton(c *ui.Context, label string, draw iconPainter) ui.Element {
	return ui.Button(c, "").Label(label).Tooltip(label).Size(controlSize, controlSize).
		Padding(0).Children(func() { toolbarIcon(c, draw) })
}

// Icon paths use an 18 DIP grid to keep their stroke and alignment consistent.
type iconPainter func(*ui.Painter, ui.Rect, ui.Color)

func toolbarIcon(c *ui.Context, draw iconPainter) {
	color := c.Theme().Text
	ui.Box(c).Size(18, 18).PassThrough().AccessHidden(true).Draw(func(p *ui.Painter, rect ui.Rect) {
		draw(p, rect, color)
	})
}

func leftIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	var path ui.Path
	path.MoveTo(r.X+11, r.Y+4).LineTo(r.X+6, r.Y+9).LineTo(r.X+11, r.Y+14)
	p.StrokePath(&path, 1.5, color)
}

func rightIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	var path ui.Path
	path.MoveTo(r.X+7, r.Y+4).LineTo(r.X+12, r.Y+9).LineTo(r.X+7, r.Y+14)
	p.StrokePath(&path, 1.5, color)
}

func downIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	var path ui.Path
	path.MoveTo(r.X+4, r.Y+7).LineTo(r.X+9, r.Y+12).LineTo(r.X+14, r.Y+7)
	p.StrokePath(&path, 1.5, color)
}

func minusIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	p.Line(r.X+4, r.Y+9, r.X+14, r.Y+9, 1.5, color)
}

func plusIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	minusIcon(p, r, color)
	p.Line(r.X+9, r.Y+4, r.X+9, r.Y+14, 1.5, color)
}

func searchIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	p.Stroke(ui.Rect{X: r.X + 3, Y: r.Y + 3, W: 9, H: 9}, color, 4.5, 1.5)
	p.StrokePolyline([][2]float32{{r.X + 10.8, r.Y + 10.8}, {r.X + 15, r.Y + 15}}, 1.5, color)
}

func outlineIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	p.Stroke(ui.Rect{X: r.X + 2, Y: r.Y + 3, W: 14, H: 12}, color, 2, 1.5)
	p.Line(r.X+7, r.Y+3, r.X+7, r.Y+15, 1.5, color)
}

func documentIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	var path ui.Path
	path.MoveTo(r.X+5, r.Y+2.5).LineTo(r.X+10.5, r.Y+2.5).LineTo(r.X+14, r.Y+6).
		LineTo(r.X+14, r.Y+15.5).LineTo(r.X+4, r.Y+15.5).LineTo(r.X+4, r.Y+2.5).Close()
	path.MoveTo(r.X+10, r.Y+3).LineTo(r.X+10, r.Y+6.5).LineTo(r.X+13.5, r.Y+6.5)
	p.StrokePath(&path, 1.4, color)
}

func closeIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	var path ui.Path
	path.MoveTo(r.X+5, r.Y+5).LineTo(r.X+13, r.Y+13)
	path.MoveTo(r.X+13, r.Y+5).LineTo(r.X+5, r.Y+13)
	p.StrokePath(&path, 1.5, color)
}
