package main

import (
	"context"
	_ "embed"
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/gvenusleo/mygo-pdf/pdf"
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
	openGeneration       uint64
	openName             string
	displayedName        string
	pageEntry, findQuery string
	pageField, findField ui.Handle
	showFind             bool
	showOutline          bool
	outlineID            uint64
	outline              pdf.OutlineResult
	outlineLoading       bool
	expanded             map[string]bool
	outlineList          ui.ListState
	outlineRowsCache     []outlineRow
}

func main() {
	d := &demo{showOutline: true}
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
	d.window = mygo.NewWindow(mygo.WindowOptions{
		Title: "MyGo PDF", Width: 1040, Height: 760, MinWidth: 720, MinHeight: 460,
		Content: ui.View(d.view),
	})
	d.viewer = pdf.NewViewer(d.window, pdf.Options{OnLink: func(url string) { _ = mygo.Shell.OpenExternal(url) }})
	d.window.OnClosed(d.cancel)
	d.viewer.OnChange(d.changed)
	d.viewer.SetFit(pdf.FitPage)
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
	c.OnShortcut(ui.Cmd, ui.KeyF, d.focusFind)
	theme := readerChrome(c.Theme())
	canvas := theme.Background.Mix(theme.Text, 0.055)
	c.SetTheme(&theme)
	ui.Column(c).Fill().Children(func() {
		if !d.preview {
			d.toolbar(c)
			if d.showFind {
				d.findBar(c)
			}
			ui.Divider(c)
		}
		d.loadStatus(c)
		ui.Row(c.Key("split")).Grow(1).FillWidth().MinHeight(0).Background(canvas).Children(func() {
			if !d.preview && d.showOutline {
				d.outlineView(c.Key("outline"))
				ui.Divider(c.Key("outline divider"))
			}
			ui.Column(c.Key("viewer column")).Grow(1).FillHeight().MinWidth(0).MinHeight(0).Children(func() {
				d.viewer.View(c.Key("pdf"), [3]bool{d.preview, d.showOutline, d.showFind})
			})
		})
	})
}

func (d *demo) documentName() string {
	if d.status.PageCount == 0 {
		return ""
	}
	if d.displayedName != "" {
		return d.displayedName
	}
	return "Sample.pdf"
}
