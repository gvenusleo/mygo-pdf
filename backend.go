package pdf

import (
	"fmt"
	"image"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/gvenusleo/mygo-pdf/internal/pdfium"
	"github.com/gvenusleo/mygo-pdf/internal/render"
)

// MaxPDFBytes is the maximum size of a PDF passed to LoadBytes.
const MaxPDFBytes = pdfium.MaxPDFBytes

var (
	ErrUnsupported = pdfium.ErrUnsupported
	ErrLibrary     = pdfium.ErrLibrary
	ErrInvalidPDF  = pdfium.ErrInvalidPDF
	ErrPassword    = pdfium.ErrPassword
	ErrLimit       = pdfium.ErrLimit
)

type (
	engine    = pdfium.Engine
	document  = pdfium.Document
	pageSize  = pdfium.PageSize
	pageLink  = pdfium.Link
	textPoint = pdfium.TextPoint
	textRange = pdfium.TextRange
	rasterKey = render.Key
)

type pageFrame struct {
	key    rasterKey
	bitmap *ui.Bitmap
	pixels *image.RGBA
	chars  []pdfium.Rect
	links  []pageLink
}

func resourceEngine(path string) (*engine, error) {
	name, err := pdfium.LibraryName()
	if err != nil {
		return nil, err
	}
	if path == "" {
		dir, err := mygo.App.Path(mygo.PathResources)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrLibrary, err)
		}
		path = filepath.Join(dir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// Older packages stored the library under a dedicated subdirectory.
			fallback := filepath.Join(dir, "pdfium", name)
			if _, err := os.Stat(fallback); err == nil {
				path = fallback
			}
		}
	}
	return pdfium.LoadEngine(path)
}
