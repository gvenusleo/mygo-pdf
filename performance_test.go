package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Opt-in headless workload. Run each subtest in a fresh process to measure RSS.
func TestViewerPerformance(t *testing.T) {
	mode := os.Getenv("MYGO_PDF_MEASURE")
	if mode != "open" && mode != "gesture" {
		t.Skip("set MYGO_PDF_MEASURE to open or gesture")
	}
	for _, fixture := range []struct {
		name string
		pdf  func() []byte
	}{
		{"ordinary", func() []byte { return samplePDF(15) }},
		{"mixed", func() []byte {
			data := bytes.Replace(samplePDF(8), []byte("300 430"), []byte("430 300"), 1)
			return bytes.Replace(data, []byte("300 430"), []byte("300 740"), 1)
		}},
		{"scan", performanceScan},
		{"long", func() []byte { return samplePDF(10_000) }},
	} {
		t.Run(fixture.name, func(t *testing.T) { measureViewer(t, fixture.pdf(), mode) })
	}
}

func measureViewer(t *testing.T, data []byte, mode string) {
	updates := make(chan func(), 12)
	v := newViewer(func(fn func()) { updates <- fn }, testEngine(t))
	t.Cleanup(func() { v.Close(); <-v.Done() })
	view := ui.NewTester(func(c *ui.Context) { v.View(c, nil) }, 900, 700)
	view.Frame()
	v.setScale(2)
	start := time.Now()
	v.LoadBytes(t.Context(), data, OpenDocument)
	var firstBitmap, complete time.Duration
	await(t, updates, view, func() bool {
		if v.document == 0 {
			return false
		}
		first, last := v.visiblePages()
		allVisible := true
		for page := first; page <= last; page++ {
			if v.cache[page].frame == nil {
				allVisible = false
			} else if firstBitmap == 0 {
				firstBitmap = time.Since(start)
			}
		}
		if !allVisible {
			return false
		}
		complete = time.Since(start)
		return true
	})
	await(t, updates, view, func() bool { return v.State().Render == RenderReady })
	t.Logf("measure open first=%d viewport=%d sharp=%d", firstBitmap.Nanoseconds(), complete.Nanoseconds(), time.Since(start).Nanoseconds())
	if mode == "gesture" {
		for step := range 12 {
			start = time.Now()
			switch step % 4 {
			case 0:
				view.Pinch(450, 350, 1.25)
			case 1:
				view.Scroll(450, 350, 64, 120)
			case 2:
				view.Scroll(450, 350, -64, -120)
			case 3:
				view.Pinch(450, 350, 0.8)
			}
			frame := time.Since(start)
			await(t, updates, view, func() bool { return v.State().Render == RenderReady })
			t.Logf("measure gesture frame=%d sharp=%d", frame.Nanoseconds(), time.Since(start).Nanoseconds())
		}
	}
}

func performanceScan() []byte {
	const width, height = 1400, 1900
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	row := make([]byte, width)
	for y := range height {
		for x := range row {
			value := 234 + (x*17+y*7+x*y%13)%16
			if y > 80 && y < height-80 && y%36 < 8 && x > 60 && x < width-60 && (x/11+y/36)%9 < 6 {
				value -= 170
			}
			row[x] = byte(value)
		}
		_, _ = writer.Write(row)
	}
	_ = writer.Close()
	stream := "q 595 0 0 842 0 0 cm /Scan Do Q\n"
	return encodePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /XObject << /Scan 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", width, height, compressed.Len(), compressed.Bytes()),
	})
}
