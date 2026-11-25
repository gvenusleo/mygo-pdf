package pdf

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestLoadNotificationsAllowReentrantOperations(t *testing.T) {
	updates := make(chan func(), 12)
	v := newViewer(func(fn func()) { updates <- fn }, testEngine(t))
	t.Cleanup(func() { v.Close(); <-v.Done() })

	var first, next uint64
	finished := false
	v.OnChange(func(s State) {
		if s.Loading || s.DisplayedGeneration == 0 {
			return
		}
		if s.DisplayedGeneration == first {
			v.Clear()
			next = v.LoadBytes(t.Context(), samplePDF(2), OpenDocument)
			return
		}
		if s.DisplayedGeneration != next || s.PageCount != 2 {
			t.Errorf("unexpected displayed document: %+v", s)
		}
		v.Close()
		finished = true
	})
	first = v.LoadBytes(t.Context(), samplePDF(4), OpenDocument)
	view := ui.NewTester(func(c *ui.Context) { v.View(c, nil) }, 420, 360)
	await(t, updates, view, func() bool { return finished })
	select {
	case <-v.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("close inside notification did not release the worker")
	}
}

func TestCancelQueuedDocumentPreservesDisplayedGeneration(t *testing.T) {
	updates := make(chan func(), 12)
	v := newViewer(func(fn func()) { updates <- fn }, testEngine(t))
	t.Cleanup(func() { v.Close(); <-v.Done() })
	view := ui.NewTester(func(c *ui.Context) { v.View(c, nil) }, 420, 360)
	first := v.LoadBytes(t.Context(), samplePDF(4), OpenDocument)
	await(t, updates, view, func() bool { return v.State().DisplayedGeneration == first && len(v.wanted) == 0 })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := v.LoadBytes(ctx, samplePDF(3), RefreshDocument)
	select {
	case result := <-updates:
		cancel()
		result()
	case <-time.After(5 * time.Second):
		t.Fatal("refresh result was not queued")
	}
	await(t, updates, view, func() bool { return !v.State().Loading })
	s := v.State()
	if s.RequestGeneration != request || s.DisplayedGeneration != first || !errors.Is(s.Err, context.Canceled) {
		t.Fatalf("unexpected state after cancel: %+v", s)
	}
}

func TestViewerNavigationAndFit(t *testing.T) {
	updates := make(chan func(), 12)
	v := newViewer(func(fn func()) { updates <- fn }, testEngine(t))
	t.Cleanup(func() { v.Close(); <-v.Done() })
	view := ui.NewTester(func(c *ui.Context) { v.View(c, nil) }, 500, 350)
	view.Frame()
	v.LoadBytes(t.Context(), samplePDF(4), OpenDocument)
	await(t, updates, view, func() bool { return v.State().PageCount == 4 })
	v.SetZoom(2)
	if !v.GoToPage(2) || v.State().Page != 2 || v.State().Zoom != 2 || v.GoToPage(0) {
		t.Fatalf("unexpected navigation: %+v", v.State())
	}
	v.SetFit(FitPage)
	if s := v.State(); s.Fit != FitPage || s.Zoom != 1 || v.layout.pages[1].H > v.height {
		t.Fatalf("unexpected page fit: %+v", s)
	}
	v.LoadBytes(t.Context(), samplePDF(3), OpenDocument)
	await(t, updates, view, func() bool { return v.State().PageCount == 3 })
	if v.State().Page != 1 || v.scroll.Y != 0 {
		t.Fatalf("new document did not start on page one: %+v", v.State())
	}
}
