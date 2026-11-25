package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"strconv"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	pdf "github.com/gvenusleo/mygo-pdf"
)

//go:embed sample.pdf
var sample []byte

type demo struct {
	window               *mygo.Window
	viewer               *pdf.Viewer
	ctx                  context.Context
	cancel               context.CancelFunc
	readID               uint64
	status               pdf.State
	readErr              error
	preview              bool
	path                 string
	password             string
	openCancel           context.CancelFunc
	pageEntry, findQuery string
	pageField            ui.Handle
	showOutline          bool
	outlineID            uint64
	outline              pdf.OutlineResult
	outlineLoading       bool
	expanded             map[string]bool
	outlineList          ui.ListState
	outlineRowsCache     []outlineRow
}

func main() {
	d := new(demo)
	d.ctx, d.cancel = context.WithCancel(context.Background())
	mygo.App.WhenReady(d.start)
	err := mygo.App.Run()
	if d.viewer != nil {
		d.viewer.Close()
		<-d.viewer.Done()
	}
	if err != nil {
		log.Fatal(err)
	}
}

func (d *demo) start() {
	d.window = mygo.NewWindow(mygo.WindowOptions{Title: "MyGo PDF", Width: 960, Height: 760, Content: ui.View(d.view)})
	d.viewer = pdf.NewViewer(d.window, pdf.Options{OnLink: func(url string) { _ = mygo.Shell.OpenExternal(url) }})
	d.window.OnClosed(d.cancel)
	d.viewer.OnChange(d.changed)
	d.viewer.LoadBytes(d.ctx, sample, pdf.OpenDocument)
}

func (d *demo) open() {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: d.window, Title: "Open PDF", Filters: []mygo.FileFilter{{Name: "PDF", Extensions: []string{"pdf"}}},
	})
	if err != nil {
		d.readErr = err
		return
	}
	if len(paths) == 0 {
		return
	}
	d.openPath(paths[0], "")
}

func (d *demo) view(c *ui.Context) {
	if d.viewer == nil {
		return
	}
	c.OnShortcut(ui.Cmd, ui.KeyP, func() { d.preview = !d.preview })
	c.OnShortcut(ui.Cmd, ui.KeyO, d.open)
	ui.Column(c).Fill().Children(func() {
		if !d.preview {
			d.toolbar(c)
			d.readingTools(c)
		}
		d.loadStatus(c)
		ui.Row(c.Key("split")).Grow(1).FillWidth().MinHeight(0).Children(func() {
			if !d.preview && d.showOutline {
				d.outlineView(c.Key("outline"))
			}
			ui.Column(c.Key("viewer column")).Grow(1).FillHeight().MinWidth(0).MinHeight(0).Children(func() {
				d.viewer.View(c.Key("pdf"), [2]bool{d.preview, d.showOutline})
			})
		})
	})
}

func (d *demo) toolbar(c *ui.Context) {
	s := d.status
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(2)).Children(func() {
		ui.Button(c, "Open PDF").OnClick(d.open)
		ui.Button(c, "Previous").Disabled(s.Page <= 1).OnClick(func() { d.viewer.GoToPage(s.Page - 1) })
		if !d.pageField.Focused(c) {
			d.pageEntry = pageLabel(s.Page)
		}
		entry := ui.TextInput(c.Key("page"), &d.pageEntry).Label("Page number").Width(72).Bind(&d.pageField).Disabled(s.Page == 0)
		entry.OnSubmit(func() {
			page, err := strconv.Atoi(d.pageEntry)
			if err != nil || !d.viewer.GoToPage(page) {
				d.pageEntry = pageLabel(s.Page)
			}
		})
		ui.Text(c, fmt.Sprintf("/ %d", s.PageCount))
		ui.Button(c, "Next").Disabled(s.Page >= s.PageCount).OnClick(func() { d.viewer.GoToPage(s.Page + 1) })
		ui.Button(c, "−").Label("Zoom out").Disabled(s.Page == 0 || s.Zoom <= 0.5).OnClick(func() { d.viewer.SetZoom(s.Zoom / 1.25) })
		ui.Button(c, "+").Label("Zoom in").Disabled(s.Page == 0 || s.Zoom >= 4).OnClick(func() { d.viewer.SetZoom(s.Zoom * 1.25) })
		ui.Button(c, "Fit width").OnClick(func() { d.viewer.SetFit(pdf.FitWidth) })
		ui.Button(c, "Fit page").OnClick(func() { d.viewer.SetFit(pdf.FitPage) })
		ui.Button(c, "Rotate 90°").Disabled(s.Page == 0).OnClick(func() { d.viewer.SetRotation(s.Rotation + 1) })
		ui.Button(c, "Hide toolbar").OnClick(func() { d.preview = true })
	})
}
