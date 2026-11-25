//go:build !windows || (!amd64 && !arm64)

package pdfium

import "fmt"

func (e *Engine) bindLoaders(lib library) error {
	var loadBytes func(*byte, uintptr, *byte) uintptr
	var loadReader func(*fileAccess, *byte) uintptr
	var lastError func() cLong
	for _, binding := range []binding{
		{"FPDF_LoadMemDocument64", &loadBytes},
		{"FPDF_GetLastError", &lastError},
	} {
		if err := lib.symbol(binding.name, binding.fn); err != nil {
			return fmt.Errorf("missing PDFium symbol %s: %w", binding.name, err)
		}
	}
	if lib.symbol("FPDF_LoadCustomDocument", &loadReader) == nil {
		e.loadCustom = func(access *fileAccess, password *byte) (uintptr, cLong) {
			handle := loadReader(access, password)
			if handle == 0 {
				return 0, lastError()
			}
			return handle, 0
		}
	}
	e.loadDocument = func(data *byte, size uintptr, password *byte) (uintptr, cLong) {
		handle := loadBytes(data, size, password)
		if handle == 0 {
			return 0, lastError()
		}
		return handle, 0
	}
	return nil
}
