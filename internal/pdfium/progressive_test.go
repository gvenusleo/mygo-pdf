package pdfium

import (
	"bytes"
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/gvenusleo/mygo-pdf/internal/render"
)

func TestProgressiveRenderClosesOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	closed := 0
	e := &Engine{
		renderStart: func(_, _ uintptr, _, _, _, _, _, _ int32, _ *renderPause) int32 { return 1 },
		renderContinue: func(_ uintptr, _ *renderPause) int32 {
			cancel()
			return 1
		},
		renderClose: func(uintptr) { closed++ },
	}
	mu.Lock()
	err := e.drawProgressive(ctx, 1, 2, 0, 0, 100, 100, 0, false)
	mu.Unlock()
	if !errors.Is(err, context.Canceled) || closed != 1 {
		t.Fatalf("canceled render returned %v and closed %d times", err, closed)
	}
}

func TestNativeGateOrdersVisibleWorkAndCanceledPrefetch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	waiting := &prefetchWaitContext{Context: ctx, registered: make(chan struct{})}
	gate := &nativeGate{Cond: sync.Cond{L: new(sync.Mutex)}}
	gate.Lock()
	release := sync.OnceFunc(gate.Unlock)
	defer release()
	order := make(chan string, 3)
	queue := func(label string, ctx context.Context, background bool) {
		go func() {
			gate.lock(ctx, background)
			order <- label
			gate.Unlock()
		}()
	}
	queue("prefetch cleanup", waiting, true)
	select {
	case <-waiting.registered:
	case <-time.After(5 * time.Second):
		t.Fatal("prefetch did not register its cancellation wakeup")
	}
	queue("visible", t.Context(), false)
	awaitNativeQueue(t, gate, 1)
	cancel()
	awaitNativeQueue(t, gate, 2)
	queue("later visible", t.Context(), false)
	awaitNativeQueue(t, gate, 3)
	release()
	for _, want := range []string{"visible", "prefetch cleanup", "later visible"} {
		select {
		case got := <-order:
			if got != want {
				t.Fatalf("next native call = %s, want %s", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("waiting for %s", want)
		}
	}
}

// Signals when prefetch registers its cancellation wakeup.
type prefetchWaitContext struct {
	context.Context
	registered chan struct{}
	once       sync.Once
}

func (c *prefetchWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.registered) })
	return c.Context.Done()
}

func awaitNativeQueue(t *testing.T, gate *nativeGate, count uint64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		gate.L.Lock()
		waiting := gate.next - gate.serving
		gate.L.Unlock()
		if waiting == count {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("native queue has %d waiters, want %d", waiting, count)
		}
		runtime.Gosched()
	}
}

func TestProgressiveMatchesSynchronousPDFium(t *testing.T) {
	name, err := LibraryName()
	if err != nil {
		t.Skip(err)
	}
	path := os.Getenv("MYGO_PDF_TEST_LIBRARY")
	if path == "" {
		path, err = filepath.Abs(filepath.Join("..", "..", "build", "test-resources", runtime.GOOS+"-"+runtime.GOARCH, "pdfium", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) && os.Getenv("MYGO_PDF_REQUIRE_INTEGRATION") != "1" {
			t.Skip("local PDFium resources have not been prepared")
		}
	}
	e, err := LoadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	if !e.HasProgressiveRendering() {
		t.Fatal("expected the pinned library to export the progressive rendering API")
	}
	data, err := os.ReadFile("../../examples/viewer/sample.pdf")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := e.Open(t.Context(), 1, data)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close(doc)
	syncEngine := *e
	syncEngine.renderStart = nil
	progressive := *e
	var pauses, resumes int
	progressive.renderStart = func(bitmap, page uintptr, x, y, w, h, rotate, flags int32, pause *renderPause) int32 {
		renderDeadline = time.Time{}
		status := e.renderStart(bitmap, page, x, y, w, h, rotate, flags, pause)
		if status == 1 {
			pauses++
		}
		return status
	}
	progressive.renderContinue = func(page uintptr, pause *renderPause) int32 {
		resumes++
		return e.renderContinue(page, pause)
	}
	for _, tile := range []image.Rectangle{{}, image.Rect(140, 200, 600, 650)} {
		key := render.Key{Document: 1, Size: image.Pt(720, 1020), Tile: tile}
		got, err := progressive.Render(t.Context(), doc, key, false)
		if err != nil {
			t.Fatal(err)
		}
		want, err := syncEngine.Render(t.Context(), doc, key, false)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Pix, want.Pix) {
			t.Fatalf("progressive pixels differ for raster tile %v", tile)
		}
	}
	if pauses == 0 || resumes == 0 {
		t.Fatalf("real PDFium did not take the forced pause, pauses=%d resumes=%d", pauses, resumes)
	}
}
