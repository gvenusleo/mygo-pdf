package pdf

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"io"
	"sync"
	"testing"
	"time"
)

func TestReaderAtLifetimeAndPrefetch(t *testing.T) {
	v, view, updates := newTestView(t, 900, 700)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	data := samplePDF(10_000)
	id, released := v.LoadReaderAt(ctx, bytes.NewReader(data), int64(len(data)), LoadOptions{})
	await(t, updates, view, func() bool { return v.document == id || v.loadErr != nil })
	if v.loadErr != nil {
		t.Fatal(v.loadErr)
	}
	if known := v.layout.Geometry.Measured(); known > 8 {
		t.Fatalf("first viewport measured %d pages", known)
	}
	_, last := v.visiblePages()
	await(t, updates, view, func() bool { return v.cache[last+1].frame != nil })
	if measured := v.layout.Geometry.Measured(); measured > 8 {
		t.Fatalf("neighbor preview waited for %d size probes", measured)
	}
	cancel()
	if !v.GoToPage(9_999) {
		t.Fatal("valid distant page rejected")
	}
	view.Frame()
	await(t, updates, view, func() bool { return v.currentPage() == 9_998 && v.cache[9_998].frame != nil })
	select {
	case <-released:
		t.Fatal("displayed source released after load cancellation")
	default:
	}
	result := v.LoadBytes(t.Context(), data, RefreshDocument)
	await(t, updates, view, func() bool { return v.document == result })
	waitReleased(t, released)
	if v.currentPage() != 9_998 || v.cache[9_998].frame == nil {
		t.Fatalf("refresh lost the current page: %+v", v.State())
	}
}

func TestReaderRefreshKeepsEveryVisibleFrame(t *testing.T) {
	v, view, updates := newTestView(t, 420, 700)
	v.setScale(2)
	view.SetScale(2)
	v.SetFit(FitPage)
	data := bytes.ReplaceAll(samplePDF(5), []byte("300 430"), []byte("400 180"))
	open := func(ctx context.Context, pdf []byte, mode LoadMode) (uint64, <-chan struct{}) {
		return v.LoadReaderAt(ctx, bytes.NewReader(pdf), int64(len(pdf)), LoadOptions{Mode: mode})
	}
	old, oldRelease := open(t.Context(), data, OpenDocument)
	await(t, updates, view, func() bool { return v.document == old })
	green := bytes.ReplaceAll(data, []byte("1 0 0 rg"), []byte("0 1 0 rg"))
	id, newRelease := open(t.Context(), green, RefreshDocument)
	for v.document != id {
		previous := v.document
		select {
		case update := <-updates:
			update()
			view.Frame()
		case <-time.After(5 * time.Second):
			t.Fatal("reader-backed multi-page refresh did not settle")
		}
		first, last := v.visiblePages()
		if last <= first {
			t.Fatal("multi-page refresh viewport shows only one page")
		}
		assertVisibleFrames(t, v, first, last)
		if v.document != previous {
			visible, ok := view.Find("Page 2")
			if !ok || view.Image().RGBAAt(int((visible.X+20)*2), int((visible.Y+40)*2)) != (color.RGBA{G: 255, A: 255}) {
				t.Fatal("first refreshed frame did not include the replaced second page")
			}
		}
	}
	waitReleased(t, oldRelease)
	v.Close()
	waitReleased(t, newRelease)
}

func TestReaderAtPageCountLimit(t *testing.T) {
	e, err := testEngine(t)()
	if err != nil {
		t.Fatal(err)
	}
	// A poisoned count alone is insufficient because PDFium can recount the tree.
	const repetitions = 100_001
	kids := bytes.Repeat([]byte("3 0 R "), repetitions)
	data := encodePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Count 100001 /Kids [" + string(kids) + "] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 430] >>",
	})
	doc, err := e.OpenReader(t.Context(), 1, bytes.NewReader(data), int64(len(data)), "", func() {})
	if doc != nil {
		e.Close(doc)
	}
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("page count limit returned %v", err)
	}
}

type testReaderAt func([]byte, int64) (int, error)

func (read testReaderAt) ReadAt(p []byte, offset int64) (int, error) { return read(p, offset) }

func waitReleased(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reader was not released")
	}
}

func TestReaderAtSupersessionAndClose(t *testing.T) {
	v, _, _ := newTestView(t, 640, 480)
	data := samplePDF(2)
	entered, resume := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(resume) })
	defer unblock()
	reader := bytes.NewReader(data)
	blocked := testReaderAt(func(p []byte, off int64) (int, error) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-resume
		return reader.ReadAt(p, off)
	})
	_, first := v.LoadReaderAt(t.Context(), blocked, int64(len(data)), LoadOptions{})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("PDFium did not call ReadAt")
	}
	_, queued := v.LoadReaderAt(t.Context(), reader, int64(len(data)), LoadOptions{Mode: RefreshDocument})
	_, pending := v.LoadReaderAt(t.Context(), reader, int64(len(data)), LoadOptions{Mode: RefreshDocument})
	waitReleased(t, queued)
	v.Close()
	waitReleased(t, pending)
	select {
	case <-first:
		t.Fatal("reader released while ReadAt was blocked")
	case <-v.Done():
		t.Fatal("viewer stopped while PDFium retained the reader")
	default:
	}
	unblock()
	waitReleased(t, v.Done())
	waitReleased(t, first)
	if generation, done := v.LoadReaderAt(t.Context(), reader, int64(len(data)), LoadOptions{}); generation != 0 {
		t.Fatalf("closed viewer accepted generation %d", generation)
	} else {
		waitReleased(t, done)
	}
}

func TestReaderAtFailedRefreshPreservesSource(t *testing.T) {
	v, view, updates := newTestView(t, 700, 600)
	data := samplePDF(4)
	reader := bytes.NewReader(data)
	fullEOF := testReaderAt(func(p []byte, off int64) (int, error) {
		n, err := reader.ReadAt(p, off)
		if err == nil {
			err = io.EOF
		}
		return n, err
	})
	id, released := v.LoadReaderAt(t.Context(), fullEOF, int64(len(data)), LoadOptions{})
	await(t, updates, view, func() bool { return !v.State().Loading })
	if v.document != id {
		t.Fatalf("full reads ending in EOF failed: %v", v.State().Err)
	}
	for _, tc := range []struct {
		name string
		read io.ReaderAt
		size int64
		want error
	}{
		{"oversize", reader, MaxPDFBytes + 1, ErrLimit},
		{"missing", nil, 1, ErrInvalidPDF},
		{"empty", reader, 0, ErrInvalidPDF},
		{"short", testReaderAt(func([]byte, int64) (int, error) { return 0, io.EOF }), int64(len(data)), io.ErrUnexpectedEOF},
		{"error", testReaderAt(func([]byte, int64) (int, error) { return 0, io.ErrClosedPipe }), int64(len(data)), io.ErrClosedPipe},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, done := v.LoadReaderAt(t.Context(), tc.read, tc.size, LoadOptions{Mode: RefreshDocument})
			await(t, updates, view, func() bool { return !v.State().Loading })
			if !errors.Is(v.loadErr, tc.want) || v.document != id {
				t.Fatalf("failed refresh changed document or lost error: %+v", v.State())
			}
			waitReleased(t, done)
		})
	}
	select {
	case <-released:
		t.Fatal("failed refresh released the displayed source")
	default:
	}
	v.Clear()
	waitReleased(t, released)
}
