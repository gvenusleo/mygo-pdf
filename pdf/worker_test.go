package pdf

import (
	"context"
	"errors"
	"testing"
)

func TestPrefetchYieldsToNewVisibleGeometry(t *testing.T) {
	w := &worker{wake: make(chan struct{}, 1)}
	prefetch := rasterKey{Document: 1, Page: 3}
	w.submitPages([]rasterKey{prefetch}, prefetch, 0, nil)
	job := w.next()
	ctx := job.ctx
	if job.page != prefetch || !job.background {
		t.Fatal("expected a prefetch raster in flight")
	}
	visible := geometryKey{document: 1, page: 2}
	w.submitPages([]rasterKey{prefetch}, prefetch, 0, []geometryKey{visible})
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("prefetch did not yield to new visible geometry")
	}
	w.completeRasterTask(prefetch, false)
	if key := w.next().metadata; key != visible {
		t.Fatalf("next geometry key = %+v, want %+v", key, visible)
	}
}
