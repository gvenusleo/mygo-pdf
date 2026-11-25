package pdfium

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/gvenusleo/mygo-pdf/internal/render"
)

const (
	maxPageChars = 50_000
	maxPageLinks = 1024
	maxURL       = 8192
)

type pdfRect struct{ left, top, right, bottom float32 }

type Rect struct{ X, Y, W, H float32 }

type Point struct{ X, Y float32 }

func (r Rect) Contains(x, y float32) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

type Link struct {
	Bounds Rect
	Page   int
	Target Destination
	URL    string
	Label  string
}

func (link Link) Bytes() int { return 96 + len(link.URL) + len(link.Label) }

// SelectionBox holds the font bounds and inline axis used for highlighting.
type SelectionBox struct {
	Rect
	Vertical bool
}

type Geometry struct {
	Chars     []Rect
	Selection []SelectionBox
	Links     []Link
	Text      string
}

const selectionBoxBytes = 20

func (g *Geometry) textBytes() int {
	return 16*cap(g.Chars) + selectionBoxBytes*cap(g.Selection) + len(g.Text)
}

func (g *Geometry) Bytes() int {
	bytes := g.textBytes()
	for _, link := range g.Links {
		bytes += link.Bytes()
	}
	return bytes
}

func (e *Engine) ReadGeometry(ctx context.Context, doc *Document, index int) (geometry *Geometry, err error) {
	mu.Lock()
	defer mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	doc.startRead(ctx)
	defer func() {
		if readErr := doc.finishRead(); readErr != nil {
			geometry, err = nil, readErr
		}
	}()
	page := e.loadPage(doc.handle, int32(index))
	if page == 0 {
		return nil, fmt.Errorf("cannot read PDF page %d", index+1)
	}
	defer e.closePage(page)
	f := &Geometry{}
	f.Links = e.annotations(ctx, doc, page)
	text := e.loadText(page)
	if text != 0 {
		defer e.closeText(text)
		if doc.copy {
			f.Chars = e.characterBoxes(ctx, page, text)
		}
		f.Links = e.webLinks(ctx, page, text, f.Links)
		if doc.accessible {
			const semanticBytes = 8 << 10
			f.Text = e.readText(text, 0, min(semanticBytes, int(e.charCount(text))), semanticBytes)
			e.labelLinks(ctx, text, f)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	remaining := render.PageMetadata - f.textBytes()
	for i, link := range f.Links {
		if size := link.Bytes(); size <= remaining {
			remaining -= size
		} else {
			clear(f.Links[i:])
			f.Links = f.Links[:i]
			break
		}
	}
	if e.looseCharBox != nil && len(f.Chars) > 0 {
		// Use the spare metadata budget so extra bounds cannot displace hit boxes or links.
		f.Selection = e.selectionBoxes(ctx, page, text, f.Chars[:min(len(f.Chars), remaining/selectionBoxBytes)])
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f, nil
}

func (e *Engine) characterBoxes(ctx context.Context, page, text uintptr) []Rect {
	boxes := make([]Rect, max(0, min(int(e.charCount(text)), maxPageChars, render.PageMetadata/32)))
	for i := range boxes {
		if i%128 == 0 && ctx.Err() != nil {
			return nil
		}
		var left, right, bottom, top float64
		if e.charBox(text, int32(i), &left, &right, &bottom, &top) != 0 {
			boxes[i] = e.normalizedRect(page, left, top, right, bottom)
		}
	}
	return boxes
}

func (e *Engine) selectionBoxes(ctx context.Context, page, text uintptr, chars []Rect) []SelectionBox {
	boxes := make([]SelectionBox, len(chars))
	var rotation float64
	if e.pageRotation != nil {
		rotation = float64(e.pageRotation(page)) * math.Pi / 2
	}
	for i, tight := range chars {
		if i%128 == 0 && ctx.Err() != nil {
			return nil
		}
		boxes[i] = e.selectionBox(page, text, int32(i), tight, rotation)
	}
	return boxes
}

func (e *Engine) selectionBox(page, text uintptr, index int32, tight Rect, rotation float64) SelectionBox {
	var bounds pdfRect
	if e.looseCharBox(text, index, &bounds) != 0 {
		if loose := e.normalizedRect(page, float64(bounds.left), float64(bounds.top), float64(bounds.right), float64(bounds.bottom)); loose.W > 0 && loose.H > 0 {
			tight = loose
		}
	}
	angle := rotation
	if e.charAngle != nil {
		if value := e.charAngle(text, index); value >= 0 {
			angle += float64(value)
		}
	}
	return SelectionBox{Rect: tight, Vertical: math.Abs(math.Sin(angle)) > math.Abs(math.Cos(angle))}
}

func (e *Engine) normalizedRect(page uintptr, left, top, right, bottom float64) Rect {
	// Normalize coordinates independently of raster size and let PDFium handle cropping and page rotation.
	const extent = 1 << 16
	var x1, y1, x2, y2 int32
	if e.pageToDevice(page, 0, 0, extent, extent, 0, left, top, &x1, &y1) == 0 ||
		e.pageToDevice(page, 0, 0, extent, extent, 0, right, bottom, &x2, &y2) == 0 {
		return Rect{}
	}
	x, y := max(0, min(x1, x2)), max(0, min(y1, y2))
	rightEdge, bottomEdge := min(extent, max(x1, x2)), min(extent, max(y1, y2))
	return Rect{X: float32(x) / extent, Y: float32(y) / extent,
		W: float32(max(0, rightEdge-x)) / extent, H: float32(max(0, bottomEdge-y)) / extent}
}

func (e *Engine) annotations(ctx context.Context, doc *Document, page uintptr) []Link {
	var links []Link
	var cursor int32
	for range maxPageLinks {
		var annot uintptr
		if ctx.Err() != nil || e.nextLink(page, &cursor, &annot) == 0 {
			break
		}
		var rect pdfRect
		if e.linkRect(annot, &rect) == 0 {
			continue
		}
		link := e.annotationTarget(ctx, doc, annot)
		if link.Page < 0 && link.URL == "" {
			continue
		}
		link.Bounds = e.normalizedRect(page, float64(rect.left), float64(rect.top), float64(rect.right), float64(rect.bottom))
		links = append(links, link)
	}
	return links
}

const (
	actionGoTo = 1
	actionURI  = 3
)

func (e *Engine) annotationTarget(ctx context.Context, doc *Document, annot uintptr) Link {
	link := Link{Page: -1}
	var dest uintptr
	if action := e.linkAction(annot); action == 0 {
		dest = e.linkDest(doc.handle, annot)
	} else {
		switch e.actionType(action) {
		case actionGoTo:
			dest = e.actionDest(doc.handle, action)
		case actionURI:
			if n := e.actionURI(doc.handle, action, nil, 0); n > 1 && n <= maxURL {
				buf := make([]byte, n)
				e.actionURI(doc.handle, action, &buf[0], n)
				link.URL = allowedURL(trimCString(buf))
			}
		}
	}
	if dest != 0 {
		if page := int(e.destPage(doc.handle, dest)); page >= 0 && page < len(doc.Pages) {
			link.Page = page
			link.Target = e.destination(ctx, doc, page, dest)
		}
	}
	return link
}

func (e *Engine) webLinks(ctx context.Context, page, text uintptr, links []Link) []Link {
	web := e.loadWebLinks(text)
	if web == 0 {
		return links
	}
	defer e.closeWebLinks(web)
	for i := range min(e.webLinkCount(web), maxPageLinks) {
		if ctx.Err() != nil || len(links) >= maxPageLinks {
			break
		}
		n := e.webLinkURL(web, i, nil, 0)
		if n <= 1 || n > maxURL {
			continue
		}
		buf := make([]uint16, n)
		e.webLinkURL(web, i, &buf[0], n)
		address := allowedURL(utf16Text(buf))
		if address == "" {
			continue
		}
		for j := range min(e.webRectCount(web, i), int32(maxPageLinks-len(links))) {
			var left, top, right, bottom float64
			if e.webRect(web, i, j, &left, &top, &right, &bottom) != 0 {
				bounds := e.normalizedRect(page, left, top, right, bottom)
				links = append(links, Link{Bounds: bounds, Page: -1, URL: address})
			}
		}
	}
	return links
}

func allowedURL(address string) string {
	u, err := url.Parse(address)
	if err != nil || u.User != nil || !safeToOpen(address) {
		return ""
	}
	if !strings.EqualFold(u.Scheme, "mailto") && u.Hostname() == "" {
		return ""
	}
	return address
}

func safeToOpen(address string) bool {
	for i := 0; i < len(address); i++ {
		if c := address[i]; c < ' ' || c == 0x7f || c == '\\' {
			return false
		}
	}
	scheme, _, _ := strings.Cut(address, ":")
	switch strings.ToLower(scheme) {
	case "http", "https", "mailto":
		return true
	}
	return false
}

func utf16Text(units []uint16) string {
	runes := utf16.Decode(units)
	return strings.TrimRight(string(runes), "\x00")
}

type TextPoint struct{ Page, Index int }
type TextRange struct{ First, Last TextPoint }

func (e *Engine) CopyText(ctx context.Context, doc *Document, selection TextRange) (content string, err error) {
	mu.Lock()
	defer mu.Unlock()
	if !doc.copy {
		return "", nil
	}
	doc.startRead(ctx)
	defer func() {
		if readErr := doc.finishRead(); readErr != nil {
			content, err = "", readErr
		}
	}()
	var result strings.Builder
	for i := selection.First.Page; i <= selection.Last.Page && i < len(doc.Pages) && result.Len() < 1<<20; i++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		page := e.loadPage(doc.handle, int32(i))
		if page == 0 {
			continue
		}
		text := e.pageSelection(page, i, selection)
		e.closePage(page)
		if text != "" {
			if result.Len() != 0 {
				result.WriteByte('\n')
			}
			result.WriteString(text)
		}
	}
	return result.String(), ctx.Err()
}

func (e *Engine) pageSelection(page uintptr, index int, selection TextRange) string {
	text := e.loadText(page)
	if text == 0 {
		return ""
	}
	defer e.closeText(text)
	start, end := 0, min(int(e.charCount(text)), maxPageChars)
	if index == selection.First.Page {
		start = selection.First.Index
	}
	if index == selection.Last.Page {
		end = min(end, selection.Last.Index+1)
	}
	return e.readText(text, start, end-start, 0)
}

func (e *Engine) readText(text uintptr, start, count, maxBytes int) string {
	if count <= 0 {
		return ""
	}
	buf := make([]uint16, count+1)
	n := e.getText(text, int32(start), int32(count), &buf[0])
	value := utf16Text(buf[:max(0, min(int(n), len(buf)))])
	if maxBytes > 0 && len(value) > maxBytes {
		for !utf8.RuneStart(value[maxBytes]) {
			maxBytes--
		}
		value = strings.Clone(value[:maxBytes])
	}
	return value
}

func (e *Engine) labelLinks(ctx context.Context, text uintptr, data *Geometry) {
	// The extraction budget also bounds the work on densely annotated pages.
	const maxChecks = 128_000
	checks, remaining := 0, 8<<10
	for i := range data.Links {
		if remaining == 0 || checks >= maxChecks || ctx.Err() != nil {
			break
		}
		link := &data.Links[i]
		first, count := -1, 0
		for index, box := range data.Chars {
			checks++
			if box.W > 0 && box.H > 0 && link.Bounds.Contains(box.X+box.W/2, box.Y+box.H/2) {
				if first < 0 {
					first = index
				}
				count = index - first + 1
			}
			if count >= 128 || checks >= maxChecks {
				break
			}
		}
		if first >= 0 {
			link.Label = strings.TrimSpace(e.readText(text, first, count, min(remaining, 384)))
			remaining -= len(link.Label)
		}
	}
}
