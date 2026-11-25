package pdf

import (
	"context"

	"github.com/gvenusleo/mygo-pdf/internal/pdfium"
)

// OutlineResult is a snapshot of one displayed document's outline.
type OutlineResult struct {
	RequestGeneration   uint64
	DisplayedGeneration uint64
	Entries             []OutlineEntry
	Truncated           bool
	Err                 error
}

// OutlineEntry is a document heading. Page is one-based, or zero if the
// entry has no supported local destination.
type OutlineEntry struct {
	Title    string
	Page     int
	Children []OutlineEntry
	owner    *Viewer
	document uint64
	target   pdfium.Destination
	page     int
}

type outlineFrame struct {
	handle uintptr
	into   *[]OutlineEntry
	depth  int
}

type outlineJob struct {
	ctx    context.Context
	result OutlineResult
	apply  func(OutlineResult)
	frames []outlineFrame
	seen   map[uintptr]struct{}
}

// ReadOutline schedules a snapshot and calls onResult on the window thread.
// A newer request or load, cancellation, Clear, or Close retires the request.
// It returns zero if the viewer is closed, loading, or without a document,
// the context is already canceled, or the callback is nil.
func (v *Viewer) ReadOutline(ctx context.Context, onResult func(OutlineResult)) uint64 {
	if v.closed || v.cancelLoad != nil || v.document == 0 || ctx.Err() != nil || onResult == nil {
		return 0
	}
	v.cancelOutlineRequest()
	ctx, v.cancelOutline = context.WithCancel(ctx)
	v.outlineID++
	job := &outlineJob{ctx: ctx, apply: onResult, seen: make(map[uintptr]struct{}),
		result: OutlineResult{RequestGeneration: v.outlineID, DisplayedGeneration: v.document}}
	job.frames = []outlineFrame{{into: &job.result.Entries, depth: 1}}
	w := v.worker
	w.mu.Lock()
	w.outline = job
	w.mu.Unlock()
	w.signal()
	return v.outlineID
}

func (v *Viewer) cancelOutlineRequest() {
	if v.cancelOutline != nil {
		v.cancelOutline()
		v.cancelOutline = nil
	}
}

// GoToOutline navigates to a local destination from the current viewer and
// document. It returns false if the entry cannot be used. Deferred navigation
// errors appear in State.Err.
func (v *Viewer) GoToOutline(entry OutlineEntry) bool {
	if v.closed || entry.owner != v || entry.document != v.document || entry.page < 0 || entry.page >= len(v.pages) {
		return false
	}
	v.navigateToWhenSized(entry.page, func() { v.goToDestination(entry.page, entry.target) })
	return true
}

func (v *Viewer) readOutlineStep(e *engine, doc *document, job *outlineJob) {
	if job.ctx.Err() == nil && doc != nil && doc.ID == job.result.DisplayedGeneration && v.advanceOutline(e, doc, job) {
		return
	}
	w := v.worker
	w.mu.Lock()
	if w.outline == job {
		w.outline = nil
	}
	w.mu.Unlock()
	if job.ctx.Err() != nil {
		return
	}
	v.deliver(job.ctx, func() bool {
		if job.ctx.Err() != nil || v.outlineID != job.result.RequestGeneration || v.document != job.result.DisplayedGeneration {
			return false
		}
		v.cancelOutlineRequest()
		job.apply(job.result)
		return true
	})
}

func (v *Viewer) advanceOutline(e *engine, doc *document, job *outlineJob) bool {
	frame := job.frames[len(job.frames)-1]
	job.frames = job.frames[:len(job.frames)-1]
	if _, seen := job.seen[frame.handle]; seen || len(job.seen) >= 10_000 {
		job.result.Truncated = true
		return len(job.frames) > 0 && len(job.seen) < 10_000
	}
	bookmark, handle, err := e.ReadBookmark(job.ctx, doc, frame.handle)
	if err != nil || handle == 0 {
		job.result.Err = err
		return false
	}
	job.seen[handle] = struct{}{}
	job.result.Truncated = job.result.Truncated || bookmark.TitleOmitted
	*frame.into = append(*frame.into, OutlineEntry{Title: bookmark.Title, Page: bookmark.Page + 1, owner: v,
		document: doc.ID, page: bookmark.Page, target: bookmark.Target})
	if bookmark.Next != 0 {
		job.frames = append(job.frames, outlineFrame{bookmark.Next, frame.into, frame.depth})
	}
	if bookmark.Child != 0 {
		if frame.depth == 64 {
			job.result.Truncated = true
		} else {
			entry := &(*frame.into)[len(*frame.into)-1]
			// The parent's next sibling is appended after its child frame is consumed.
			job.frames = append(job.frames, outlineFrame{bookmark.Child, &entry.Children, frame.depth + 1})
		}
	}
	return len(job.frames) > 0
}
