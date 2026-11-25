package pdfium

import (
	"context"
	"fmt"
)

type Match struct {
	Page, Index, Count int
	Boxes              []Rect
}

// FindPage returns at most one match and releases all page and search handles.
// Start is a character index, or -1 to search backward from the end.
func (e *Engine) FindPage(ctx context.Context, doc *Document, index int, query []uint16, start int, backward bool) (match *Match, err error) {
	if !e.hasFind() {
		return nil, fmt.Errorf("%w: PDFium find unavailable", ErrUnsupported)
	}
	mu.lock(ctx, true)
	defer mu.Unlock()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	doc.startRead(ctx)
	defer func() {
		if readErr := doc.finishRead(); readErr != nil {
			match, err = nil, readErr
		}
	}()
	page := e.loadPage(doc.handle, int32(index))
	if page == 0 {
		return nil, fmt.Errorf("cannot read PDF page %d", index+1)
	}
	defer e.closePage(page)
	text := e.loadText(page)
	if text == 0 {
		return nil, fmt.Errorf("cannot read text on PDF page %d", index+1)
	}
	defer e.closeText(text)
	charCount := int(e.charCount(text))
	if charCount <= 0 || !backward && start >= charCount {
		return nil, ctx.Err()
	}
	if backward && start >= charCount {
		start = -1
	}
	search := e.findStart(text, &query[0], 0, int32(start))
	if search == 0 {
		return nil, ctx.Err()
	}
	defer e.findClose(search)
	step := e.findNext
	if backward {
		step = e.findPrev
	}
	for ctx.Err() == nil && step(search) != 0 {
		first, count := e.findIndex(search), e.findCount(search)
		boxes := e.matchBoxes(ctx, page, text, first, count)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// PDFium searches the full page, including text outside the crop box.
		if len(boxes) > 0 {
			return &Match{Page: index, Index: int(first), Count: int(count), Boxes: boxes}, nil
		}
	}
	return nil, ctx.Err()
}

func (e *Engine) matchBoxes(ctx context.Context, page, text uintptr, first, count int32) []Rect {
	var boxes []Rect
	for i := range e.countTextRects(text, first, count) {
		if ctx.Err() != nil || len(boxes) == 256 {
			break
		}
		var left, top, right, bottom float64
		if e.getTextRect(text, i, &left, &top, &right, &bottom) != 0 {
			if r := e.normalizedRect(page, left, top, right, bottom); r.W > 0 && r.H > 0 {
				boxes = append(boxes, r)
			}
		}
	}
	return boxes
}

func (e *Engine) hasFind() bool {
	return e.findStart != nil && e.findNext != nil && e.findPrev != nil && e.findIndex != nil && e.findCount != nil &&
		e.findClose != nil && e.countTextRects != nil && e.getTextRect != nil
}
