package pdf

import (
	"context"
	"slices"
)

// The queue belongs to the worker. The window thread supplies current priorities.
type dimensionQueue struct {
	document uint64
	ctx      context.Context
	checked  []bool
	wanted   []int
	prefetch int
	cursor   int
	inFlight int
}

func (w *worker) startDimensions(plan loadPlan, ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	g := plan.layout.geometry.lazy
	w.dimensions = nil
	if g == nil {
		return
	}
	q := &dimensionQueue{document: plan.keys[0].Document, ctx: ctx, checked: make([]bool, len(g.sizes)), inFlight: -1, prefetch: -1}
	for page, size := range g.sizes {
		q.checked[page] = size.Width > 0
	}
	w.dimensions = q
}

func (w *worker) requestSizes(document uint64, pages []int, prefetch int) {
	w.mu.Lock()
	if q := w.dimensions; q != nil && q.document == document {
		q.prefetch = prefetch
		q.wanted = q.wanted[:0]
		for _, page := range pages {
			if !q.checked[page] && page != q.inFlight {
				q.wanted = append(q.wanted, page)
			}
		}
		if len(q.wanted) > 0 || q.hasPrefetch() {
			w.signal()
		}
	}
	w.mu.Unlock()
}

func (q *dimensionQueue) hasWanted() bool { return q != nil && len(q.wanted) > 0 }

func (q *dimensionQueue) nextWanted() (geometryKey, context.Context) {
	q.inFlight, q.wanted = q.wanted[0], q.wanted[1:]
	return geometryKey{q.document, q.inFlight}, q.ctx
}

func (q *dimensionQueue) hasBackground() bool {
	if q == nil {
		return false
	}
	for q.cursor < len(q.checked) && q.checked[q.cursor] {
		q.cursor++
	}
	return q.cursor < len(q.checked)
}

func (q *dimensionQueue) hasPrefetch() bool {
	return q != nil && q.prefetch >= 0 && !q.checked[q.prefetch]
}

func (q *dimensionQueue) nextBackground() (geometryKey, context.Context) {
	q.inFlight = q.cursor
	if q.hasPrefetch() {
		q.inFlight = q.prefetch
	}
	return geometryKey{q.document, q.inFlight}, q.ctx
}

func (v *Viewer) readDimension(e *engine, doc *document, task workerTask) {
	key := task.size
	if doc == nil || doc.ID != key.document || task.ctx.Err() != nil {
		return
	}
	size, err := e.ReadPageSize(task.ctx, doc, key.page, task.background)
	if task.ctx.Err() == nil {
		v.deliver(task.ctx, func() bool { return v.finishSize(key, size, err) })
	}
	w := v.worker
	w.mu.Lock()
	if q := w.dimensions; q != nil && q.document == key.document {
		q.checked[key.page], q.inFlight = true, -1
		q.wanted = slices.DeleteFunc(q.wanted, func(page int) bool { return page == key.page })
	}
	w.mu.Unlock()
}
