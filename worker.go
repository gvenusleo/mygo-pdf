package pdf

import (
	"context"
	"io"
	"slices"
	"sync"
)

type loadJob struct {
	ctx      context.Context
	id       uint64
	data     []byte
	reader   io.ReaderAt
	size     int64
	password string
	release  func()
	viewport loadViewport
}

func (job *loadJob) releaseSource() {
	if job != nil && job.release != nil {
		job.release()
	}
}

type loadViewport struct {
	anchor                   readingPosition
	width, height, zoom, gap float32
	fit                      Fit
	rotation                 int
}

type loadPlan struct {
	viewport loadViewport
	layout   pageLayout
	y        float32
	keys     []rasterKey
}

func (l pageLayout) viewportOffset(vp loadViewport) float32 {
	return max(0, min(l.offset(vp.anchor), l.height-vp.height))
}

func (vp loadViewport) plan(id uint64, layout pageLayout) loadPlan {
	y := layout.viewportOffset(vp)
	first, last := layout.visiblePages(y, vp.height)
	keys := make([]rasterKey, last-first+1)
	budget := visibleBudget / len(keys)
	for i := range keys {
		keys[i] = layout.fallback(id, first+i, vp.zoom, budget)
	}
	return loadPlan{viewport: vp, layout: layout, y: y, keys: keys}
}

type copyJob struct {
	ctx      context.Context
	document uint64
	selected textRange
	apply    func(string)
}

type worker struct {
	mu             sync.Mutex
	wake           chan struct{}
	stop           chan struct{}
	done           chan struct{}
	load           *loadJob
	copy           *copyJob
	clear          bool
	pages          []rasterKey
	missingBitmaps int
	metadata       []geometryKey
	prefetch       rasterKey
	inFlight       rasterKey
	inMetadata     geometryKey
	background     bool
	cancelPage     context.CancelFunc
	getEngine      func() (*engine, error)
	dimensions     *dimensionQueue
	outline        *outlineJob
	find           *findJob
}

func (w *worker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *worker) submitLoad(job *loadJob) {
	w.mu.Lock()
	previous := w.load
	w.load = job
	w.outline, w.find = nil, nil
	w.mu.Unlock()
	previous.releaseSource()
	w.signal()
}

func (w *worker) cancelQueuedLoad(id uint64) {
	w.mu.Lock()
	if w.load != nil && w.load.id == id {
		w.load.releaseSource()
		w.load = nil
	}
	w.mu.Unlock()
}

func (w *worker) submitPages(pages []rasterKey, prefetch rasterKey, missing int, metadata []geometryKey) {
	w.mu.Lock()
	w.pages = slices.Clone(pages)
	w.missingBitmaps, w.metadata = missing, slices.Clone(metadata)
	w.prefetch = prefetch
	if w.cancelPage != nil {
		if w.inMetadata.document != 0 {
			if missing > 0 || !slices.Contains(metadata, w.inMetadata) {
				w.cancelPage()
			}
		} else if !slices.Contains(pages, w.inFlight) ||
			w.background && (prefetch != w.inFlight || len(pages) > 0 && pages[0] != w.inFlight || w.shouldReadMetadata()) {
			w.cancelPage()
		}
	}
	w.mu.Unlock()
	w.signal()
}

func (w *worker) submitCopy(job copyJob) {
	w.mu.Lock()
	w.copy = &job
	w.mu.Unlock()
	w.signal()
}

func (w *worker) reset() {
	w.mu.Lock()
	w.clear = true
	previous := w.load
	w.load, w.copy, w.pages, w.metadata = nil, nil, nil, nil
	w.dimensions = nil
	w.outline, w.find = nil, nil
	w.missingBitmaps = 0
	if w.cancelPage != nil {
		w.cancelPage()
	}
	w.mu.Unlock()
	previous.releaseSource()
	w.signal()
}

func (w *worker) shouldReadMetadata() bool {
	return w.missingBitmaps == 0 && len(w.metadata) > 0
}

type workerTask struct {
	load       *loadJob
	copy       *copyJob
	clear      bool
	page       rasterKey
	metadata   geometryKey
	size       geometryKey
	outline    *outlineJob
	find       *findJob
	ctx        context.Context
	background bool
}

func (w *worker) next() (task workerTask) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case w.clear:
		w.clear = false
		task.clear = true
	case w.load != nil:
		task.load, w.load = w.load, nil
	case w.copy != nil:
		task.copy, w.copy = w.copy, nil
	case w.dimensions.hasWanted():
		task.size, task.ctx = w.dimensions.nextWanted()
	case w.shouldReadMetadata():
		task.metadata, w.metadata = w.metadata[0], w.metadata[1:]
		w.inMetadata = task.metadata
		task.ctx, w.cancelPage = context.WithCancel(context.Background())
	case len(w.pages) > 0 && w.pages[0] != w.prefetch:
		w.nextRaster(&task)
	case w.outline != nil:
		task.outline = w.outline
	case w.find != nil:
		task.find = w.find
	case len(w.pages) > 0:
		w.nextRaster(&task)
	case w.dimensions.hasPrefetch() || w.dimensions.hasBackground():
		task.size, task.ctx = w.dimensions.nextBackground()
		task.background = true
	}
	return task
}

func (w *worker) nextRaster(task *workerTask) {
	task.page, w.pages = w.pages[0], w.pages[1:]
	w.missingBitmaps = max(0, w.missingBitmaps-1)
	w.inFlight = task.page
	w.background = task.page == w.prefetch
	task.background = w.background
	task.ctx, w.cancelPage = context.WithCancel(context.Background())
}

func (w *worker) finishPage(key rasterKey, delivered bool) {
	w.mu.Lock()
	w.cancelPage()
	w.cancelPage = nil
	if delivered {
		if i := slices.Index(w.pages, key); i >= 0 {
			if i < w.missingBitmaps {
				w.missingBitmaps--
			}
			w.pages = slices.Delete(w.pages, i, i+1)
		}
	}
	w.mu.Unlock()
}

func (v *Viewer) run() {
	w := v.worker
	defer close(w.done)
	var e *engine
	var doc *document
	defer func() { e.Close(doc) }()
	for {
		select {
		case <-w.stop:
			return
		default:
		}
		task := w.next()
		switch {
		case task.clear:
			e.Close(doc)
			doc = nil
		case task.load != nil:
			e, doc = v.replaceDocument(e, doc, task.load)
		case task.copy != nil:
			v.readSelection(e, doc, task.copy)
		case task.size.document != 0:
			v.readDimension(e, doc, task)
		case task.metadata.document != 0:
			v.readMetadata(e, doc, task.metadata, task.ctx)
		case task.outline != nil:
			v.readOutlineStep(e, doc, task.outline)
		case task.find != nil:
			v.findStep(e, doc, task.find)
		case task.ctx != nil:
			v.renderPage(e, doc, task.page, task.ctx, task.background)
		default:
			select {
			case <-w.stop:
				return
			case <-w.wake:
			}
		}
	}
}

func (v *Viewer) replaceDocument(e *engine, old *document, job *loadJob) (*engine, *document) {
	if job.ctx.Err() != nil {
		job.releaseSource()
		return e, old
	}
	doc, e, err := v.worker.openDocument(e, job)
	var plan loadPlan
	var frames []*pageFrame
	if err == nil {
		plan, err = prepareLayout(e, job.ctx, doc, job.viewport, pageGeometry{})
		if err == nil {
			frames, err = renderLoad(e, job.ctx, doc, plan.keys, nil)
		}
	}
	if err != nil {
		e.Close(doc)
		doc = nil
		job.releaseSource()
	}
	for {
		reply := v.publishLoad(job, doc, plan, frames, err)
		if reply.next != nil {
			plan, err = prepareLayout(e, job.ctx, doc, *reply.next, plan.layout.geometry)
			if err == nil {
				frames, err = renderLoad(e, job.ctx, doc, plan.keys, frames)
			}
			if err != nil {
				e.Close(doc)
				doc, frames = nil, nil
			}
			continue
		}
		if reply.installed {
			e.Close(old)
			return e, doc
		}
		e.Close(doc)
		return e, old
	}
}

type loadReply struct {
	installed bool
	next      *loadViewport
}

func (v *Viewer) publishLoad(job *loadJob, doc *document, plan loadPlan, frames []*pageFrame, err error) loadReply {
	reply := make(chan loadReply, 1)
	v.publish(func() {
		if !v.currentLoad(job) {
			reply <- loadReply{}
			return
		}
		if err == nil && v.document != 0 && v.measured {
			// Window.Update runs before the view can observe changes to its parent geometry.
			v.pendingRefresh = func(commit bool) {
				if commit {
					reply <- v.applyLoad(job, doc, plan, frames, err)
				} else {
					reply <- loadReply{}
				}
			}
			return
		}
		reply <- v.applyLoad(job, doc, plan, frames, err)
	})
	select {
	case result := <-reply:
		return result
	case <-v.worker.stop:
		return loadReply{}
	}
}

func (v *Viewer) currentLoad(job *loadJob) bool {
	return !v.closed && job.id == v.loadID && job.ctx.Err() == nil
}

func (v *Viewer) resolveRefresh(commit bool) {
	if pending := v.pendingRefresh; pending != nil {
		v.pendingRefresh = nil
		pending(commit)
	}
}

func (v *Viewer) applyLoad(job *loadJob, doc *document, plan loadPlan, frames []*pageFrame, err error) loadReply {
	if !v.currentLoad(job) {
		return loadReply{}
	}
	// Clamp scroll before checking which bitmaps cover the current viewport.
	if viewport := v.loadViewport(); err == nil && viewport != plan.viewport {
		next := viewport.plan(doc.ID, plan.layout.geometry.fit(viewport))
		if !slices.Equal(next.keys, plan.keys) {
			return loadReply{next: &viewport}
		}
		plan = next
	}
	return loadReply{installed: v.finishLoad(doc, plan, frames, err)}
}

func (w *worker) openDocument(e *engine, job *loadJob) (*document, *engine, error) {
	if e == nil {
		var err error
		e, err = w.getEngine()
		if err != nil {
			return nil, nil, err
		}
	}
	password := job.password
	job.password = ""
	if job.release != nil {
		doc, err := e.OpenReader(job.ctx, job.id, job.reader, job.size, password, job.release)
		return doc, e, err
	}
	doc, err := e.OpenBytes(job.ctx, job.id, job.data, password)
	return doc, e, err
}

func renderLoad(e *engine, ctx context.Context, doc *document, keys []rasterKey, previous []*pageFrame) ([]*pageFrame, error) {
	frames := make([]*pageFrame, len(keys))
	if len(previous) > 0 {
		first := previous[0].key.Page
		for i, key := range keys {
			index := key.Page - first
			if index >= 0 && index < len(previous) && previous[index].key == key {
				frames[i] = previous[index]
			}
		}
		clear(previous)
	}
	for i, key := range keys {
		if frames[i] != nil {
			continue
		}
		frame, err := renderFrame(e, ctx, doc, key, false)
		if err != nil {
			return nil, err
		}
		frames[i] = frame
	}
	return frames, nil
}

func (v *Viewer) renderPage(e *engine, doc *document, key rasterKey, ctx context.Context, background bool) {
	delivered := false
	if doc != nil && doc.ID == key.Document && ctx.Err() == nil {
		frame, err := renderFrame(e, ctx, doc, key, background)
		if ctx.Err() == nil {
			delivered = v.deliver(ctx, func() bool { return v.finishPage(key, frame, err) })
		}
	}
	v.worker.finishPage(key, delivered)
}

func (v *Viewer) readMetadata(e *engine, doc *document, key geometryKey, ctx context.Context) {
	delivered := false
	if doc != nil && doc.ID == key.document && ctx.Err() == nil {
		data, err := e.ReadGeometry(ctx, doc, key.page)
		if ctx.Err() == nil {
			delivered = v.deliver(ctx, func() bool { return v.finishMetadata(key, data, err) })
		}
	}
	w := v.worker
	w.mu.Lock()
	w.cancelPage()
	w.cancelPage, w.inMetadata = nil, geometryKey{}
	if delivered {
		w.metadata = slices.DeleteFunc(w.metadata, func(wanted geometryKey) bool { return wanted == key })
	}
	w.mu.Unlock()
}

func (v *Viewer) readSelection(e *engine, doc *document, job *copyJob) {
	if doc == nil || doc.ID != job.document {
		return
	}
	text, err := e.CopyText(job.ctx, doc, job.selected)
	if job.ctx.Err() == nil && (err != nil || text != "") {
		v.deliver(job.ctx, func() bool {
			if v.document != job.document || !v.hasSelection || v.selection != job.selected {
				return false
			}
			v.interactionErr = err
			if err == nil {
				job.apply(text)
			}
			v.notify()
			return true
		})
	}
}

func renderFrame(e *engine, ctx context.Context, doc *document, key rasterKey, background bool) (*pageFrame, error) {
	pixels, err := e.Render(ctx, doc, key, background)
	if err != nil {
		return nil, err
	}
	return &pageFrame{key: key, pixels: pixels}, nil
}

// Wait for the UI thread to consume each result so bitmaps cannot pile up in its queue.
func (v *Viewer) deliver(ctx context.Context, apply func() bool) bool {
	accepted := make(chan bool, 1)
	v.publish(func() {
		accepted <- (!v.closed && ctx.Err() == nil && apply())
	})
	select {
	case ok := <-accepted:
		return ok
	case <-ctx.Done():
		return false
	case <-v.worker.stop:
		return false
	}
}
