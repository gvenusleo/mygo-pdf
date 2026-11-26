package pdf

import (
	"container/list"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/gvenusleo/mygo-pdf/internal/render"
)

type cachedPage struct {
	frame   *pageFrame
	failed  rasterKey
	err     error
	recency *list.Element
}

func (v *Viewer) clearTiles() {
	clear(v.tiles)
	v.tileKeys = nil
	v.tileBytes = 0
}

func (v *Viewer) applyRasterResult(key rasterKey, frame *pageFrame, err error) bool {
	if v.document != key.Document || key.Rotation != v.rotation || !slices.Contains(v.wanted, key) {
		return false
	}
	if !key.Tile.Empty() {
		entry := v.tiles[key]
		if err == nil {
			frame.installBitmap(entry.frame)
			v.tiles[key] = cachedPage{frame: frame}
			if entry.frame == nil {
				v.tileBytes += frame.frameBytes()
			}
			v.trimTiles()
		} else {
			entry.failed, entry.err = key, err
			v.tiles[key] = entry
		}
		return true
	}
	if err == nil {
		v.cachePage(frame)
		for v.cacheSize > render.BitmapBudget {
			v.removePage(v.recency.Back().Value.(int))
		}
	} else {
		entry := v.cache[key.Page]
		entry.failed, entry.err = key, err
		v.cache[key.Page] = entry
	}
	return true
}

func (v *Viewer) cachePage(frame *pageFrame) {
	page := frame.key.Page
	frame.installBitmap(v.cache[page].frame)
	v.removePage(page)
	v.cache[page] = cachedPage{frame: frame, recency: v.recency.PushFront(page)}
	v.cacheSize += frame.frameBytes()
}

func (v *Viewer) removePage(page int) {
	entry := v.cache[page]
	delete(v.cache, page)
	if entry.frame != nil {
		v.cacheSize -= entry.frame.frameBytes()
		v.recency.Remove(entry.recency)
	}
}

func (f *pageFrame) installBitmap(old *pageFrame) {
	if old != nil && old.key.Region().Size() == f.key.Region().Size() {
		// Reuse the texture so a PDF refresh only has to upload new pixels.
		old.bitmap.Update(f.pixels)
		f.bitmap = old.bitmap
	} else {
		f.bitmap = ui.NewBitmap(f.pixels)
	}
	f.pixels = nil
}

func (f *pageFrame) frameBytes() int {
	region := f.key.Region()
	return region.Dx() * region.Dy() * 4
}
