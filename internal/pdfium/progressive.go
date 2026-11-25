package pdfium

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// The gate serializes native calls and gives visible work priority over prefetch.
type nativeGate struct {
	sync.Cond
	busy          bool
	next, serving uint64
}

func (g *nativeGate) Lock() { g.lock(context.Background(), false) }

func (g *nativeGate) lock(ctx context.Context, background bool) {
	g.L.Lock()
	turn := g.next
	if !background {
		g.next++
	} else if g.busy || g.serving != g.next {
		stop := context.AfterFunc(ctx, func() {
			g.L.Lock()
			g.Broadcast()
			g.L.Unlock()
		})
		defer stop()
	}
	for {
		if background && ctx.Err() != nil {
			// A canceled raster still needs the gate to release native handles.
			// Give cleanup a FIFO position so later visible work cannot starve it.
			background, turn = false, g.next
			g.next++
		}
		if !g.busy && (background && g.serving == g.next || !background && turn == g.serving) {
			break
		}
		g.Wait()
	}
	if !background {
		g.serving++
	}
	g.busy = true
	g.L.Unlock()
}

func (g *nativeGate) Unlock() {
	g.L.Lock()
	g.busy = false
	g.Broadcast()
	g.L.Unlock()
}

// Every raster is at most 16 MiB. Reserve one of four slots for visible work.
var renderSlots = make(chan struct{}, 4)
var prefetchSlots = make(chan struct{}, 3)

func acquireRenderSlot(ctx context.Context, background bool) bool {
	if background {
		select {
		case prefetchSlots <- struct{}{}:
		case <-ctx.Done():
			return false
		}
	}
	select {
	case renderSlots <- struct{}{}:
		return true
	case <-ctx.Done():
		if background {
			<-prefetchSlots
		}
		return false
	}
}

func releaseRenderSlot(background bool) {
	<-renderSlots
	if background {
		<-prefetchSlots
	}
}

// Match PDFium's IFSDK_PAUSE layout on the supported 64-bit platforms.
type renderPause struct {
	version int32
	pause   uintptr
	user    uintptr
}

var pauseCallback = sync.OnceValue(func() uintptr {
	return newPauseCallback(func(uintptr) uintptr {
		if activeRenderContext.Err() != nil || !time.Now().Before(renderDeadline) {
			return 1
		}
		return 0
	})
})

// The active call owns these values under the native gate. PDFium invokes the
// callback synchronously, and the trampoline is reused for every render slice.
var activeRenderContext context.Context
var renderDeadline time.Time

func (e *Engine) drawProgressive(ctx context.Context, bitmap, page uintptr, x, y, width, height, rotation, flags int32, background bool) error {
	const (
		continued = 1
		done      = 2
		slice     = 8 * time.Millisecond
	)
	if err := ctx.Err(); err != nil {
		return err
	}
	pause := renderPause{version: 1, pause: pauseCallback()}
	defer e.renderClose(page)
	for start := true; ; start = false {
		if !start {
			// Resume behind waiting visible work, even when this raster is visible.
			mu.Unlock()
			mu.lock(ctx, background)
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		activeRenderContext, renderDeadline = ctx, time.Now().Add(slice)
		var status int32
		if start {
			status = e.renderStart(bitmap, page, x, y, width, height, rotation, flags, &pause)
		} else {
			status = e.renderContinue(page, &pause)
		}
		activeRenderContext = nil
		if err := ctx.Err(); err != nil {
			return err
		}
		switch status {
		case done:
			return nil
		case continued:
			continue
		default:
			return fmt.Errorf("PDFium progressive rendering failed (status %d)", status)
		}
	}
}

func (e *Engine) HasProgressiveRendering() bool {
	return e.renderStart != nil && e.renderContinue != nil && e.renderClose != nil
}
