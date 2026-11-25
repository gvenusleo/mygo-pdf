package pdfium

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"runtime"
	"strings"
	"unsafe"

	"github.com/gvenusleo/mygo-pdf/internal/render"
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
	ID           uint64
	handle       uintptr
	pin          runtime.Pinner
	Pages        []PageSize
	copy         bool
	accessible   bool
	source       *fileSource
	contentAreas map[int]Rect
}

func (e *Engine) Open(ctx context.Context, id uint64, data []byte) (*Document, error) {
	return e.OpenBytes(ctx, id, data, "")
}

func (e *Engine) OpenBytes(ctx context.Context, id uint64, data []byte, password string) (*Document, error) {
	if len(data) == 0 {
		return nil, ErrInvalidPDF
	}
	if len(data) > MaxPDFBytes {
		return nil, ErrLimit
	}
	return e.open(ctx, &Document{ID: id}, password, func(doc *Document, pass *byte) uintptr {
		// PDFium may read this Go memory until the document closes.
		doc.pin.Pin(&data[0])
		return e.loadDocument(&data[0], uintptr(len(data)), pass)
	})
}

func (e *Engine) OpenReader(ctx context.Context, id uint64, reader io.ReaderAt, size int64, password string, release func()) (*Document, error) {
	if e.loadCustom == nil {
		return nil, fmt.Errorf("%w: PDFium reader loading unavailable", ErrUnsupported)
	}
	if reader == nil || size <= 0 {
		return nil, ErrInvalidPDF
	}
	if size > MaxPDFBytes {
		return nil, ErrLimit
	}
	source := &fileSource{reader: reader, size: size, release: release}
	return e.open(ctx, &Document{ID: id, source: source}, password, func(doc *Document, pass *byte) uintptr {
		fileToken++
		source.token = fileToken
		fileSources[fileToken] = source
		access := fileAccess{length: cLong(size), read: fileReader(), token: fileToken}
		return e.loadCustom(&access, pass)
	})
}

func (e *Engine) open(ctx context.Context, doc *Document, password string, load func(*Document, *byte) uintptr) (*Document, error) {
	if strings.ContainsRune(password, 0) {
		return nil, fmt.Errorf("%w: password contains NUL", ErrInvalidPDF)
	}
	mu.Lock()
	defer mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	doc.startRead(ctx)
	var pass *byte
	if password != "" {
		buf := make([]byte, len(password)+1)
		copy(buf, password)
		defer clear(buf)
		pass = &buf[0]
	}
	doc.handle = load(doc, pass)
	var err error
	if doc.handle == 0 {
		code := e.lastError()
		err = fmt.Errorf("%w (PDFium error %d)", ErrInvalidPDF, code)
		if code == 4 {
			err = ErrPassword
		}
	} else {
		err = e.readPages(ctx, doc)
		permissions := e.userPermissions(doc.handle)
		doc.copy, doc.accessible = permissions&16 != 0, permissions&(1<<9) != 0
	}
	if readErr := doc.finishRead(); readErr != nil {
		err = readErr
	}
	if err != nil {
		e.close(doc)
		return nil, err
	}
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
	if doc.source != nil {
		return nil
	}
	for i := range doc.Pages {
		if err := ctx.Err(); err != nil {
			return err
		}
		size, err := e.readSize(doc, i)
		if err != nil {
			return err
		}
		doc.Pages[i] = size
	}
	return nil
}

func (e *Engine) readSize(doc *Document, index int) (size PageSize, err error) {
	if e.pageSize(doc.handle, int32(index), &size) == 0 || !validSize(size.Width) || !validSize(size.Height) {
		return PageSize{}, fmt.Errorf("%w: cannot read dimensions of page %d", ErrInvalidPDF, index+1)
	}
	return size, nil
}

func (e *Engine) ReadPageSize(ctx context.Context, doc *Document, index int, background bool) (size PageSize, err error) {
	mu.lock(ctx, background)
	defer mu.Unlock()
	if err = ctx.Err(); err != nil {
		return size, err
	}
	doc.startRead(ctx)
	size, err = e.readSize(doc, index)
	if readErr := doc.finishRead(); readErr != nil {
		return PageSize{}, readErr
	}
	return size, err
}

func validSize(size float32) bool {
	return size >= 0.01 && size <= 1_000_000
}

func (e *Engine) close(doc *Document) {
	if doc != nil {
		if doc.handle != 0 {
			e.closeDocument(doc.handle)
		}
		doc.pin.Unpin()
		if doc.source != nil {
			delete(fileSources, doc.source.token)
			doc.source.release()
			doc.source = nil
		}
	}
}

func (e *Engine) Close(doc *Document) {
	if doc != nil {
		mu.Lock()
		defer mu.Unlock()
		e.close(doc)
	}
}

func (e *Engine) drawPage(ctx context.Context, page uintptr, key render.Key, background bool) (*image.RGBA, error) {
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
	x, y := -int32(region.Min.X), -int32(region.Min.Y)
	fullWidth, fullHeight := int32(key.Size.X), int32(key.Size.Y)
	if e.HasProgressiveRendering() {
		if err := e.drawProgressive(ctx, bitmap, page, x, y, fullWidth, fullHeight, int32(key.Rotation), annotations|reverseByteOrder, background); err != nil {
			return nil, err
		}
	} else {
		e.renderBitmap(bitmap, page, x, y, fullWidth, fullHeight, int32(key.Rotation), annotations|reverseByteOrder)
	}
	return img, nil
}

func (e *Engine) Render(ctx context.Context, doc *Document, key render.Key, background bool) (pixels *image.RGBA, err error) {
	if !acquireRenderSlot(ctx, background) {
		return nil, ctx.Err()
	}
	defer releaseRenderSlot(background)
	mu.lock(ctx, background)
	defer mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	doc.startRead(ctx)
	defer func() {
		if readErr := doc.finishRead(); readErr != nil {
			pixels, err = nil, readErr
		}
	}()
	page := e.loadPage(doc.handle, int32(key.Page))
	if page == 0 {
		return nil, fmt.Errorf("cannot read PDF page %d", key.Page+1)
	}
	defer e.closePage(page)
	img, err := e.drawPage(ctx, page, key, background)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return img, nil
}

func trimCString(data []byte) string {
	text, _, _ := bytes.Cut(data, []byte{0})
	return string(text)
}
