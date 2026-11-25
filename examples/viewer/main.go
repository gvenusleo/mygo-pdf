package main

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	pdf "github.com/gvenusleo/mygo-pdf"
)

//go:embed sample.pdf
var sample []byte

type demo struct {
	window  *mygo.Window
	viewer  *pdf.Viewer
	ctx     context.Context
	cancel  context.CancelFunc
	readID  uint64
	status  pdf.State
	readErr error
	preview bool
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
	d.viewer.OnChange(func(s pdf.State) { d.status = s })
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
	d.readID++
	id, ctx, window := d.readID, d.ctx, d.window
	go func(path string) {
		data, err := readPDF(path)
		window.Update(func() {
			if ctx.Err() != nil || d.readID != id {
				return
			}
			d.readErr = err
			if err == nil {
				d.viewer.LoadBytes(ctx, data, pdf.OpenDocument)
			}
		})
	}(paths[0])
}

func readPDF(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, pdf.MaxPDFBytes+1))
	if len(data) > pdf.MaxPDFBytes {
		return nil, pdf.ErrLimit
	}
	return data, err
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
		}
		if d.readErr != nil {
			ui.Text(c, d.readErr.Error()).TextColor(c.Theme().Danger).Wrap()
		} else if d.status.Err != nil {
			ui.Text(c, d.status.Err.Error()).TextColor(c.Theme().Danger).Wrap()
		}
		d.viewer.View(c.Key("pdf"), d.preview)
	})
}

func (d *demo) toolbar(c *ui.Context) {
	s := d.status
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Padding(t.Space(2)).Children(func() {
		ui.Button(c, "Open PDF").OnClick(d.open)
		ui.Button(c, "Previous").Disabled(s.Page <= 1).OnClick(func() { d.viewer.GoToPage(s.Page - 1) })
		ui.Text(c, fmt.Sprintf("%d / %d", s.Page, s.PageCount))
		ui.Button(c, "Next").Disabled(s.Page >= s.PageCount).OnClick(func() { d.viewer.GoToPage(s.Page + 1) })
		ui.Button(c, "−").Label("Zoom out").Disabled(s.Page == 0 || s.Zoom <= 0.5).OnClick(func() { d.viewer.SetZoom(s.Zoom / 1.25) })
		ui.Button(c, "+").Label("Zoom in").Disabled(s.Page == 0 || s.Zoom >= 4).OnClick(func() { d.viewer.SetZoom(s.Zoom * 1.25) })
		ui.Button(c, "Fit width").OnClick(func() { d.viewer.SetFit(pdf.FitWidth) })
		ui.Button(c, "Fit page").OnClick(func() { d.viewer.SetFit(pdf.FitPage) })
		ui.Button(c, "Hide toolbar").OnClick(func() { d.preview = true })
	})
}
