package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPasswordRetryAndExtractionPermissions(t *testing.T) {
	v, view, updates := newTestView(t, 700, 600)
	data, err := os.ReadFile("testdata/restricted.pdf")
	if err != nil {
		t.Fatal(err)
	}
	v.LoadBytes(t.Context(), samplePDF(2), OpenDocument)
	await(t, updates, view, func() bool { return v.document != 0 })
	old := v.document
	id := v.LoadBytesWithOptions(t.Context(), data, LoadOptions{Mode: RefreshDocument, Password: "wrong"})
	await(t, updates, view, func() bool { return !v.State().Loading })
	if !errors.Is(v.loadErr, ErrPassword) || v.document != old {
		t.Fatalf("byte-backed password failure in request %d: %+v", id, v.State())
	}
	for _, password := range []string{"", "wrong"} {
		_, released := v.LoadReaderAt(t.Context(), bytes.NewReader(data), int64(len(data)), LoadOptions{Mode: RefreshDocument, Password: password})
		await(t, updates, view, func() bool { return !v.State().Loading })
		if !errors.Is(v.loadErr, ErrPassword) || v.document != old {
			t.Fatalf("failed retry replaced document or lost password error: %+v", v.State())
		}
		waitReleased(t, released)
	}
	for _, password := range []string{"café", "fixture-owner"} {
		id := v.LoadBytesWithOptions(t.Context(), data, LoadOptions{Password: password})
		await(t, updates, view, func() bool { return !v.State().Loading && (v.metadata[0].data != nil || v.loadErr != nil) })
		if v.document != id || len(v.metadata[0].data.Chars) != 0 || v.metadata[0].data.Text == "" {
			t.Fatalf("wrong permissions after unlock: %+v", v.State())
		}
		query := strings.Fields(v.metadata[0].data.Text)[0]
		v.Find(t.Context(), query, FindForward)
		await(t, updates, view, func() bool { return !v.State().Find.Searching })
		if v.State().Find.Page == 0 || v.State().Find.Err != nil {
			t.Fatalf("find failed on copy-restricted PDF: %+v", v.State().Find)
		}
	}
	data, err = os.ReadFile("testdata/inaccessible.pdf")
	if err != nil {
		t.Fatal(err)
	}
	id, released := v.LoadReaderAt(t.Context(), bytes.NewReader(data), int64(len(data)), LoadOptions{Password: "café"})
	await(t, updates, view, func() bool { return !v.State().Loading && (v.metadata[0].data != nil || v.loadErr != nil) })
	if v.document != id || len(v.metadata[0].data.Chars) != 0 || v.metadata[0].data.Text != "" {
		t.Fatalf("restricted text was exposed: %+v", v.State())
	}
	v.Clear()
	waitReleased(t, released)
}

func TestOutlineAndFindAcrossReplacement(t *testing.T) {
	v, view, updates := newTestView(t, 900, 700)
	id := v.LoadBytes(t.Context(), readingToolPDF(), OpenDocument)
	await(t, updates, view, func() bool { return v.document == id })
	var result OutlineResult
	request := v.ReadOutline(t.Context(), func(r OutlineResult) { result = r })
	await(t, updates, view, func() bool { return result.RequestGeneration == request })
	if result.Err != nil || result.Truncated || len(result.Entries) != 2 {
		t.Fatalf("unexpected outline: %+v", result)
	}
	root := result.Entries[0]
	if root.Title != "章节" || root.Page != 0 || len(root.Children) != 1 || root.Children[0].Page != 2 || result.Entries[1].Page != 0 {
		t.Fatalf("incorrect entries: %+v", result.Entries)
	}
	if !v.GoToOutline(root.Children[0]) || v.currentPage() != 1 {
		t.Fatal("could not navigate to outline destination")
	}
	v.GoToPage(1)
	view.Frame()
	await(t, updates, view, func() bool { return v.metadata[0].data != nil })
	if len(v.metadata[0].data.Links) != 0 {
		t.Fatal("remote annotation became an interactive local link")
	}
	for _, tc := range []struct {
		direction FindDirection
		page      int
		index     int
	}{
		{FindForward, 1, 0}, {FindForward, 1, 7}, {FindForward, 2, 0}, {FindForward, 1, 0},
		{FindBackward, 2, 0}, {FindBackward, 1, 7}, {FindBackward, 1, 0},
	} {
		v.Find(t.Context(), "needle", tc.direction)
		await(t, updates, view, func() bool { return !v.findState.Searching })
		if v.findState.Page != tc.page || v.findState.Err != nil || v.findMatch.Index != tc.index || len(v.findMatch.Boxes) == 0 {
			t.Fatalf("find got %+v, match %+v, want page %d index %d", v.findState, v.findMatch, tc.page, tc.index)
		}
	}
	for _, query := range []string{"Needle\x00", strings.Repeat("文", 257)} {
		v.Find(t.Context(), query, FindForward)
		if v.findState.Searching || v.findState.Err == nil {
			t.Fatalf("invalid query was accepted: %+v", v.findState)
		}
	}
	v.Find(t.Context(), "missing", FindForward)
	await(t, updates, view, func() bool { return !v.findState.Searching })
	if v.findState.Page != 0 || v.findState.Err != nil {
		t.Fatalf("unexpected result for missing text: %+v", v.findState)
	}
	ctx, cancel := context.WithCancel(t.Context())
	v.Find(ctx, "missing", FindForward)
	cancel()
	await(t, updates, view, func() bool { return !v.findState.Searching })
	if !errors.Is(v.findState.Err, context.Canceled) {
		t.Fatalf("find cancellation lost: %+v", v.findState)
	}
	v.Find(t.Context(), "missing", FindForward)
	id = v.LoadBytes(t.Context(), samplePDF(3), OpenDocument)
	await(t, updates, view, func() bool { return v.document == id })
	if v.GoToOutline(root.Children[0]) || v.findState != (FindState{}) || v.currentPage() != 0 {
		t.Fatalf("stale reading tools affected the replacement: %+v", v.State())
	}
}

func TestKeyboardLinksAndRotation(t *testing.T) {
	v, view, updates := newTestView(t, 900, 700)
	var external string
	v.options.OnLink = func(url string) { external = url }
	v.LoadBytes(t.Context(), samplePDF(3), OpenDocument)
	await(t, updates, view, func() bool { return v.metadata[0].data != nil })
	if _, ok := view.AccessByRole(ui.RoleGroup, "Page 1"); !ok {
		t.Fatal("page number is absent from the accessibility tree")
	}
	if _, ok := view.AccessByRole(ui.RoleText, "Hello PDF"); !ok {
		t.Fatal("visible page text is absent from the accessibility tree")
	}
	if link, ok := view.AccessByRole(ui.RoleLink, "https://example.com/"); !ok || !link.Focusable {
		t.Fatalf("external link is not keyboard accessible: %+v", link)
	}
	view.Key(0, ui.KeyTab)
	view.Key(0, ui.KeyTab)
	view.Key(0, ui.KeyEnter)
	if external != "https://example.com/" {
		t.Fatalf("keyboard did not activate the external link: %q", external)
	}
	for turn := 1; turn <= 4; turn++ {
		v.SetRotation(turn)
		view.Frame()
		if _, ok := view.AccessByRole(ui.RoleGroup, "Page 1"); !ok {
			t.Fatalf("rotation %d removed the visible page number", turn)
		}
		if _, ok := view.AccessByRole(ui.RoleText, "Hello PDF"); !ok {
			t.Fatalf("rotation %d removed the visible text", turn)
		}
		await(t, updates, view, func() bool {
			frame := v.cache[0].frame
			return frame != nil && frame.key.Rotation == turn&3 && v.State().Render == RenderReady
		})
		if v.State().Rotation != turn&3 || (v.layout.page(0).W > v.layout.page(0).H) != (turn%2 != 0) {
			t.Fatalf("incorrect rotated layout on turn %d: %+v", turn, v.layout.page(0))
		}
	}
	v.GoToPage(3)
	view.Frame()
	if !view.Focused("PDF document") {
		t.Fatal("focus did not return to the viewport when the link disappeared")
	}
	if _, ok := view.AccessByRole(ui.RoleLink, "https://example.com/"); ok {
		t.Fatal("offscreen link remains in the accessibility tree")
	}
}

func readingToolPDF() []byte {
	const remote = "/S /GoToR /F (other.pdf) /D /end"
	page := func(stream int, extra string) string {
		return fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 430] /CropBox [100 10 295 420] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R %s >>", stream, extra)
	}
	stream := func(text, extra string) string {
		value := "BT /F1 16 Tf 140 350 Td (" + text + ") Tj " + extra + " ET\n"
		return fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(value), value)
	}
	return encodePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Outlines 8 0 R /Dests << /end [5 0 R /Fit] >> >>",
		"<< /Type /Pages /Count 2 /Kids [4 0 R 5 0 R] >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		page(6, "/Annots [12 0 R]"), page(7, ""), stream("Needle needle", "-120 -25 Td (needle) Tj"), stream("needle", ""),
		"<< /Type /Outlines /First 9 0 R /Last 11 0 R /Count 3 >>",
		"<< /Title <FEFF7AE08282> /Parent 8 0 R /First 10 0 R /Last 10 0 R /Next 11 0 R /Count 1 >>",
		"<< /Title (Last page) /Parent 9 0 R /A << /S /GoTo /D /end >> >>",
		"<< /Title (External) /Parent 8 0 R /Prev 9 0 R /A << " + remote + " >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [140 320 250 370] /A << " + remote + " >> >>",
	})
}
