package pdf

import (
	"slices"

	"github.com/gvenusleo/mygo-pdf/internal/render"
)

type geometryKey struct {
	document uint64
	page     int
}

type cachedGeometry struct {
	data  *geometry
	bytes int
	err   error
}

const maxCachedGeometryPages = 128

func (v *Viewer) metadataRequests(first, last int) []geometryKey {
	// Reserve one page's maximum size for each queued read. Small entries free
	// up room in later passes, and a crowded viewport cannot evict its own data.
	last = min(last, first+maxCachedGeometryPages-1)
	remaining := render.MetadataBudget
	for page := first; page <= last; page++ {
		remaining -= v.metadata[page].bytes
	}
	var keys []geometryKey
	for page := first; page <= last && remaining >= render.PageMetadata; page++ {
		if _, ok := v.metadata[page]; !ok && v.layout.geometry.exact(page) {
			remaining -= render.PageMetadata
			keys = append(keys, geometryKey{v.document, page})
		}
	}
	return keys
}

func (v *Viewer) resetMetadata() {
	clear(v.metadata)
	v.metadataOrder, v.metadataWanted, v.metadataBytes = nil, nil, 0
}

func (v *Viewer) finishMetadata(key geometryKey, data *geometry, err error) bool {
	if key.document != v.document || !slices.Contains(v.metadataWanted, key) {
		return false
	}
	first, last := v.visiblePages()
	if key.page < first || key.page > last {
		return false
	}
	if _, exists := v.metadata[key.page]; exists {
		return true
	}
	if v.metadata == nil {
		v.metadata = make(map[int]cachedGeometry)
	}
	bytes := 0
	if data != nil {
		bytes = data.Bytes()
	}
	v.metadata[key.page] = cachedGeometry{data: data, bytes: bytes, err: err}
	v.metadataOrder = append(v.metadataOrder, key.page)
	v.metadataBytes += bytes
	last = min(last, first+maxCachedGeometryPages-1)
	for v.metadataBytes > render.MetadataBudget || len(v.metadataOrder) > maxCachedGeometryPages {
		index := slices.IndexFunc(v.metadataOrder, func(page int) bool { return page < first || page > last })
		if index < 0 {
			// An input event can move the viewport before its demand is rebuilt.
			index = len(v.metadataOrder) - 1
		}
		page := v.metadataOrder[index]
		v.metadataBytes -= v.metadata[page].bytes
		delete(v.metadata, page)
		v.metadataOrder = slices.Delete(v.metadataOrder, index, index+1)
	}
	return true
}
