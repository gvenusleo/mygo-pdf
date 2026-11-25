package main

import (
	"context"
	"errors"
	"os"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
	pdf "github.com/gvenusleo/mygo-pdf"
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
			_, released := d.viewer.LoadReaderAt(ctx, file, info.Size(), pdf.LoadOptions{Password: password})
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
		ui.Text(c, d.readErr.Error()).TextColor(c.Theme().Danger).Wrap()
		return
	}
	if d.path != "" && !d.preview && !d.status.Loading && errors.Is(d.status.Err, pdf.ErrPassword) {
		ui.Row(c.Key("password")).Gap(c.Theme().Space(2)).Padding(c.Theme().Space(2)).Children(func() {
			retry := func() { d.openPath(d.path, d.password) }
			ui.TextInput(c, &d.password).Password().Label("PDF password").Placeholder("PDF password").OnSubmit(retry)
			ui.Button(c, "Unlock").OnClick(retry)
			ui.Button(c, "Cancel").OnClick(func() {
				if d.openCancel != nil {
					d.openCancel()
					d.openCancel = nil
				}
				d.path, d.password = "", ""
				d.viewer.Clear()
			})
		})
	} else if d.status.Loading {
		ui.Text(c, "Opening PDF…")
	} else if d.status.Err != nil {
		ui.Text(c, d.status.Err.Error()).TextColor(c.Theme().Danger).Wrap()
	}
}
