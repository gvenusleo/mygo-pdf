package pdf

import (
	"context"
	"math"
	"slices"

	"github.com/egoist/mygo"
)

// Fit controls the base scale before applying the user's zoom multiplier.
type Fit uint8

const (
	FitWidth Fit = iota
	FitPage
)

// LoadMode determines whether the incoming bytes replace the document or refresh it in place.
type LoadMode uint8

const (
	OpenDocument LoadMode = iota
	RefreshDocument
)

// RenderState describes the currently visible portion of the displayed document.
type RenderState uint8

const (
	RenderEmpty RenderState = iota
	RenderPending
	RenderRefining
	RenderReady
	RenderFailed
)

// State is a snapshot of the viewer. Page is one-based, or zero when empty.
type State struct {
	RequestGeneration   uint64
	DisplayedGeneration uint64
	Page, PageCount     int
	Zoom                float32
	Fit                 Fit
	Loading             bool
	Render              RenderState
	Err                 error
}

// Options customizes platform integration and the built-in menu labels.
type Options struct {
	// LibraryPath optionally overrides the PDFium library with an absolute path.
	LibraryPath string
	// OnLink receives validated http, https and mailto URLs on the UI thread.
	OnLink func(string)
	// OpenExternal adds a menu item that lets the host open the current file.
	OpenExternal func()
	// CanOpenExternal optionally decides whether the current document offers OpenExternal.
	CanOpenExternal func() bool
	Labels          Labels
}

// Labels contains optional overrides for English viewer labels.
type Labels struct {
	Document, Page, Copy, ZoomIn, ZoomOut, FitWidth, FitPage, OpenExternal string
}

// ChineseLabels returns Simplified Chinese viewer labels. Page is a format string.
func ChineseLabels() Labels {
	return Labels{Document: "PDF 页面", Page: "第 %d 页", Copy: "复制", ZoomIn: "放大", ZoomOut: "缩小", FitWidth: "适应宽度", FitPage: "适应页面", OpenExternal: "在默认应用中打开"}
}

// NewViewer binds a viewer to a window, including display changes and closure.
// Its public operations are called on the window's UI thread.
func NewViewer(win *mygo.Window, options Options) *Viewer {
	v := newViewer(win.Update, func() (*engine, error) { return resourceEngine(options.LibraryPath) })
	v.options = options
	v.invalidate = win.Invalidate
	update := func() {
		if v.setScale(mygo.Screen.DisplayMatching(win.Bounds()).ScaleFactor) {
			win.Invalidate()
		}
	}
	offDisplay := mygo.Screen.OnDisplaysChanged(update)
	offMove, offResize := win.OnMove(update), win.OnResize(update)
	offClose := win.OnClosed(v.Close)
	v.unbind = func() {
		offDisplay()
		offMove()
		offResize()
		offClose()
	}
	update()
	return v
}

// LoadBytes schedules a PDF. The caller hands over an immutable byte slice.
// Zero means the request was already canceled or the viewer was closed.
func (v *Viewer) LoadBytes(ctx context.Context, data []byte, mode LoadMode) uint64 {
	if v.closed || ctx.Err() != nil {
		return 0
	}
	v.stopLoad()
	if mode != RefreshDocument {
		v.clearDocument()
	}
	ctx, v.cancelLoad = context.WithCancel(ctx)
	v.loadID++
	id, publish := v.loadID, v.publish
	v.loadErr = nil
	v.stopCancellation = context.AfterFunc(ctx, func() {
		publish(func() {
			if !v.closed && v.loadID == id && v.cancelLoad != nil {
				v.stopLoad()
				v.loadErr = ctx.Err()
				v.notify()
			}
		})
	})
	v.worker.submitLoad(&loadJob{ctx: ctx, id: id, data: data, viewport: v.loadViewport()})
	v.redraw()
	return id
}

func (v *Viewer) stopLoad() {
	if v.cancelLoad != nil {
		v.stopCancellation()
		v.cancelLoad()
		v.cancelLoad, v.stopCancellation = nil, nil
	}
}

// State returns the current document, viewport, load and render status.
func (v *Viewer) State() State {
	s := State{RequestGeneration: v.loadID, DisplayedGeneration: v.document, PageCount: len(v.pages), Zoom: v.zoom, Fit: v.fit, Loading: v.cancelLoad != nil, Err: v.loadErr}
	if len(v.pages) == 0 {
		return s
	}
	s.Page = v.currentPage() + 1
	s.Render = RenderReady
	first, last := v.visiblePages()
	for i := first; i <= last; i++ {
		entry := v.cache[i]
		if entry.err != nil {
			s.Render = RenderFailed
			if s.Err == nil {
				s.Err = entry.err
			}
		} else if entry.frame == nil && s.Render != RenderFailed {
			s.Render = RenderPending
		}
	}
	for _, key := range v.tileKeys {
		if entry := v.tiles[key]; entry.err != nil {
			s.Render = RenderFailed
			if s.Err == nil {
				s.Err = entry.err
			}
		}
	}
	if s.Render == RenderReady && v.visibleWorkPending() {
		s.Render = RenderRefining
	}
	return s
}

// OnChange subscribes to coalesced UI-thread state changes. It returns an unsubscribe function.
func (v *Viewer) OnChange(fn func(State)) func() {
	if v.closed || fn == nil {
		return func() {}
	}
	v.nextSubscriber++
	id := v.nextSubscriber
	if v.subscribers == nil {
		v.subscribers = make(map[uint64]func(State))
		v.lastState = v.State()
	}
	v.subscribers[id] = fn
	return func() { delete(v.subscribers, id) }
}

func (v *Viewer) notify() {
	if len(v.subscribers) == 0 || v.notificationQueued || v.State() == v.lastState {
		return
	}
	v.notificationQueued = true
	v.publish(func() {
		v.notificationQueued = false
		if v.closed || len(v.subscribers) == 0 {
			return
		}
		s := v.State()
		if s == v.lastState {
			return
		}
		v.lastState = s
		for id := range v.subscribers {
			if v.closed || v.State() != s {
				break
			}
			v.subscribers[id](s)
		}
	})
}

// GoToPage scrolls to the top of a one-based page. It returns false for an invalid page.
func (v *Viewer) GoToPage(page int) bool {
	if page < 1 || page > len(v.pages) {
		return false
	}
	v.scroll.Y = min(v.layout.offset(readingPosition{page: page - 1}), max(0, v.layout.height-v.height))
	v.redraw()
	return true
}

// SetZoom sets the multiplier relative to the current fit mode, anchored at the viewport center.
func (v *Viewer) SetZoom(zoom float32) {
	if math.IsNaN(float64(zoom)) || len(v.pages) == 0 {
		return
	}
	v.zoomCentered(zoom)
	v.redraw()
}

// SetFit changes the fit mode and restores its base scale.
func (v *Viewer) SetFit(fit Fit) {
	if v.closed || fit > FitPage || v.fit == fit && v.zoom == 1 {
		return
	}
	anchor := v.anchor()
	v.fit, v.zoom = fit, 1
	v.layout = newFittedLayout(v.pages, v.loadViewport())
	v.scroll.Y = min(v.layout.offset(anchor), max(0, v.layout.height-v.height))
	v.scroll.X = min(v.scroll.X, max(0, v.layout.width-v.width))
	v.redraw()
}

func (v *Viewer) redraw() {
	if v.invalidate != nil {
		v.invalidate()
	}
	v.notify()
}

func (v *Viewer) currentPage() int {
	y := v.scroll.Y + v.height/2
	page := v.layout.firstPage(y)
	if page > 0 && y < v.layout.pages[page].Y {
		before := v.layout.pages[page-1]
		if y-before.Y-before.H <= v.layout.pages[page].Y-y {
			page--
		}
	}
	return page
}

func (v *Viewer) visibleWorkPending() bool {
	_, last := v.visiblePages()
	return slices.ContainsFunc(v.wanted, func(key rasterKey) bool {
		return key.Page <= last && v.cache[key.Page].failed != key && v.tiles[key].failed != key
	})
}
