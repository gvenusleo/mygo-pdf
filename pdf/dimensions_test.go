package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"testing"
)

func TestDistantPageSizeAndRefreshAnchor(t *testing.T) {
	v, view, updates := newTestView(t, 900, 700)
	data := longMixedPDF()
	id, released := v.LoadReaderAt(t.Context(), bytes.NewReader(data), int64(len(data)), LoadOptions{})
	await(t, updates, view, func() bool { return v.document == id })
	if !v.GoToPage(6_000) || v.pendingNavigation == nil {
		t.Fatal("expected navigation to wait for page size")
	}
	v.finishSize(geometryKey{id, 5_999}, pageSize{}, io.ErrUnexpectedEOF)
	if state := v.State(); state.Page != 1 || !errors.Is(state.Err, io.ErrUnexpectedEOF) {
		t.Fatalf("failed jump was not reported: %+v", state)
	}
	v.GoToPage(9_999)
	view.Frame()
	await(t, updates, view, func() bool { return v.pendingNavigation == nil && v.currentPage() == 9_998 })
	if size := v.layout.Size(9_998); size.Width != 480 || size.Height != 420 || v.State().Err != nil {
		t.Fatalf("target used estimated or uncropped dimensions: %+v", size)
	}
	v.SetZoom(2)
	v.scroll.Y = float32(v.layout.Top(9_998)) + 30
	before := v.centerAnchor()
	key := geometryKey{document: id, page: 5_000}
	if !v.finishSize(key, pageSize{Width: 720, Height: 380}, nil) {
		t.Fatal("current dimension result rejected")
	}
	after := v.centerAnchor()
	page := v.layout.page(9_998)
	if before.Page != after.Page || math.Abs(float64(before.point.Y-after.point.Y))*float64(page.H) > 1 ||
		math.Abs(float64(before.point.X-after.point.X))*float64(page.W) > 1 {
		t.Fatalf("viewport anchor shifted: %+v to %+v", before, after)
	}
	short := samplePDF(3)
	refresh, next := v.LoadReaderAt(t.Context(), bytes.NewReader(short), int64(len(short)), LoadOptions{Mode: RefreshDocument})
	await(t, updates, view, func() bool { return v.document == refresh })
	waitReleased(t, released)
	if v.State().Page != 3 || v.finishSize(key, pageSize{Width: 600, Height: 500}, nil) {
		t.Fatalf("short refresh lost its clamped page or accepted an old result: %+v", v.State())
	}
	v.Close()
	waitReleased(t, next)
}

func TestDimensionQueuePriority(t *testing.T) {
	w := &worker{wake: make(chan struct{}, 1), dimensions: &dimensionQueue{
		document: 1, ctx: t.Context(), checked: make([]bool, 100_000), inFlight: -1, prefetch: -1}}
	if task := w.next(); !task.background || task.size.page != 0 {
		t.Fatalf("expected background sizing: %+v", task)
	}
	w.requestSizes(1, []int{99_999, 40}, -1)
	if task := w.next(); task.background || task.size.page != 99_999 {
		t.Fatalf("visible dimension request did not win: %+v", task)
	}
	w.requestSizes(2, []int{13}, -1)
	if task := w.next(); task.background || task.size.page != 40 {
		t.Fatalf("stale priority changed the active queue: %+v", task)
	}
}

func longMixedPDF() []byte {
	const count = 10_000
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", count, longPageReferences(count)),
	}
	for i := range count {
		bounds := "[0 0 300 430]"
		switch i {
		case 5_000:
			bounds = "[0 0 720 380]"
		case 9_997:
			bounds = "[0 0 290 820]"
		case 9_998:
			bounds = "[0 0 600 450] /CropBox [100 15 580 435]"
		}
		objects = append(objects, "<< /Type /Page /Parent 2 0 R /MediaBox "+bounds+" >>")
	}
	return encodePDF(objects)
}

func longPageReferences(count int) []byte {
	refs := make([]byte, 0, count*10)
	for i := range count {
		refs = fmt.Appendf(refs, "%d 0 R ", i+3)
	}
	return refs
}
