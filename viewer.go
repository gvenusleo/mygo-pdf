// Package pdf provides a native MyGo PDF viewer backed by PDFium.
package pdf

import (
	"container/list"
	"context"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/gvenusleo/mygo-pdf/internal/render"
)

type cachedPage struct {
	frame   *pageFrame
	failed  rasterKey
	err     error
	recency *list.Element
}

// Viewer holds the state of a single window's PDF viewport.
type Viewer struct {
	publish            func(func())
	invalidate         func()
	unbind             func()
	options            Options
	fit                Fit
	loadErr            error
	stopCancellation   func() bool
	nextSubscriber     uint64
	subscribers        map[uint64]func(State)
	lastState          State
	notificationQueued bool
	worker             *worker
	closed             bool
	loadID             uint64
	cancelLoad         context.CancelFunc
	pendingRefresh     func(bool)
	document           uint64
	docContext         context.Context
	cancelDoc          context.CancelFunc
	pages              []pageSize
	wanted             []rasterKey
	prefetch           rasterKey
	missingBitmaps     int
	metadata           map[int]cachedGeometry
	metadataOrder      []int
	metadataWanted     []geometryKey
	metadataBytes      int
	cache              map[int]cachedPage
	recency            list.List
	cacheSize          int
	tiles              map[rasterKey]cachedPage
	tileKeys           []rasterKey
	tileBytes          int
	lastScrollY        float32
	scrollingUp        bool
	scale              float64

	scroll        ui.ScrollState
	navigation    pageNavigation
	layout        pageLayout
	width         float32
	height        float32
	windowWidth   float32
	windowHeight  float32
	layoutKey     any
	gap           float32
	zoom          float32
	measured      bool
	waitingBounds bool
	selection     textRange
	pointer       pointerDrag
	hasSelection  bool
}

func newViewer(publish func(func()), getEngine func() (*engine, error)) *Viewer {
	v := &Viewer{
		publish: publish, zoom: 1, width: 640, scale: 1,
		cache: make(map[int]cachedPage), tiles: make(map[rasterKey]cachedPage),
		worker: &worker{
			wake: make(chan struct{}, 1), stop: make(chan struct{}),
			done: make(chan struct{}), getEngine: getEngine,
		},
	}
	go v.run()
	return v
}

func (v *Viewer) setScale(scale float64) bool {
	scale = max(1, min(4, scale))
	if v.scale == scale {
		return false
	}
	v.scale = scale
	return true
}

func (v *Viewer) loadViewport() loadViewport {
	return loadViewport{anchor: v.anchor(), width: v.width, height: v.height, zoom: v.zoom, gap: v.gap, fit: v.fit}
}

func (v *Viewer) finishLoad(doc *document, plan loadPlan, frames []*pageFrame, err error) bool {
	v.stopLoad()
	v.loadErr = err
	if err != nil {
		v.notify()
		return false
	}
	if v.cancelDoc != nil {
		v.cancelDoc()
	}
	page := v.selectedPage()
	if v.document == 0 {
		page = 1
	}
	v.docContext, v.cancelDoc = context.WithCancel(context.Background())
	v.document, v.pages = doc.ID, doc.Pages
	v.hasSelection, v.pointer = false, pointerDrag{}
	v.layout, v.scroll.Y = plan.layout, plan.y
	v.navigation = pageNavigation{}
	if page = min(page, len(v.pages)); page > 0 {
		first, last := v.visiblePages()
		if page-1 >= first && page-1 <= last {
			v.navigation = pageNavigation{page: page, y: v.scroll.Y}
		}
	}
	v.lastScrollY = v.scroll.Y
	v.scroll.X = min(v.scroll.X, max(0, v.layout.width-v.width))
	v.setRasterDemand(nil, -1, 0, nil)
	v.resetMetadata()
	v.clearTiles()
	// Install only images from the new document so its text and links match the display.
	first, last := frames[0].key.Page, frames[len(frames)-1].key.Page
	for page := range v.cache {
		if page < first || page > last {
			v.removePage(page)
		}
	}
	for _, frame := range frames {
		v.cachePage(frame)
	}
	v.notify()
	return true
}

// Clear cancels the active load and releases the displayed document.
func (v *Viewer) Clear() {
	if v.closed {
		return
	}
	v.stopLoad()
	v.loadID++
	v.loadErr = nil
	v.clearDocument()
	v.redraw()
}

func (v *Viewer) clearDocument() {
	if v.cancelDoc != nil {
		v.cancelDoc()
		v.docContext, v.cancelDoc = nil, nil
	}
	v.document, v.pages, v.wanted = 0, nil, nil
	v.prefetch = rasterKey{}
	v.missingBitmaps = 0
	v.resetMetadata()
	v.layout, v.zoom = pageLayout{}, 1
	v.navigation = pageNavigation{}
	v.scroll.X, v.scroll.Y = 0, 0
	v.lastScrollY, v.scrollingUp = 0, false
	v.hasSelection, v.pointer = false, pointerDrag{}
	clear(v.cache)
	v.clearTiles()
	v.recency.Init()
	v.cacheSize = 0
	v.worker.reset()
}

// Close stops the viewer without waiting on the UI thread.
func (v *Viewer) Close() {
	if !v.closed {
		v.closed = true
		v.stopLoad()
		v.clearDocument()
		clear(v.subscribers)
		if v.unbind != nil {
			v.unbind()
			v.unbind = nil
		}
		close(v.worker.stop)
	}
}

// Done closes when the worker has released the native document. It can be used from any goroutine.
func (v *Viewer) Done() <-chan struct{} { return v.worker.done }

func (v *Viewer) setRasterDemand(keys []rasterKey, prefetchPage, missing int, metadata []geometryKey) {
	var prefetch rasterKey
	if len(keys) > 0 && keys[len(keys)-1].Page == prefetchPage {
		prefetch = keys[len(keys)-1]
	}
	if !slices.Equal(keys, v.wanted) || v.prefetch != prefetch ||
		v.missingBitmaps != missing || !slices.Equal(metadata, v.metadataWanted) {
		v.wanted, v.prefetch, v.missingBitmaps, v.metadataWanted = keys, prefetch, missing, metadata
		v.worker.submitPages(keys, prefetch, missing, metadata)
	}
}

func (v *Viewer) clearTiles() {
	clear(v.tiles)
	v.tileKeys = nil
	v.tileBytes = 0
}

func (v *Viewer) finishPage(key rasterKey, frame *pageFrame, err error) bool {
	if v.document != key.Document || !slices.Contains(v.wanted, key) {
		return false
	}
	if !key.Tile.Empty() {
		entry := v.tiles[key]
		if err == nil {
			frame.installBitmap(entry.frame)
			v.tiles[key] = cachedPage{frame: frame}
			if entry.frame == nil {
				v.tileBytes += frame.frameBytes()
			}
			v.trimTiles()
		} else {
			entry.failed, entry.err = key, err
			v.tiles[key] = entry
		}
		return true
	}
	if err == nil {
		v.cachePage(frame)
		for v.cacheSize > render.BitmapBudget {
			v.removePage(v.recency.Back().Value.(int))
		}
	} else {
		entry := v.cache[key.Page]
		entry.failed, entry.err = key, err
		v.cache[key.Page] = entry
	}
	return true
}

func (v *Viewer) cachePage(frame *pageFrame) {
	page := frame.key.Page
	frame.installBitmap(v.cache[page].frame)
	v.removePage(page)
	v.cache[page] = cachedPage{frame: frame, recency: v.recency.PushFront(page)}
	v.cacheSize += frame.frameBytes()
}

func (v *Viewer) removePage(page int) {
	entry := v.cache[page]
	delete(v.cache, page)
	if entry.frame != nil {
		v.cacheSize -= entry.frame.frameBytes()
		v.recency.Remove(entry.recency)
	}
}

func (f *pageFrame) installBitmap(old *pageFrame) {
	if old != nil && old.key.Region().Size() == f.key.Region().Size() {
		// Reuse the texture so a PDF refresh only has to upload new pixels.
		old.bitmap.Update(f.pixels)
		f.bitmap = old.bitmap
	} else {
		f.bitmap = ui.NewBitmap(f.pixels)
	}
	f.pixels = nil
}

func (f *pageFrame) frameBytes() int {
	region := f.key.Region()
	return region.Dx() * region.Dy() * 4
}
