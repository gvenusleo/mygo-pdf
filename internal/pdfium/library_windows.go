//go:build amd64 || arm64

package pdfium

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
)

type cLong = uint32

func newPauseCallback(fn func(uintptr) uintptr) uintptr { return purego.NewCallback(fn) }

func newFileCallback(fn func(uintptr, cLong, unsafe.Pointer, cLong) uintptr) uintptr {
	return purego.NewCallback(fn)
}

func openLibrary(path string) (library, error) {
	handle, err := windows.LoadLibraryEx(path, 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		return library{}, err
	}
	lookup := func(name string) (uintptr, error) { return windows.GetProcAddress(handle, name) }
	return library{
		address: lookup,
		symbol: func(name string, fn any) error {
			address, err := lookup(name)
			if err == nil {
				purego.RegisterFunc(fn, address)
			}
			return err
		},
		close: func() { windows.FreeLibrary(handle) },
	}, nil
}

func (e *Engine) bindLoaders(lib library) error {
	loadBytes, err := lib.address("FPDF_LoadMemDocument64")
	if err != nil {
		return fmt.Errorf("missing PDFium symbol FPDF_LoadMemDocument64: %w", err)
	}
	// PDFium uses Win32 last-error state. SyscallN captures it before
	// returning to Go, where runtime calls can overwrite it on this thread.
	e.loadDocument = func(data *byte, size uintptr, password *byte) (uintptr, cLong) {
		handle, _, code := syscall.SyscallN(loadBytes, uintptr(unsafe.Pointer(data)), size, uintptr(unsafe.Pointer(password)))
		return handle, cLong(code)
	}
	if loadReader, err := lib.address("FPDF_LoadCustomDocument"); err == nil {
		e.loadCustom = func(access *fileAccess, password *byte) (uintptr, cLong) {
			handle, _, code := syscall.SyscallN(loadReader, uintptr(unsafe.Pointer(access)), uintptr(unsafe.Pointer(password)))
			return handle, cLong(code)
		}
	}
	return nil
}
