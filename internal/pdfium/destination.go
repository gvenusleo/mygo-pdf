package pdfium

import (
	"context"
	"math"
)

type DestinationMode uint8

const (
	DestinationUnknown DestinationMode = iota
	DestinationXYZ
	DestinationFit
	DestinationFitH
	DestinationFitV
	DestinationFitR
	DestinationFitB
	DestinationFitBH
	DestinationFitBV
)

// Destination stores positions and bounds in normalized, rotated page coordinates.
type Destination struct {
	Mode       DestinationMode
	Area       Rect
	Position   Point
	HasX, HasY bool
	Scale      float32
}

func (e *Engine) destination(ctx context.Context, doc *Document, index int, dest uintptr) Destination {
	var count cLong
	var params [4]float32
	mode := e.destView(dest, &count, &params[0])
	if mode == 0 || mode > cLong(DestinationFitBV) {
		return Destination{}
	}
	result := Destination{Mode: DestinationMode(mode), Area: Rect{W: 1, H: 1}}
	if result.Mode == DestinationFit {
		return result
	}
	page := e.loadPage(doc.handle, int32(index))
	if page == 0 {
		return result
	}
	defer e.closePage(page)
	var bounds pdfRect
	if e.pageBounds(page, &bounds) == 0 {
		return result
	}
	var x, y float32
	var hasX, hasY, hasScale int32
	switch result.Mode {
	case DestinationXYZ:
		e.destLocation(dest, &hasX, &hasY, &hasScale, &x, &y, &result.Scale)
		if hasScale == 0 || !finite(result.Scale) || result.Scale <= 0 {
			result.Scale = 0
		}
	case DestinationFitR:
		if count == 4 && finite(params[0]) && finite(params[1]) && finite(params[2]) && finite(params[3]) &&
			params[0] < params[2] && params[1] < params[3] {
			if area := e.normalizedRect(page, float64(params[0]), float64(params[3]), float64(params[2]), float64(params[1])); area.W > 0 && area.H > 0 {
				result.Area = area
			}
		}
	case DestinationFitH, DestinationFitBH:
		if count > 0 {
			y, hasY = params[0], 1
		}
	case DestinationFitV, DestinationFitBV:
		if count > 0 {
			x, hasX = params[0], 1
		}
	}
	if result.Mode >= DestinationFitB {
		result.Area = e.contentArea(ctx, doc, index, page)
	}
	result.Position, result.HasX, result.HasY = e.destinationPosition(page, bounds, x, y, hasX != 0 && finite(x), hasY != 0 && finite(y))
	return result
}

func (e *Engine) destinationPosition(page uintptr, bounds pdfRect, x, y float32, hasX, hasY bool) (Point, bool, bool) {
	if !hasX && !hasY {
		return Point{}, false, false
	}
	// Crop and rotation can swap which screen axis a PDF coordinate affects.
	x, y = min(bounds.right, max(bounds.left, x)), min(bounds.top, max(bounds.bottom, y))
	if !hasX {
		x = bounds.left
	}
	if !hasY {
		y = bounds.top
	}
	const extent = 1 << 16
	var dx, dy, x1, y1, x2, y2 int32
	if e.pageToDevice(page, 0, 0, extent, extent, 0, float64(x), float64(y), &dx, &dy) == 0 ||
		e.pageToDevice(page, 0, 0, extent, extent, 0, float64(bounds.left), float64(bounds.top), &x1, &y1) == 0 ||
		e.pageToDevice(page, 0, 0, extent, extent, 0, float64(bounds.right), float64(bounds.top), &x2, &y2) == 0 {
		return Point{}, false, false
	}
	if x1 == x2 {
		hasX, hasY = hasY, hasX
	}
	return Point{X: float32(max(0, min(extent, dx))) / extent, Y: float32(max(0, min(extent, dy))) / extent}, hasX, hasY
}

func finite(value float32) bool {
	return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
}

func (e *Engine) contentArea(ctx context.Context, doc *Document, index int, page uintptr) Rect {
	if area, ok := doc.contentAreas[index]; ok {
		return area
	}
	area := e.measureContentArea(ctx, page)
	if len(doc.contentAreas) < 128 && ctx.Err() == nil {
		if doc.contentAreas == nil {
			doc.contentAreas = make(map[int]Rect)
		}
		doc.contentAreas[index] = area
	}
	return area
}

func (e *Engine) measureContentArea(ctx context.Context, page uintptr) Rect {
	fallback := Rect{W: 1, H: 1}
	if e.pageObjectCount == nil || e.pageObject == nil || e.objectBounds == nil {
		return fallback
	}
	count := e.pageObjectCount(page)
	if count <= 0 || count > 4096 {
		return fallback
	}
	left, bottom := float32(math.Inf(1)), float32(math.Inf(1))
	right, top := -left, -bottom
	for index := range count {
		if ctx.Err() != nil {
			return fallback
		}
		object := e.pageObject(page, index)
		if object == 0 {
			return fallback
		}
		if e.objectActive != nil {
			var active int32
			if e.objectActive(object, &active) != 0 && active == 0 {
				continue
			}
		}
		var l, b, r, t float32
		if e.objectBounds(object, &l, &b, &r, &t) == 0 || !finite(l) || !finite(b) || !finite(r) || !finite(t) {
			return fallback
		}
		left, bottom, right, top = min(left, l), min(bottom, b), max(right, r), max(top, t)
	}
	if !finite(left) || left >= right || bottom >= top {
		return fallback
	}
	area := e.normalizedRect(page, float64(left), float64(top), float64(right), float64(bottom))
	if area.W <= 0 || area.H <= 0 {
		return fallback
	}
	return area
}
