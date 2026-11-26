package pdf

import (
	"slices"

	"github.com/gvenusleo/mygo-pdf/internal/render"
)

func (l pageLayout) rasterKey(document uint64, page int, resolution render.PageResolution) rasterKey {
	rect := l.Page(page)
	return rasterKey{Document: document, Page: page, Rotation: l.Rotation, Size: resolution.Size(rect.W, rect.H)}
}

type pageRasterDemand struct {
	budget  int
	tileAll bool
}

func (v *Viewer) pageRasterDemand(first, last int) pageRasterDemand {
	resolution := render.FullPageResolution(v.scale)
	var fullBytes int
	for i := first; i <= last; i++ {
		rect := v.layout.Page(i)
		size := resolution.Size(rect.W, rect.H)
		fullBytes += size.X * size.Y * 4
	}
	budget := render.VisibleBitmapBudget
	return pageRasterDemand{budget: budget / (last - first + 1), tileAll: fullBytes > budget}
}

func (v *Viewer) visiblePageRaster(page int, demand pageRasterDemand) rasterKey {
	key := v.layout.rasterKey(v.document, page, render.FullPageResolution(v.scale))
	if !demand.tileAll && !v.needsTiles(page, key) {
		return key
	}
	if frame := v.cache[page].frame; frame != nil && frame.key.Document == v.document && frame.key.Rotation == v.rotation && frame.frameBytes() <= demand.budget {
		return frame.key
	}
	return v.layout.rasterKey(v.document, page, render.FallbackResolution(v.zoom, demand.budget))
}

func (v *Viewer) pageRasterError(page int, demand pageRasterDemand) error {
	if err := v.dimensionErrors[page]; err != nil {
		return err
	}
	entry := v.cache[page]
	if entry.err != nil && entry.failed == v.visiblePageRaster(page, demand) {
		return entry.err
	}
	return nil
}

func (v *Viewer) requestVisible(first, last int) pageRasterDemand {
	if v.scroll.Y != v.lastScrollY {
		v.scrollingUp = v.scroll.Y < v.lastScrollY
		v.lastScrollY = v.scroll.Y
	}
	next := v.prefetchPage(first, last)
	v.requestDimensions(first, last, next)
	demand := v.pageRasterDemand(first, last)
	var requests, refinements []rasterKey
	for i := first; i <= last; i++ {
		if !v.layout.Geometry.Exact(i) {
			continue
		}
		key := v.visiblePageRaster(i, demand)
		if v.cache[i].frame == nil {
			requests = v.requestPage(requests, key)
		} else {
			refinements = v.requestPage(refinements, key)
		}
	}
	missing := len(requests)
	requests = append(requests, v.requestTiles(first, last, demand.tileAll)...)
	requests = append(requests, refinements...)
	if next >= 0 && next < len(v.pages) && v.layout.Geometry.Exact(next) && v.cache[next].frame == nil {
		requests = v.requestPage(requests, v.layout.rasterKey(v.document, next, render.PreviewResolution(v.zoom)))
	}
	v.setRasterDemand(requests, next, missing, v.metadataRequests(first, last))
	return demand
}

func (v *Viewer) prefetchPage(first, last int) int {
	if v.scrollingUp {
		return first - 1
	}
	return last + 1
}

func (v *Viewer) requestPage(keys []rasterKey, key rasterKey) []rasterKey {
	entry := v.cache[key.Page]
	if entry.frame != nil {
		v.recency.MoveToFront(entry.recency)
		if entry.frame.key == key {
			return keys
		}
	}
	if entry.failed == key {
		return keys
	}
	return append(keys, key)
}

func (v *Viewer) setRasterDemand(keys []rasterKey, prefetchPage, missing int, metadata []geometryKey) {
	var prefetch rasterKey
	if len(keys) > 0 && keys[len(keys)-1].Page == prefetchPage {
		prefetch = keys[len(keys)-1]
	}
	if !slices.Equal(keys, v.wanted) || v.prefetch != prefetch ||
		v.missingBitmaps != missing || !slices.Equal(metadata, v.metadataWanted) {
		v.wanted, v.prefetch, v.missingBitmaps, v.metadataWanted = keys, prefetch, missing, metadata
		v.worker.submitPages(keys, prefetch, missing, metadata)
	}
}
