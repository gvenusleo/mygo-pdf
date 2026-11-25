package pdf

import (
	"bytes"
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
	for _, tc := range []struct {
		name         string
		waitsForView bool
	}{
		{name: "before delivery"},
		{name: "while hidden", waitsForView: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updates := make(chan func(), 12)
			v := newViewer(func(fn func()) { updates <- fn }, testEngine(t))
			t.Cleanup(func() { v.Close(); <-v.Done() })
			visible := true
			view := ui.NewTester(func(c *ui.Context) {
				if visible {
					v.View(c, nil)
				}
			}, 420, 360)
			first := v.LoadBytes(t.Context(), samplePDF(4), OpenDocument)
			await(t, updates, view, func() bool { return v.State().DisplayedGeneration == first && len(v.wanted) == 0 })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			request := v.LoadBytes(ctx, samplePDF(3), RefreshDocument)
			select {
			case result := <-updates:
				if tc.waitsForView {
					visible = false
					result()
					view.Frame()
					if !v.State().Loading || v.State().DisplayedGeneration != first {
						t.Fatal("hidden viewer did not wait for layout before replacing the document")
					}
					cancel()
				} else {
					cancel()
					result()
				}
			case <-time.After(5 * time.Second):
				t.Fatal("refresh result was not queued")
			}
			await(t, updates, view, func() bool { return !v.State().Loading })
			visible = true
			view.Frame()
			s := v.State()
			if s.RequestGeneration != request || s.DisplayedGeneration != first || !errors.Is(s.Err, context.Canceled) {
				t.Fatalf("unexpected state after cancel: %+v", s)
			}
			if tc.waitsForView {
				next := v.LoadBytes(t.Context(), samplePDF(2), RefreshDocument)
				await(t, updates, view, func() bool { return v.State().DisplayedGeneration == next })
			}
		})
	}
}

func TestGoToPageWithMultipleVisiblePages(t *testing.T) {
	updates := make(chan func(), 12)
	v := newViewer(func(fn func()) { updates <- fn }, testEngine(t))
	t.Cleanup(func() { v.Close(); <-v.Done() })
	view := ui.NewTester(func(c *ui.Context) { v.View(c, nil) }, 510, 900)
	view.Frame()

	for _, count := range []int{2, 10} {
		data := bytes.ReplaceAll(samplePDF(count), []byte("[0 0 300 430]"), []byte("[0 0 500 280]"))
		id := v.LoadBytes(t.Context(), data, OpenDocument)
		await(t, updates, view, func() bool { return v.State().DisplayedGeneration == id })
		if got := v.State().Page; got != 1 {
			t.Errorf("opening %d pages selected page %d, want 1", count, got)
		}
		for _, page := range []int{2, min(3, count), 1} {
			if !v.GoToPage(page) {
				t.Fatalf("GoToPage rejected page %d of %d", page, count)
			}
			view.Frame()
			if got := v.State().Page; got != page {
				t.Errorf("navigating to page %d of %d selected page %d", page, count, got)
			}
		}
		if count == 10 {
			view.Scroll(100, 100, 0, 400)
			if got := v.State().Page; got != 3 {
				t.Errorf("scrolling selected page %d, want 3 at the viewport center", got)
			}
		}
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
