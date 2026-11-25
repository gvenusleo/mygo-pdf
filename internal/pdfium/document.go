package pdfium

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/gvenusleo/mygo-pdf/internal/render"
	"image"
	"runtime"
	"unsafe"
)

// MaxPDFBytes is the maximum input size for a single PDF.
const MaxPDFBytes = 256 << 20

const maxPages = 100_000

var (
	ErrInvalidPDF = errors.New("invalid PDF")
	ErrPassword   = errors.New("PDF requires a password")
	ErrLimit      = errors.New("PDF exceeds resource limits")
)

type PageSize struct{ Width, Height float32 }

type Document struct {
	ID     uint64
	handle uintptr
	pin    runtime.Pinner
	Pages  []PageSize
	copy   bool
}

func (e *Engine) Open(ctx context.Context, id uint64, data []byte) (*Document, error) {
	if len(data) == 0 {
		return nil, ErrInvalidPDF
	}
	if len(data) > MaxPDFBytes {
		return nil, ErrLimit
	}
	mu.Lock()
	defer mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	doc := &Document{ID: id}
	// PDFium may read this Go memory until the document closes.
	doc.pin.Pin(&data[0])
	doc.handle = e.loadDocument(&data[0], uintptr(len(data)), nil)
	if doc.handle == 0 {
		doc.pin.Unpin()
		code := e.lastError()
		if code == 4 {
			return nil, ErrPassword
		}
		return nil, fmt.Errorf("%w (PDFium error %d)", ErrInvalidPDF, code)
	}
	if err := e.readPages(ctx, doc); err != nil {
		e.close(doc)
		return nil, err
	}
	doc.copy = e.userPermissions(doc.handle)&16 != 0
	return doc, nil
}

func (e *Engine) readPages(ctx context.Context, doc *Document) error {
	count := e.pageCount(doc.handle)
	if count <= 0 {
		return fmt.Errorf("%w: no pages", ErrInvalidPDF)
	}
	if count > maxPages {
		return fmt.Errorf("%w: %d pages", ErrLimit, count)
	}
	doc.Pages = make([]PageSize, count)
	for i := range doc.Pages {
		if err := ctx.Err(); err != nil {
			return err
		}
		size := &doc.Pages[i]
		if e.pageSize(doc.handle, int32(i), size) == 0 || !validSize(size.Width) || !validSize(size.Height) {
			return fmt.Errorf("%w: cannot read dimensions of page %d", ErrInvalidPDF, i+1)
		}
	}
	return nil
}

func validSize(size float32) bool {
	return size >= 0.01 && size <= 1_000_000
}

func (e *Engine) close(doc *Document) {
	if doc != nil {
		e.closeDocument(doc.handle)
		doc.pin.Unpin()
	}
}

func (e *Engine) Close(doc *Document) {
	if doc != nil {
		mu.Lock()
		defer mu.Unlock()
		e.close(doc)
	}
}

func (e *Engine) drawPage(page uintptr, key render.Key) (*image.RGBA, error) {
	region := key.Region()
	img := image.NewRGBA(image.Rectangle{Max: region.Size()})
	width, height := int32(region.Dx()), int32(region.Dy())
	const bgra = 4
	var pixels runtime.Pinner
	pixels.Pin(&img.Pix[0])
	defer pixels.Unpin()
	bitmap := e.createBitmap(width, height, bgra, unsafe.Pointer(&img.Pix[0]), int32(img.Stride))
	if bitmap == 0 {
		return nil, fmt.Errorf("cannot allocate a PDF bitmap")
	}
	defer e.destroyBitmap(bitmap)
	if e.fillBitmap(bitmap, 0, 0, width, height, 0xffffffff) == 0 {
		return nil, fmt.Errorf("cannot fill a PDF bitmap")
	}
	const annotations = 1
	const reverseByteOrder = 0x10
	e.renderBitmap(bitmap, page, -int32(region.Min.X), -int32(region.Min.Y), int32(key.Size.X), int32(key.Size.Y), 0, annotations|reverseByteOrder)
	return img, nil
}

func (e *Engine) Render(ctx context.Context, doc *Document, key render.Key) (*Frame, error) {
	mu.Lock()
	defer mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	page := e.loadPage(doc.handle, int32(key.Page))
	if page == 0 {
		return nil, fmt.Errorf("cannot read PDF page %d", key.Page+1)
	}
	defer e.closePage(page)
	img, err := e.drawPage(page, key)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !key.Tile.Empty() {
		return &Frame{Pixels: img}, nil
	}
	return e.frame(ctx, doc, page, key, img)
}

func trimCString(data []byte) string {
	text, _, _ := bytes.Cut(data, []byte{0})
	return string(text)
}
