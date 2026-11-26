package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
	"github.com/gvenusleo/mygo-pdf/pdf"
)

func (d *demo) openPath(path, password string) {
	if d.openCancel != nil {
		d.openCancel()
	}
	ctx, cancel := context.WithCancel(d.ctx)
	d.openCancel = cancel
	d.path, d.password, d.readErr = path, "", nil
	d.readID++
	id, window := d.readID, d.window
	go func() {
		file, err := os.Open(path)
		var info os.FileInfo
		if err == nil {
			defer file.Close()
			info, err = file.Stat()
		}
		if err != nil {
			window.Update(func() {
				if ctx.Err() == nil && d.readID == id {
					d.readErr = err
				}
			})
			return
		}
		// A lock-free handoff lets cancellation close an unclaimed file even if
		// the window shuts down before it can run the queued update.
		lease := make(chan (<-chan struct{}), 1)
		var claimed atomic.Bool
		window.Update(func() {
			if ctx.Err() != nil || d.readID != id || claimed.Swap(true) {
				return
			}
			generation, released := d.viewer.LoadReaderAt(ctx, file, info.Size(), pdf.LoadOptions{Password: password})
			d.openGeneration, d.openName = generation, filepath.Base(path)
			lease <- released
		})
		select {
		case released := <-lease:
			<-released
		case <-ctx.Done():
			// After the UI claims the file, wait for PDFium before closing it.
			if claimed.Swap(true) {
				released := <-lease
				<-released
			}
		}
	}()
}

func (d *demo) loadStatus(c *ui.Context) {
	if d.readErr != nil {
		ui.Text(c.Key("open error"), d.readErr.Error()).Padding(6, chromeInset).TextColor(c.Theme().Danger).Wrap()
		return
	}
	if d.path != "" && !d.status.Loading && errors.Is(d.status.Err, pdf.ErrPassword) {
		ui.Row(c.Key("password")).AlignItems(ui.Center).Gap(6).Padding(6, chromeInset).Children(func() {
			retry := func() { d.openPath(d.path, d.password) }
			ui.TextInput(c.Key("password field"), &d.password).Password().Label("PDF password").Placeholder("PDF password").Height(controlSize).OnSubmit(retry)
			ui.PrimaryButton(c.Key("unlock"), "Unlock").Height(controlSize).PaddingX(10).OnClick(retry)
			ui.Button(c.Key("cancel"), "Cancel").Height(controlSize).PaddingX(10).OnClick(func() {
				if d.openCancel != nil {
					d.openCancel()
					d.openCancel = nil
				}
				d.path, d.password = "", ""
				d.viewer.Clear()
			})
		})
	} else if d.status.Loading {
		ui.Text(c.Key("opening"), "Opening PDF…").Padding(6, chromeInset).TextColor(c.Theme().TextMuted)
	} else if d.status.Err != nil {
		ui.Text(c.Key("load error"), d.status.Err.Error()).Padding(6, chromeInset).TextColor(c.Theme().Danger).Wrap()
	}
}
