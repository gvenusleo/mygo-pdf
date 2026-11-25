package pdf

import (
	"context"
	"slices"
	"sync"
)

type loadJob struct {
	ctx      context.Context
	id       uint64
	data     []byte
	viewport loadViewport
}

type loadViewport struct {
	anchor                   readingPosition
	width, height, zoom, gap float32
	fit                      Fit
}

type loadPlan struct {
	viewport loadViewport
	layout   pageLayout
	y        float32
	keys     []rasterKey
}

func (vp loadViewport) plan(doc *document) loadPlan {
	layout := newFittedLayout(doc.Pages, vp)
	y := max(0, min(layout.offset(vp.anchor), layout.height-vp.height))
	first, last := layout.visiblePages(y, vp.height)
	keys := make([]rasterKey, last-first+1)
	budget := visibleBudget / len(keys)
	for i := range keys {
		keys[i] = layout.fallback(doc.ID, first+i, vp.zoom, budget)
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
	mu         sync.Mutex
	wake       chan struct{}
	stop       chan struct{}
	done       chan struct{}
	load       *loadJob
	copy       *copyJob
	clear      bool
	pages      []rasterKey
	inFlight   rasterKey
	cancelPage context.CancelFunc
	getEngine  func() (*engine, error)
}

func (w *worker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *worker) submitLoad(job *loadJob) {
	w.mu.Lock()
	w.load = job
	w.mu.Unlock()
	w.signal()
}

func (w *worker) submitPages(pages []rasterKey) {
	w.mu.Lock()
	w.pages = slices.Clone(pages)
	if w.cancelPage != nil && !slices.Contains(pages, w.inFlight) {
		w.cancelPage()
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
	w.load, w.copy, w.pages = nil, nil, nil
	if w.cancelPage != nil {
		w.cancelPage()
	}
	w.mu.Unlock()
	w.signal()
}

func (w *worker) next() (load *loadJob, copy *copyJob, clear bool, page rasterKey, ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case w.clear:
		w.clear = false
		clear = true
	case w.load != nil:
		load, w.load = w.load, nil
	case w.copy != nil:
		copy, w.copy = w.copy, nil
	case len(w.pages) > 0:
		page, w.pages = w.pages[0], w.pages[1:]
		w.inFlight = page
		ctx, w.cancelPage = context.WithCancel(context.Background())
	}
	return
}

func (w *worker) finishPage(key rasterKey, delivered bool) {
	w.mu.Lock()
	w.cancelPage()
	w.cancelPage = nil
	if delivered {
		w.pages = slices.DeleteFunc(w.pages, func(p rasterKey) bool { return p == key })
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
		load, copy, clear, page, ctx := w.next()
		switch {
		case clear:
			e.Close(doc)
			doc = nil
		case load != nil:
			e, doc = v.replaceDocument(e, doc, load)
		case copy != nil:
			v.readSelection(e, doc, copy)
		case ctx != nil:
			v.renderPage(e, doc, page, ctx)
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
		return e, old
	}
	doc, e, err := v.worker.openDocument(e, job)
	var plan loadPlan
	var frames []*pageFrame
	if err == nil {
		plan = job.viewport.plan(doc)
		frames, err = renderLoad(e, job.ctx, doc, plan.keys, nil)
	}
	for {
		reply := v.publishLoad(job, doc, plan, frames, err)
		if reply.next != nil {
			plan = *reply.next
			frames, err = renderLoad(e, job.ctx, doc, plan.keys, frames)
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
	next      *loadPlan
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
		next := viewport.plan(doc)
		if !slices.Equal(next.keys, plan.keys) {
			return loadReply{next: &next}
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
	doc, err := e.Open(job.ctx, job.id, job.data)
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
		frame, err := renderFrame(e, ctx, doc, key)
		if err != nil {
			return nil, err
		}
		frames[i] = frame
	}
	return frames, nil
}

func (v *Viewer) renderPage(e *engine, doc *document, key rasterKey, ctx context.Context) {
	delivered := false
	if doc != nil && doc.ID == key.Document && ctx.Err() == nil {
		frame, err := renderFrame(e, ctx, doc, key)
		if ctx.Err() == nil {
			delivered = v.deliver(func() bool { return v.finishPage(key, frame, err) })
		}
	}
	v.worker.finishPage(key, delivered)
}

func (v *Viewer) readSelection(e *engine, doc *document, job *copyJob) {
	if doc == nil || doc.ID != job.document {
		return
	}
	text := e.CopyText(job.ctx, doc, job.selected)
	if text != "" {
		v.deliver(func() bool {
			if v.document != job.document || !v.hasSelection || v.selection != job.selected {
				return false
			}
			job.apply(text)
			return true
		})
	}
}

func renderFrame(e *engine, ctx context.Context, doc *document, key rasterKey) (*pageFrame, error) {
	frame, err := e.Render(ctx, doc, key)
	if err != nil {
		return nil, err
	}
	return &pageFrame{key: key, pixels: frame.Pixels, chars: frame.Chars, links: frame.Links}, nil
}

// Wait for the UI thread to consume each result so bitmaps cannot pile up in its queue.
func (v *Viewer) deliver(apply func() bool) bool {
	accepted := make(chan bool, 1)
	v.publish(func() {
		accepted <- (!v.closed && apply())
	})
	select {
	case ok := <-accepted:
		return ok
	case <-v.worker.stop:
		return false
	}
}
