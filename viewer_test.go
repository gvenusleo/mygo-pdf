package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/gvenusleo/mygo-pdf/internal/pdfium"
	"github.com/gvenusleo/mygo-pdf/internal/render"
)

func TestPDFiumPreview(t *testing.T) {
	loader := testEngine(t)
	updates := make(chan func(), 1)
	v := newViewer(func(fn func()) { updates <- fn }, loader)
	t.Cleanup(func() {
		v.Close()
		select {
		case <-v.Done():
		case <-time.After(5 * time.Second):
			t.Error("background work remained after closing the viewer")
		}
	})
	view := ui.NewTester(func(c *ui.Context) { v.View(c, nil) }, 420, 360)
	view.Frame()
	v.LoadBytes(t.Context(), samplePDF(4), RefreshDocument)
	await(t, updates, view, func() bool { return v.document != 0 })
	first, ok := view.Find("Page 1")
	if !ok || first.W < 300 || len(v.pages) != 4 {
		t.Fatalf("initial load: first page %v, page count %d", first, len(v.pages))
	}
	img := view.Image()
	if pixel := img.RGBAAt(int(first.X+first.W*0.1), int(first.Y+first.H*0.2)); pixel != (color.RGBA{255, 0, 0, 255}) {
		t.Fatalf("unexpected PDF pixel color: %v", pixel)
	}
	await(t, updates, view, func() bool { return v.metadata[0].data != nil })
	data := v.metadata[0].data
	if len(data.Chars) == 0 || len(data.Links) < 2 {
		t.Fatalf("missing PDF text or links: %d glyphs, %d links", len(data.Chars), len(data.Links))
	}
	for _, label := range []string{"Zoom out", "Zoom in", "Fit width"} {
		if _, exists := view.Find(label); exists {
			t.Fatalf("unexpected %s button in the preview", label)
		}
	}
	view.ClickAt(first.X+30, first.Y+30)
	view.Key(ui.Shift, ui.KeyEqual)
	if v.zoom != 1.25 {
		t.Fatalf("zoom after keyboard zoom-in: %v", v.zoom)
	}
	view.Key(0, ui.KeyMinus)
	if v.zoom != 1 {
		t.Fatalf("zoom after keyboard zoom-out: %v", v.zoom)
	}
	view.Pinch(first.X+80, first.Y+100, 1.25)
	if v.zoom != 1.25 {
		t.Fatalf("zoom after a pinch on the page: %v", v.zoom)
	}
	view.Key(0, ui.Key0)
	if !v.viewportInput(ui.Services{})(ui.InputEvent{Kind: ui.InputScroll, Mods: ui.Ctrl, DY: -30, X: 100, Y: 100}) || v.zoom <= 1 {
		t.Fatalf("zoom after modifier-wheel input: %v", v.zoom)
	}
	view.Key(0, ui.Key0)
	if v.zoom != 1 {
		t.Fatalf("zoom after keyboard fit-width: %v", v.zoom)
	}
	v.scroll.X, v.scroll.Y = 0, 0
	view.Frame()
	if v.metadata[0].data != data {
		t.Fatal("zoom replaced the cached page geometry")
	}
	checkCopy(t, v, view, updates, first, v.layout.page(0), data)
	view.Scroll(100, 100, 0, v.layout.page(2).Y-v.scroll.Y+20)
	if v.anchor().page != 2 {
		t.Fatalf("continuous scrolling stopped on page %d", v.anchor().page+1)
	}
	checkRefresh(t, v, view, updates)
	view.SetSize(600, 380)
	view.Frame()
	if v.width != 600 || v.height <= 0 {
		t.Fatalf("viewport dimensions did not update: %v x %v", v.width, v.height)
	}
	checkSupersededLoad(t, v, view, updates)
	checkSharpTiles(t, v, view, updates)
}

func checkCopy(t *testing.T, v *Viewer, view *ui.Tester, updates <-chan func(), visible, page ui.Rect, data *geometry) {
	t.Helper()
	begin, end := data.Chars[0], data.Chars[len(data.Chars)-1]
	x1, y1 := visible.X+(begin.X+begin.W/2)*page.W, visible.Y+(begin.Y+begin.H/2)*page.H
	x2, y2 := visible.X+(end.X+end.W/2)*page.W, visible.Y+(end.Y+end.H/2)*page.H
	view.Press(x1, y1)
	view.Move(x2, y2)
	view.Release(x2, y2)
	view.Key(ui.Cmd, ui.KeyC)
	if !v.hasSelection {
		t.Fatal("dragging did not select text")
	}
	await(t, updates, view, func() bool { return view.Clipboard() != "" })
	if got := view.Clipboard(); got != "Hello PDF" {
		t.Errorf("copied text: %q", got)
	}
	view.SetClipboard("")
	view.RightClickAt(x1, y1)
	if err := view.ChooseMenuItem("Copy"); err != nil {
		t.Fatal(err)
	}
	await(t, updates, view, func() bool { return view.Clipboard() == "Hello PDF" })
	var internal pageLink
	for _, link := range data.Links {
		if link.Page == 1 {
			internal = link
		}
	}
	if internal.Bounds.W == 0 {
		t.Fatal("internal link was not found")
	}
	view.ClickAt(visible.X+(internal.Bounds.X+internal.Bounds.W/2)*page.W, visible.Y+(internal.Bounds.Y+internal.Bounds.H/2)*page.H)
	if v.anchor().page != 1 {
		t.Fatalf("internal link jumped to page %d", v.anchor().page+1)
	}
	if v.anchor().fraction < 0.2 {
		t.Fatal("the jump stopped at the page top instead of the destination within the page")
	}
}

func checkRefresh(t *testing.T, v *Viewer, view *ui.Tester, updates <-chan func()) {
	t.Helper()
	v.setZoom(1.25, 0, 0)
	v.scroll.Y = v.layout.offset(readingPosition{page: 1, fraction: 0.4})
	view.Frame()
	before, id := v.anchor(), v.document
	v.LoadBytes(t.Context(), samplePDF(3), RefreshDocument)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if v.LoadBytes(canceled, samplePDF(1), OpenDocument) != 0 {
		t.Fatal("a canceled request entered the queue")
	}
	await(t, updates, view, func() bool { return v.document != id })
	if after := v.anchor(); after.page != before.page || after.fraction < 0.39 || after.fraction > 0.41 || v.zoom != 1.25 {
		t.Fatalf("refresh lost the reading position: before %+v, after %+v, zoom %v", before, after, v.zoom)
	}
	id = v.document
	v.LoadBytes(t.Context(), []byte("invalid"), RefreshDocument)
	await(t, updates, view, func() bool { return !v.State().Loading })
	if !errors.Is(v.State().Err, ErrInvalidPDF) {
		t.Fatalf("unexpected load error for malformed PDF: %v", v.State().Err)
	}
	if v.document != id || len(v.pages) != 3 {
		t.Fatal("a malformed PDF replaced the existing preview")
	}
	v.Clear()
	if len(v.pages) != 0 || len(v.cache) != 0 || v.scroll.Y != 0 {
		t.Fatal("clearing retained the old document state")
	}
}

func checkSharpTiles(t *testing.T, v *Viewer, view *ui.Tester, updates <-chan func()) {
	t.Helper()
	v.setScale(2)
	v.setZoom(4, 0, 0)
	v.scroll.X, v.scroll.Y = 200, 30
	view.Frame()
	before := bytes.Clone(view.Image().Pix)
	if len(v.tileKeys) == 0 {
		t.Fatal("no high-resolution tiles requested at high zoom")
	}
	key := v.tileKeys[0]
	if got, want := float32(key.Size.X), 2*v.layout.page(key.Page).W; got < want || got > want+1 {
		t.Fatalf("render width at high zoom was %v, want at least %v pixels", got, want)
	}
	await(t, updates, view, func() bool {
		for _, key := range v.tileKeys {
			if v.tiles[key].frame == nil {
				return false
			}
		}
		return true
	})
	w, h := v.tiles[key].frame.bitmap.Size()
	if w != key.Tile.Dx() || h != key.Tile.Dy() {
		t.Fatalf("unexpected tile dimensions: %d x %d", w, h)
	}
	if got := view.Image().RGBAAt(100, 60); got != (color.RGBA{255, 0, 0, 255}) {
		t.Fatalf("offset tiles did not align with the page: pixel color %v", got)
	}
	if bytes.Equal(before, view.Image().Pix) {
		t.Fatal("the screen still shows only the fallback image after the tiles completed")
	}
	previous := v.document
	changed := bytes.ReplaceAll(samplePDF(2), []byte("1 0 0 rg"), []byte("0 1 0 rg"))
	v.LoadBytes(t.Context(), changed, RefreshDocument)
	await(t, updates, view, func() bool { return v.document != previous })
	if got := view.Image().RGBAAt(100, 60); got != (color.RGBA{0, 255, 0, 255}) {
		t.Fatalf("a tile from the old document remained after refresh: pixel color %v", got)
	}
}

func TestGeometryOutlivesRasterAndRejectsOldDocument(t *testing.T) {
	v, view, updates := newTestView(t, 420, 360)
	old := v.LoadBytes(t.Context(), samplePDF(4), OpenDocument)
	await(t, updates, view, func() bool { return v.document == old && v.metadata[0].data != nil })
	data := v.metadata[0].data
	v.removePage(0)
	v.setScale(2)
	view.Frame()
	await(t, updates, view, func() bool { return v.cache[0].frame != nil })
	if v.metadata[0].data != data || len(v.metadataWanted) != 0 {
		t.Fatal("evicting a raster invalidated page geometry")
	}
	next := v.LoadBytes(t.Context(), samplePDF(3), RefreshDocument)
	await(t, updates, view, func() bool { return v.document == next })
	if v.finishMetadata(geometryKey{old, 0}, data) || v.metadata[0].data == data {
		t.Fatal("old page geometry was applied after refresh")
	}
}

func TestRefreshRendersVisiblePages(t *testing.T) {
	updates := make(chan func(), 1)
	v := newViewer(func(fn func()) { updates <- fn }, testEngine(t))
	t.Cleanup(func() { v.Close(); <-v.Done() })
	view := ui.NewTester(func(c *ui.Context) { v.View(c, nil) }, 420, 700)
	view.Frame()
	data := samplePDF(6)
	load := func(data []byte) {
		previous := v.document
		v.LoadBytes(t.Context(), data, RefreshDocument)
		await(t, updates, view, func() bool { return v.document != previous })
	}
	load(data)
	await(t, updates, view, func() bool { return v.cache[1].frame != nil })
	second, ok := view.Find("Page 2")
	if !ok || second.Y+40 >= v.height {
		t.Fatal("the second page is outside the test viewport")
	}
	green := color.RGBA{0, 255, 0, 255}
	load(bytes.ReplaceAll(data, []byte("1 0 0 rg"), []byte("0 1 0 rg")))
	if got := view.Image().RGBAAt(int(second.X+20), int(second.Y+40)); got != green {
		t.Errorf("second-page pixel after refresh: got %v, want %v", got, green)
	}

	// The user can scroll while a result is queued. The refresh must cover the new viewport.
	await(t, updates, view, func() bool { return len(v.wanted) == 0 && len(v.metadataWanted) == 0 })
	previous := v.document
	v.LoadBytes(t.Context(), data, RefreshDocument)
	select {
	case apply := <-updates:
		v.scroll.Y = v.layout.page(3).Y
		view.Frame()
		apply()
		view.Frame()
	case <-time.After(5 * time.Second):
		t.Fatal("refresh result did not arrive")
	}
	await(t, updates, view, func() bool { return v.document != previous })
	first, last := v.visiblePages()
	assertVisibleFrames(t, v, first, last)

	v.scroll.Y = v.layout.page(4).Y
	view.Frame()
	if v.anchor().page < 2 {
		t.Fatal("the test viewport did not reach the pages about to be removed")
	}
	load(samplePDF(2))
	first, last = v.visiblePages()
	assertVisibleFrames(t, v, first, last)
}

func TestRefreshWaitsForParentLayout(t *testing.T) {
	updates := make(chan func(), 12)
	v := newViewer(func(fn func()) { updates <- fn }, testEngine(t))
	t.Cleanup(func() { v.Close(); <-v.Done() })
	expanded := false
	view := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			if !expanded {
				ui.Box(c).Height(520).Shrink(0)
			}
			v.View(c.Key("pdf"), expanded)
		})
	}, 500, 700)
	view.Frame()
	data := bytes.ReplaceAll(samplePDF(4), []byte("[0 0 300 430]"), []byte("[0 0 500 280]"))
	old := v.LoadBytes(t.Context(), data, OpenDocument)
	await(t, updates, view, func() bool { return v.document == old && len(v.wanted) == 0 && len(v.metadataWanted) == 0 })
	changed := bytes.ReplaceAll(data, []byte("1 0 0 rg"), []byte("0 1 0 rg"))
	id := v.LoadBytes(t.Context(), changed, RefreshDocument)
	select {
	case apply := <-updates:
		expanded = true
		apply()
		view.Frame()
	case <-time.After(5 * time.Second):
		t.Fatal("refresh result did not arrive")
	}
	first, last := v.visiblePages()
	if last <= first {
		t.Fatal("the expanded viewport must show multiple pages")
	}
	if v.document != old {
		t.Error("the new document replaced the old one before the expanded viewport was rendered")
	}
	await(t, updates, view, func() bool { return v.document == id })
	assertVisibleFrames(t, v, first, last)
	second, ok := view.Find("Page 2")
	if !ok || view.Image().RGBAAt(int(second.X+10), int(second.Y+20)) != (color.RGBA{0, 255, 0, 255}) {
		t.Fatal("the first frame of the new document does not show the new second page")
	}
}

func assertVisibleFrames(t *testing.T, v *Viewer, first, last int) {
	t.Helper()
	for page := first; page <= last; page++ {
		frame := v.cache[page].frame
		if frame == nil || frame.key.Document != v.document {
			t.Errorf("page %d has no bitmap from the new document", page+1)
		}
	}
}

func TestVisiblePageRequestsSettle(t *testing.T) {
	v := &Viewer{
		worker: &worker{wake: make(chan struct{}, 1)}, document: 1,
		width: 3800, height: 2200, gap: 12, zoom: 1, scale: 1,
		cache: make(map[int]cachedPage), tiles: make(map[rasterKey]cachedPage),
	}
	v.pages = make([]pageSize, 10)
	for i := range v.pages {
		v.pages[i] = pageSize{Width: 3800, Height: 180}
	}
	v.layout = newFittedLayout(v.pages, v.loadViewport())
	first, last := v.visiblePages()
	for range 60 {
		v.requestVisible(first, last)
		if len(v.wanted) == 0 {
			if len(v.tiles) == 0 || len(v.cache) != len(v.pages) {
				t.Fatal("visible tiles and full-page fallbacks did not finish")
			}
			return
		}
		key := v.wanted[0]
		v.finishPage(key, &pageFrame{key: key, pixels: image.NewRGBA(image.Rectangle{Max: key.Region().Size()})}, nil)
	}
	t.Fatal("visible pages kept rerendering")
}

func TestMissingVisibleBitmapsTakePriorityOverTiles(t *testing.T) {
	v := &Viewer{
		worker: &worker{wake: make(chan struct{}, 1)}, document: 1,
		width: 1800, height: 1000, gap: 12, zoom: 2, scale: 2,
		cache: make(map[int]cachedPage), tiles: make(map[rasterKey]cachedPage),
		pages: []pageSize{
			{Width: 600, Height: 100}, {Width: 600, Height: 100},
			{Width: 600, Height: 100}, {Width: 600, Height: 100},
		},
	}
	v.layout = newFittedLayout(v.pages, v.loadViewport())
	v.scroll.Y = v.layout.page(1).Y
	first, last := v.visiblePages()
	v.requestVisible(first, last)
	visible := last - first + 1
	if visible < 2 || len(v.tileKeys) == 0 || len(v.wanted) <= visible {
		t.Fatal("expected several visible bitmaps and visible tiles")
	}
	for i, key := range v.wanted[:visible] {
		if key.Page != first+i || !key.Tile.Empty() {
			t.Fatalf("request %d = %+v, want the missing bitmap for visible page %d", i, key, first+i+1)
		}
	}
	if v.wanted[visible].Tile.Empty() {
		t.Fatal("expected visible tiles immediately after the missing bitmaps")
	}
	if key := v.wanted[len(v.wanted)-1]; key.Page != last+1 || !key.Tile.Empty() {
		t.Fatalf("last request = %+v, want a prefetch for page %d", key, last+2)
	}
}

func TestDirectionalPrefetchAndTileReuse(t *testing.T) {
	v := &Viewer{
		worker: &worker{wake: make(chan struct{}, 1)}, document: 1,
		width: 900, height: 600, gap: 12, zoom: 3, scale: 2,
		cache: make(map[int]cachedPage), tiles: make(map[rasterKey]cachedPage),
		pages: []pageSize{{Width: 600, Height: 800}, {Width: 600, Height: 800}, {Width: 600, Height: 800}},
	}
	v.layout = newFittedLayout(v.pages, v.loadViewport())
	v.scroll.Y = v.layout.page(1).Y + 30
	first, last := v.visiblePages()
	v.requestVisible(first, last)
	if got := v.wanted[len(v.wanted)-1].Page; first != 1 || last != 1 || got != 2 {
		t.Fatalf("downward prefetch = %d, visible = %d..%d", got, first, last)
	}
	v.scroll.Y -= 15
	v.requestVisible(first, last)
	if got := v.wanted[len(v.wanted)-1].Page; got != 0 {
		t.Fatalf("upward prefetch = %d, want 0", got)
	}
	key := v.tileKeys[0]
	frame := &pageFrame{key: key, pixels: image.NewRGBA(key.Tile.Sub(key.Tile.Min))}
	if !v.finishPage(key, frame, nil) || v.tileBytes != key.Tile.Dx()*key.Tile.Dy()*4 {
		t.Fatal("visible tile was not cached with its region size")
	}
	v.scroll.X = 270
	v.requestVisible(first, last)
	if slices.Contains(v.tileKeys, key) || v.tiles[key].frame != frame {
		t.Fatal("expected the tile just outside the viewport to stay cached")
	}
	v.scroll.X = 0
	v.requestVisible(first, last)
	if !slices.Contains(v.tileKeys, key) || slices.Contains(v.wanted, key) {
		t.Fatal("returning to the cached tile requested another raster")
	}
	v.setZoom(3.25, 0, 0)
	v.requestVisible(first, last)
	if slices.Contains(v.tileKeys, key) || v.tiles[key].frame != frame {
		t.Fatal("the old raster should stay cached without being drawn at the new scale")
	}
	v.setZoom(3, 0, 0)
	v.requestVisible(first, last)
	if !slices.Contains(v.tileKeys, key) || slices.Contains(v.wanted, key) {
		t.Fatal("returning to the original zoom requested another raster")
	}
	v.scroll.X = 560
	v.requestVisible(first, last)
	if v.tiles[key].frame != nil || v.tileBytes != 0 {
		t.Fatal("tile beyond the one-tile margin was retained")
	}
	v.wanted = []rasterKey{v.layout.preview(v.document, 0, v.zoom)}
	if v.visibleWorkPending() {
		t.Fatal("upward prefetch was counted as visible work")
	}
}

func TestTileCoverageKeepsPreviewUntilViewportIsFilled(t *testing.T) {
	v := &Viewer{document: 1, width: 700, height: 400, tiles: make(map[rasterKey]cachedPage)}
	bounds := ui.Rect{W: 1024, H: 512}
	size := image.Pt(1024, 512)
	left := rasterKey{Document: 1, Size: size, Tile: image.Rect(0, 0, 512, 512)}
	right := rasterKey{Document: 1, Size: size, Tile: image.Rect(512, 0, 1024, 512)}
	v.tileKeys = []rasterKey{left, right}
	v.tiles[left] = cachedPage{frame: &pageFrame{key: left}}
	if v.tilesCoverViewport(0, bounds) {
		t.Fatal("a missing visible tile must keep the page preview")
	}
	v.tiles[right] = cachedPage{frame: &pageFrame{key: right}}
	if !v.tilesCoverViewport(0, bounds) {
		t.Fatal("completed tiles cover the viewport")
	}
	v.tileKeys = v.tileKeys[:1]
	if v.tilesCoverViewport(0, bounds) {
		t.Fatal("an incomplete demand list must keep the page preview")
	}
}

func TestTileBudgetEvictsOnlyOffscreenTiles(t *testing.T) {
	v := &Viewer{
		document: 1, width: 512, height: 512, zoom: 1, scale: 1,
		pages: []pageSize{{Width: 600, Height: 800}}, tiles: make(map[rasterKey]cachedPage),
	}
	v.layout = newFittedLayout(v.pages, v.loadViewport())
	visible := rasterKey{Document: 1, Size: image.Pt(512, 800), Tile: image.Rect(0, 0, 512, 512)}
	v.tileKeys = []rasterKey{visible}
	v.tiles[visible] = cachedPage{frame: &pageFrame{key: visible}}
	v.tileBytes = v.tiles[visible].frame.frameBytes()
	for i := range 41 {
		key := rasterKey{Document: 1, Size: image.Pt(1024, 1600+i), Tile: image.Rect(512, 1024, 1024, 1536)}
		v.tiles[key] = cachedPage{frame: &pageFrame{key: key}}
		v.tileBytes += v.tiles[key].frame.frameBytes()
	}
	v.trimTiles()
	if v.tileBytes > render.TileBudget || v.tiles[visible].frame == nil {
		t.Fatalf("tile cache has %d bytes after eviction, visible tile present: %t", v.tileBytes, v.tiles[visible].frame != nil)
	}
}

func checkSupersededLoad(t *testing.T, v *Viewer, view *ui.Tester, updates <-chan func()) {
	t.Helper()
	v.LoadBytes(t.Context(), samplePDF(5), OpenDocument)
	var oldUpdate func()
	select {
	case oldUpdate = <-updates:
	case <-time.After(10 * time.Second):
		t.Fatal("the old document did not finish background rendering")
	}
	v.Clear()
	v.LoadBytes(t.Context(), samplePDF(2), RefreshDocument)
	oldUpdate()
	if v.document != 0 {
		t.Fatal("a stale document replaced the preview after switching")
	}
	await(t, updates, view, func() bool { return v.document != 0 })
	if len(v.pages) != 2 {
		t.Fatalf("unexpected page count after switching documents: %d", len(v.pages))
	}
}

func await(t *testing.T, updates <-chan func(), view *ui.Tester, until func() bool) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for !until() {
		select {
		case update := <-updates:
			update()
			view.Frame()
		case <-timeout:
			t.Fatal("PDFium result did not arrive")
		}
	}
}

func newTestView(t *testing.T, width, height int) (*Viewer, *ui.Tester, <-chan func()) {
	t.Helper()
	updates := make(chan func(), 12)
	v := newViewer(func(fn func()) { updates <- fn }, testEngine(t))
	t.Cleanup(func() { v.Close(); <-v.Done() })
	view := ui.NewTester(func(c *ui.Context) { v.View(c, nil) }, width, height)
	view.Frame()
	return v, view, updates
}

func testEngine(t *testing.T) func() (*engine, error) {
	t.Helper()
	name, err := pdfium.LibraryName()
	if err != nil {
		t.Skip(err)
	}
	path := os.Getenv("MYGO_PDF_TEST_LIBRARY")
	if path == "" {
		path, err = filepath.Abs(filepath.Join("build", "test-resources", runtime.GOOS+"-"+runtime.GOARCH, "pdfium", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if os.Getenv("MYGO_PDF_REQUIRE_INTEGRATION") == "1" {
				t.Fatalf("PDFium integration requires %s", path)
			}
			t.Skip("local PDFium resources have not been prepared")
		}
	}
	return func() (*engine, error) { return pdfium.LoadEngine(path) }
}

func samplePDF(count int) []byte {
	var kids strings.Builder
	for i := range count {
		fmt.Fprintf(&kids, "%d 0 R ", 5+i)
	}
	stream := "1 0 0 rg 0 0 60 430 re f\n0 0 0 rg BT /F1 22 Tf 85 380 Td (Hello PDF) Tj ET\n"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", count, kids.String()),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream),
	}
	for i := range count {
		page := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 430] /Resources << /Font << /F1 3 0 R >> >> /Contents 4 0 R"
		if i == 0 && count >= 2 {
			page += fmt.Sprintf(" /Annots [%d 0 R %d 0 R]", 5+count, 6+count)
		}
		objects = append(objects, page+" >>")
	}
	objects = append(objects,
		"<< /Type /Annot /Subtype /Link /Rect [120 325 212 355] /A << /S /URI /URI (https://example.com/) >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [80 325 115 355] /Dest [6 0 R /XYZ 0 300 null] >>",
	)
	return encodePDF(objects)
}

func encodePDF(objects []string) []byte {
	var pdf strings.Builder
	pdf.WriteString("%PDF-1.7\n")
	positions := make([]int, len(objects))
	for i, object := range objects {
		positions[i] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range positions {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return []byte(pdf.String())
}

func TestRasterErrorFollowsViewportDemand(t *testing.T) {
	v := &Viewer{width: 500, height: 300, zoom: 1, scale: 2, document: 1, cache: make(map[int]cachedPage), worker: &worker{wake: make(chan struct{}, 1)}}
	v.pages = []pageSize{{Width: 400, Height: 600}}
	v.layout = newFittedLayout(v.pages, v.loadViewport())
	view := ui.NewTester(func(c *ui.Context) { v.View(c, nil) }, 500, 300)
	view.Frame()
	v.cache[0] = cachedPage{failed: v.layout.raster(v.document, 0, v.scale), err: fmt.Errorf("obsolete resolution failed")}
	view.Frame()
	if _, shown := view.Find("obsolete resolution failed"); !shown || v.State().Render != RenderFailed {
		t.Fatal("the current raster error must be visible in both state and view")
	}
	v.SetZoom(1.25)
	view.Frame()
	if state := v.State(); state.Render != RenderPending || state.Err != nil {
		t.Fatalf("a different raster is pending, got %+v", state)
	}
	if _, shown := view.Find("obsolete resolution failed"); shown {
		t.Fatal("the view still shows the obsolete raster error")
	}
}
