package pdf

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/gvenusleo/mygo-pdf/internal/pdfium"
)

// FindDirection controls which match follows the current location.
type FindDirection uint8

const (
	FindForward FindDirection = iota
	FindBackward
)

// FindState describes the active find request. Page is one-based, or zero
// when there is no active match.
type FindState struct {
	RequestGeneration uint64
	Searching         bool
	Page              int
	Err               error
}

type findJob struct {
	ctx            context.Context
	id, document   uint64
	query          []uint16
	backward       bool
	first, visited int
	previous       *pdfium.Match
}

// Find schedules one case-insensitive match, wrapping once. Repeating a query
// starts after or before its current match. An empty query clears the result.
// It returns zero for an empty query or an unavailable document or viewer.
func (v *Viewer) Find(ctx context.Context, query string, direction FindDirection) uint64 {
	if v.closed || v.cancelLoad != nil || v.document == 0 || ctx.Err() != nil {
		return 0
	}
	if query == "" {
		v.ClearFind()
		return 0
	}
	if v.cancelFind != nil {
		v.cancelFind()
		v.cancelFind = nil
	}
	v.pendingNavigation = nil
	v.findID++
	id := v.findID
	first, previous := v.currentPage(), v.findMatch
	if query != v.findQuery {
		previous = nil
	} else if previous != nil {
		first = previous.Page
	}
	v.findState, v.findQuery, v.findMatch = FindState{RequestGeneration: id}, "", nil
	if invalidFind(query, direction) {
		v.findState.Err = fmt.Errorf("invalid find query or direction")
		v.redraw()
		return id
	}
	ctx, v.cancelFind = context.WithCancel(ctx)
	v.findState.Searching, v.findQuery = true, query
	job := &findJob{ctx: ctx, id: id, document: v.document, query: append(utf16.Encode([]rune(query)), 0),
		backward: direction == FindBackward, first: first, previous: previous}
	w := v.worker
	w.mu.Lock()
	w.find = job
	w.mu.Unlock()
	w.signal()
	v.redraw()
	return id
}

func invalidFind(query string, direction FindDirection) bool {
	if len(query) > 256*3 || strings.TrimSpace(query) == "" || strings.ContainsRune(query, 0) || direction > FindBackward {
		return true
	}
	units := 0
	for _, r := range query {
		units += utf16.RuneLen(r)
	}
	return units > 256
}

// ClearFind cancels find and removes its active highlight.
func (v *Viewer) ClearFind() {
	v.clearFind()
	v.redraw()
}

func (v *Viewer) clearFind() {
	if v.cancelFind != nil {
		v.cancelFind()
		v.cancelFind = nil
	}
	if v.pendingNavigation != nil && v.pendingNavigation.find {
		v.pendingNavigation = nil
	}
	v.findState, v.findQuery, v.findMatch = FindState{}, "", nil
}

func (v *Viewer) findStep(e *engine, doc *document, job *findJob) {
	var match *pdfium.Match
	err := job.ctx.Err()
	if err == nil && doc != nil && doc.ID == job.document {
		var more bool
		match, more, err = job.advance(e, doc)
		if more {
			return
		}
	}
	w := v.worker
	w.mu.Lock()
	if w.find == job {
		w.find = nil
	}
	w.mu.Unlock()
	v.publish(func() {
		if !v.closed {
			v.finishFind(job, match, err)
		}
	})
}

func (job *findJob) advance(e *engine, doc *document) (*pdfium.Match, bool, error) {
	count := len(doc.Pages)
	start, page := 0, (job.first+job.visited)%count
	if job.backward {
		start, page = -1, (job.first+count-job.visited)%count
	}
	job.visited++
	if job.visited == 1 && job.previous != nil {
		start = job.previous.Index + job.previous.Count
		if job.backward {
			if job.previous.Index == 0 {
				return nil, true, nil
			}
			start = job.previous.Index - 1
		}
	}
	match, err := e.FindPage(job.ctx, doc, page, job.query, start, job.backward)
	if job.visited == count+1 && match == nil && err == nil {
		match = job.previous
	}
	more := match == nil && err == nil && (job.visited < count || job.visited == count && job.previous != nil)
	return match, more, err
}

func (v *Viewer) finishFind(job *findJob, match *pdfium.Match, err error) bool {
	if job.id != v.findState.RequestGeneration || job.document != v.document {
		return false
	}
	if canceled := job.ctx.Err(); canceled != nil {
		match, err = nil, canceled
	}
	v.cancelFind()
	v.cancelFind = nil
	v.findState.Searching, v.findState.Err = false, err
	if match == nil {
		v.redraw()
		return true
	}
	v.findMatch, v.findState.Page = match, match.Page+1
	v.navigateWhenSized(match.Page, func(sizeErr error) {
		if sizeErr != nil {
			v.findMatch, v.findState.Page, v.findState.Err = nil, 0, sizeErr
			return
		}
		v.revealMatch(match)
	})
	if v.pendingNavigation != nil {
		v.pendingNavigation.find = true
	}
	return true
}

func (v *Viewer) revealMatch(match *pdfium.Match) {
	page := v.layout.page(match.Page)
	y := page.Y
	if len(match.Boxes) > 0 {
		box := rotatedRect(match.Boxes[0], v.rotation)
		y += min(page.H, (box.Y+box.H/2)*page.H) - v.height/2
		x := page.X + (box.X+box.W/2)*page.W - v.width/2
		v.scroll.X = max(0, min(x, v.layout.Width-v.width))
	}
	v.navigateTo(match.Page+1, y)
}
