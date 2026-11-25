package pdfium

import (
	"context"
	"fmt"
)

type Bookmark struct {
	Title        string
	Page         int
	Target       Destination
	Child, Next  uintptr
	TitleOmitted bool
}

func (e *Engine) ReadBookmark(ctx context.Context, doc *Document, handle uintptr) (bookmark Bookmark, actual uintptr, err error) {
	if e.bookmarkFirst == nil || e.bookmarkNext == nil || e.bookmarkTitle == nil || e.bookmarkDest == nil || e.bookmarkAction == nil {
		return bookmark, 0, fmt.Errorf("%w: PDFium outline unavailable", ErrUnsupported)
	}
	mu.lock(ctx, true)
	defer mu.Unlock()
	if err := ctx.Err(); err != nil {
		return bookmark, 0, err
	}
	doc.startRead(ctx)
	defer func() {
		if readErr := doc.finishRead(); readErr != nil {
			err = readErr
		}
	}()
	if handle == 0 {
		handle = e.bookmarkFirst(doc.handle, 0)
		if handle == 0 {
			return bookmark, 0, nil
		}
	}
	bookmark.Page = -1
	bookmark.Child = e.bookmarkFirst(doc.handle, handle)
	bookmark.Next = e.bookmarkNext(doc.handle, handle)
	length := e.bookmarkTitle(handle, nil, 0)
	const maxTitleBytes = 2 * (4096 + 1)
	if length > maxTitleBytes || length%2 != 0 {
		bookmark.TitleOmitted = true
	} else if length > 0 {
		units := make([]uint16, length/2)
		if e.bookmarkTitle(handle, &units[0], length) == length {
			bookmark.Title = utf16Text(units)
		}
	}
	// PDFium's bookmark destination helper also resolves remote actions.
	var dest uintptr
	if action := e.bookmarkAction(handle); action == 0 {
		dest = e.bookmarkDest(doc.handle, handle)
	} else if e.actionType(action) == actionGoTo {
		dest = e.actionDest(doc.handle, action)
	}
	if dest != 0 {
		if page := int(e.destPage(doc.handle, dest)); page >= 0 && page < len(doc.Pages) {
			bookmark.Page = page
			bookmark.Target = e.destination(ctx, doc, page, dest)
		}
	}
	return bookmark, handle, ctx.Err()
}
