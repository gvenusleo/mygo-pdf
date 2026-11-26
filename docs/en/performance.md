# MyGo PDF performance

[简体中文](../zh-CN/performance.md)

## Current measurements

Measured on the current viewer source (`99231ad`) on Apple M3 with macOS 27.2.1, Go 1.27.4, MyGo v0.10.0 and PDFium chromium/8179. The headless MyGo Tester viewport is 900 × 700 DIP at 2× raster scale. Each row reports P50/P95 from 30 fresh processes. Times are in milliseconds and peak process RSS is in MiB.

| Document | Input | Complete viewport | Render ready | Peak RSS |
| --- | --- | --- | --- | --- |
| 15 pages | Bytes | 18.41/23.40 | 51.54/66.32 | 49.97/50.12 |
| 15 pages | `io.ReaderAt` | 18.23/18.34 | 50.90/51.41 | 46.00/49.73 |
| 10,000 pages | Bytes | 52.58/56.50 | 85.14/92.23 | 63.59/64.09 |
| 10,000 pages | `io.ReaderAt` | 20.26/24.38 | 52.83/59.93 | 50.86/51.41 |

The complete viewport time ends when a first set of bitmaps covers the visible pages. Render-ready time ends at `RenderReady`. The 10,000-page reader path measured one page and read 294,047 bytes in 198 reads before the viewport was complete. It uses an in-memory `bytes.Reader` to measure the callback path.

On the 15-page document, the synchronous gesture frame took 1.82/4.45 ms with bytes and 1.70/4.45 ms with `io.ReaderAt`, based on 12 runs of 12 gestures per input. `BenchmarkZoom10000Pages` measured 304.7 ns/op at the median of five runs and allocated zero bytes per operation. Each operation applies two zoom changes. These measurements cover headless rendering and process memory. Native window timing and GPU memory are outside their scope.

## Run the benchmarks

Run these commands from the repository root on macOS. Keep other builds and tests idle during comparisons.

```sh
mise run resources
go test -c -o /tmp/mygo-pdf-performance.test ./pdf
(
  cd pdf
  MYGO_PDF_REQUIRE_INTEGRATION=1 MYGO_PDF_MEASURE=open \
    /usr/bin/time -l /tmp/mygo-pdf-performance.test -test.v \
    -test.run '^TestViewerPerformance$/^ordinary$'
)
go test -run '^$' -bench '^BenchmarkZoom10000Pages$' -benchmem -count=5 ./pdf
```

Choose `ordinary`, `mixed`, `scan`, `long` or `long-mixed` in the test filter. Set `MYGO_PDF_MEASURE_SOURCE=reader` for `io.ReaderAt`. Set `MYGO_PDF_MEASURE=gesture` for the 12-step pinch and scroll workload or `MYGO_PDF_MEASURE=native` to time PDFium's custom open and page count. Timings in the test log are in nanoseconds. Repeat cold opens in fresh processes and use the same inputs, hardware and toolchain when comparing results. [`TestViewerPerformance`](../../pdf/performance_test.go)
