//go:build (!darwin && !linux && !windows) || (!amd64 && !arm64)

package pdfium

import (
	"errors"
	"unsafe"
)

type cLong = uint64

func newPauseCallback(func(uintptr) uintptr) uintptr { return 0 }

func newFileCallback(func(uintptr, cLong, unsafe.Pointer, cLong) uintptr) uintptr { return 0 }

func openLibrary(string) (library, error) { return library{}, errors.ErrUnsupported }
