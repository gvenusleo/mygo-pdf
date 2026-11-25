//go:build (!darwin && !linux && !windows) || (!amd64 && !arm64)

package pdfium

import "errors"

type cLong = uint64

func newPauseCallback(func(uintptr) uintptr) uintptr { return 0 }

func openLibrary(string) (library, error) { return library{}, errors.ErrUnsupported }
