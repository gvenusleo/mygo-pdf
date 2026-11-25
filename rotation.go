package pdf

import "github.com/gvenusleo/mygo-pdf/internal/pdfium"

// SetRotation sets the document's view rotation in clockwise quarter turns.
// Rotation is relative to the intrinsic PDF orientation and wraps at four turns.
func (v *Viewer) SetRotation(quarterTurns int) {
	rotation := quarterTurns & 3
	if v.closed || len(v.pages) == 0 || rotation == v.rotation {
		return
	}
	anchor, selected := v.centerAnchor(), v.selectedPage()
	v.rotation = rotation
	v.layout = v.layout.geometry.fit(v.loadViewport())
	v.restoreCenter(anchor)
	v.navigation = pageNavigation{selected, v.scroll.Y}
	if nav := v.pendingNavigation; nav != nil {
		nav.x, nav.y = v.scroll.X, v.scroll.Y
	}
	v.pointer = pointerDrag{}
	v.clearTiles()
	v.setRasterDemand(nil, -1, 0, nil)
	v.redraw()
}

func rotatedPoint(p pdfium.Point, turns int) pdfium.Point {
	switch turns & 3 {
	case 1:
		return pdfium.Point{X: 1 - p.Y, Y: p.X}
	case 2:
		return pdfium.Point{X: 1 - p.X, Y: 1 - p.Y}
	case 3:
		return pdfium.Point{X: p.Y, Y: 1 - p.X}
	default:
		return p
	}
}

func rotatedRect(r pdfium.Rect, turns int) pdfium.Rect {
	a := rotatedPoint(pdfium.Point{X: r.X, Y: r.Y}, turns)
	b := rotatedPoint(pdfium.Point{X: r.X + r.W, Y: r.Y + r.H}, turns)
	return pdfium.Rect{X: min(a.X, b.X), Y: min(a.Y, b.Y), W: max(a.X, b.X) - min(a.X, b.X), H: max(a.Y, b.Y) - min(a.Y, b.Y)}
}

func rotatedDestination(dest pdfium.Destination, turns int) pdfium.Destination {
	if turns == 0 {
		return dest
	}
	dest.Area = rotatedRect(dest.Area, turns)
	dest.Position = rotatedPoint(dest.Position, turns)
	if turns%2 != 0 {
		dest.HasX, dest.HasY = dest.HasY, dest.HasX
		switch dest.Mode {
		case pdfium.DestinationFitH:
			dest.Mode = pdfium.DestinationFitV
		case pdfium.DestinationFitV:
			dest.Mode = pdfium.DestinationFitH
		case pdfium.DestinationFitBH:
			dest.Mode = pdfium.DestinationFitBV
		case pdfium.DestinationFitBV:
			dest.Mode = pdfium.DestinationFitBH
		}
	}
	return dest
}
