package pdfium

import (
	"fmt"
	"path/filepath"
	"runtime"
	"unsafe"

	"errors"
	"sync"
)

// The ABI follows the public headers for PDFium chromium/8179.
// An unsigned long is 64 bits on Unix LP64 and 32 bits on Windows LLP64.
type Engine struct {
	initLibrary     func()
	loadDocument    func(*byte, uintptr, *byte) uintptr
	loadCustom      func(*fileAccess, *byte) uintptr
	lastError       func() cLong
	closeDocument   func(uintptr)
	pageCount       func(uintptr) int32
	userPermissions func(uintptr) cLong
	pageSize        func(uintptr, int32, *PageSize) int32
	loadPage        func(uintptr, int32) uintptr
	closePage       func(uintptr)
	createBitmap    func(int32, int32, int32, unsafe.Pointer, int32) uintptr
	fillBitmap      func(uintptr, int32, int32, int32, int32, cLong) int32
	destroyBitmap   func(uintptr)
	renderBitmap    func(uintptr, uintptr, int32, int32, int32, int32, int32, int32)
	renderStart     func(uintptr, uintptr, int32, int32, int32, int32, int32, int32, *renderPause) int32
	renderContinue  func(uintptr, *renderPause) int32
	renderClose     func(uintptr)
	pageToDevice    func(uintptr, int32, int32, int32, int32, int32, float64, float64, *int32, *int32) int32
	loadText        func(uintptr) uintptr
	closeText       func(uintptr)
	charCount       func(uintptr) int32
	charBox         func(uintptr, int32, *float64, *float64, *float64, *float64) int32
	looseCharBox    func(uintptr, int32, *pdfRect) int32
	charAngle       func(uintptr, int32) float32
	pageRotation    func(uintptr) int32
	getText         func(uintptr, int32, int32, *uint16) int32
	nextLink        func(uintptr, *int32, *uintptr) int32
	linkRect        func(uintptr, *pdfRect) int32
	linkDest        func(uintptr, uintptr) uintptr
	linkAction      func(uintptr) uintptr
	actionType      func(uintptr) cLong
	actionDest      func(uintptr, uintptr) uintptr
	actionURI       func(uintptr, uintptr, *byte, cLong) cLong
	destPage        func(uintptr, uintptr) int32
	destView        func(uintptr, *cLong, *float32) cLong
	destLocation    func(uintptr, *int32, *int32, *int32, *float32, *float32, *float32) int32
	pageBounds      func(uintptr, *pdfRect) int32
	pageObjectCount func(uintptr) int32
	pageObject      func(uintptr, int32) uintptr
	objectBounds    func(uintptr, *float32, *float32, *float32, *float32) int32
	objectActive    func(uintptr, *int32) int32
	loadWebLinks    func(uintptr) uintptr
	closeWebLinks   func(uintptr)
	webLinkCount    func(uintptr) int32
	webLinkURL      func(uintptr, int32, *uint16, int32) int32
	webRectCount    func(uintptr, int32) int32
	webRect         func(uintptr, int32, int32, *float64, *float64, *float64, *float64) int32
	bookmarkFirst   func(uintptr, uintptr) uintptr
	bookmarkNext    func(uintptr, uintptr) uintptr
	bookmarkTitle   func(uintptr, *uint16, cLong) cLong
	bookmarkDest    func(uintptr, uintptr) uintptr
	bookmarkAction  func(uintptr) uintptr
	findStart       func(uintptr, *uint16, cLong, int32) uintptr
	findNext        func(uintptr) int32
	findPrev        func(uintptr) int32
	findIndex       func(uintptr) int32
	findCount       func(uintptr) int32
	findClose       func(uintptr)
	countTextRects  func(uintptr, int32, int32) int32
	getTextRect     func(uintptr, int32, *float64, *float64, *float64, *float64) int32
}

type library struct {
	symbol func(string, any) error
	close  func()
}

var (
	ErrUnsupported = errors.New("unsupported PDFium platform")
	ErrLibrary     = errors.New("PDFium library unavailable")
	mu             = &nativeGate{Cond: sync.Cond{L: new(sync.Mutex)}}
	engines        = make(map[string]*Engine)
)

func LibraryName() (string, error) {
	if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
		switch runtime.GOOS {
		case "darwin":
			return "libpdfium.dylib", nil
		case "linux":
			return "libpdfium.so", nil
		case "windows":
			return "pdfium.dll", nil
		}
	}
	return "", fmt.Errorf("%w: %s/%s", ErrUnsupported, runtime.GOOS, runtime.GOARCH)
}

func LoadEngine(path string) (*Engine, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: expected an absolute path", ErrLibrary)
	}
	mu.Lock()
	defer mu.Unlock()
	if e := engines[path]; e != nil {
		return e, nil
	}
	e, err := loadEngine(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLibrary, err)
	}
	engines[path] = e
	return e, nil
}

func loadEngine(path string) (*Engine, error) {
	lib, err := openLibrary(path)
	if err != nil {
		return nil, fmt.Errorf("load PDFium: %w", err)
	}
	e := new(Engine)
	for _, symbol := range e.symbols() {
		if err := lib.symbol(symbol.name, symbol.fn); err != nil {
			lib.close()
			return nil, fmt.Errorf("missing PDFium symbol %s: %w", symbol.name, err)
		}
	}
	for _, symbol := range []binding{
		{"FPDF_LoadCustomDocument", &e.loadCustom},
		{"FPDFBookmark_GetFirstChild", &e.bookmarkFirst},
		{"FPDFBookmark_GetNextSibling", &e.bookmarkNext},
		{"FPDFBookmark_GetTitle", &e.bookmarkTitle},
		{"FPDFBookmark_GetDest", &e.bookmarkDest},
		{"FPDFBookmark_GetAction", &e.bookmarkAction},
		{"FPDFText_FindStart", &e.findStart},
		{"FPDFText_FindNext", &e.findNext},
		{"FPDFText_FindPrev", &e.findPrev},
		{"FPDFText_GetSchResultIndex", &e.findIndex},
		{"FPDFText_GetSchCount", &e.findCount},
		{"FPDFText_FindClose", &e.findClose},
		{"FPDFText_CountRects", &e.countTextRects},
		{"FPDFText_GetRect", &e.getTextRect},
		{"FPDF_RenderPageBitmap_Start", &e.renderStart},
		{"FPDF_RenderPage_Continue", &e.renderContinue},
		{"FPDF_RenderPage_Close", &e.renderClose},
		{"FPDFPage_CountObjects", &e.pageObjectCount},
		{"FPDFPage_GetObject", &e.pageObject},
		{"FPDFPageObj_GetBounds", &e.objectBounds},
		{"FPDFPageObj_GetIsActive", &e.objectActive},
		{"FPDFText_GetLooseCharBox", &e.looseCharBox},
		{"FPDFText_GetCharAngle", &e.charAngle},
		{"FPDFPage_GetRotation", &e.pageRotation},
	} {
		_ = lib.symbol(symbol.name, symbol.fn)
	}
	e.initLibrary()
	return e, nil
}

type binding struct {
	name string
	fn   any
}

func (e *Engine) symbols() []binding {
	return []binding{
		{"FPDF_InitLibrary", &e.initLibrary},
		{"FPDF_LoadMemDocument64", &e.loadDocument},
		{"FPDF_GetLastError", &e.lastError},
		{"FPDF_CloseDocument", &e.closeDocument},
		{"FPDF_GetPageCount", &e.pageCount},
		{"FPDF_GetDocUserPermissions", &e.userPermissions},
		{"FPDF_GetPageSizeByIndexF", &e.pageSize},
		{"FPDF_LoadPage", &e.loadPage},
		{"FPDF_ClosePage", &e.closePage},
		{"FPDFBitmap_CreateEx", &e.createBitmap},
		{"FPDFBitmap_FillRect", &e.fillBitmap},
		{"FPDFBitmap_Destroy", &e.destroyBitmap},
		{"FPDF_RenderPageBitmap", &e.renderBitmap},
		{"FPDF_PageToDevice", &e.pageToDevice},
		{"FPDFText_LoadPage", &e.loadText},
		{"FPDFText_ClosePage", &e.closeText},
		{"FPDFText_CountChars", &e.charCount},
		{"FPDFText_GetCharBox", &e.charBox},
		{"FPDFText_GetText", &e.getText},
		{"FPDFLink_Enumerate", &e.nextLink},
		{"FPDFLink_GetAnnotRect", &e.linkRect},
		{"FPDFLink_GetDest", &e.linkDest},
		{"FPDFLink_GetAction", &e.linkAction},
		{"FPDFAction_GetType", &e.actionType},
		{"FPDFAction_GetDest", &e.actionDest},
		{"FPDFAction_GetURIPath", &e.actionURI},
		{"FPDFDest_GetDestPageIndex", &e.destPage},
		{"FPDFDest_GetView", &e.destView},
		{"FPDFDest_GetLocationInPage", &e.destLocation},
		{"FPDF_GetPageBoundingBox", &e.pageBounds},
		{"FPDFLink_LoadWebLinks", &e.loadWebLinks},
		{"FPDFLink_CloseWebLinks", &e.closeWebLinks},
		{"FPDFLink_CountWebLinks", &e.webLinkCount},
		{"FPDFLink_GetURL", &e.webLinkURL},
		{"FPDFLink_CountRects", &e.webRectCount},
		{"FPDFLink_GetRect", &e.webRect},
	}
}
