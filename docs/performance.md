# MyGo PDF performance baseline

Measured on an Apple M3 running macOS 27.2.1 with Go 1.27.4, MyGo v0.10.0, and PDFium chromium/8179. The reference build is `v0.1.1` (`7239d35`). The pre-change branch is `62025b3`.

The opt-in `TestViewerPerformance` in [`pdf/performance_test.go`](../pdf/performance_test.go) uses the same generated documents and 900 × 700 DIP headless viewport at 2× raster scale on all three builds. A cold-open sample starts a fresh process. Each gesture run performs 12 pinch and scroll steps, waiting for visible rendering to settle after each step. Open timings start immediately before `LoadBytes`. Gesture frame timing covers delivery of the input and its synchronous MyGo Tester frame. Peak RSS comes from `/usr/bin/time -l` on macOS and includes the test process. Run the timings in isolation from build and test jobs.

```sh
mise run resources
go test -c -o /tmp/mygo-pdf-performance.test ./pdf
(
  cd pdf
  MYGO_PDF_REQUIRE_INTEGRATION=1 MYGO_PDF_MEASURE=open \
    /usr/bin/time -l /tmp/mygo-pdf-performance.test -test.v \
    -test.run '^TestViewerPerformance$/^ordinary$'
)
go test -run '^$' -bench '^BenchmarkZoom10000Pages$' -benchmem -count=10 ./pdf
```

Replace `open` with `gesture` for gesture runs. The byte-backed fixture names are `ordinary`, `mixed`, `scan`, and `long`. For v0.2.1, run all commands from that tag's repository root and pass `.` as the Go test package. For the historical v0.1.1 and 62025b3 comparisons, restore the harness from `git show v0.2.1:performance_test.go` in a temporary checkout and copy the `encodePDF` helper from v0.2.1's `viewer_test.go` into a test file there. For the historical zoom benchmark, copy only `BenchmarkZoom10000Pages` from v0.2.1's `layout_test.go`.

## Measurements

Each row reports P50/P95 from 30 fresh opens and three gesture runs. Times are in milliseconds and RSS is in MiB. The first bitmap and full viewport are usually observed together because the initial load installs its first visible bitmaps as one batch.

| Fixture | Build | First bitmap | Full viewport | Open sharp | Open RSS | Gesture frame | Gesture sharp | Gesture RSS |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Ordinary | v0.1.1 | 19.11/23.01 | 19.11/23.01 | 49.21/57.87 | 49.52/49.73 | 4.59/5.94 | 18.01/33.37 | 79.02/79.28 |
| Ordinary | 62025b3 | 19.11/22.30 | 19.11/22.30 | 49.50/58.27 | 49.48/49.86 | 4.80/5.90 | 17.97/34.05 | 76.59/78.92 |
| Ordinary | v0.2 candidate | 19.26/23.11 | 19.26/23.11 | 53.19/66.11 | 49.71/49.91 | 1.81/4.49 | 1.81/31.00 | 75.55/75.64 |
| Mixed sizes | v0.1.1 | 16.32/17.91 | 16.32/17.91 | 28.59/31.12 | 49.88/50.19 | 3.78/3.87 | 20.86/41.62 | 99.19/99.44 |
| Mixed sizes | 62025b3 | 16.37/17.45 | 16.37/17.45 | 28.65/30.26 | 49.86/50.06 | 3.79/5.67 | 22.49/46.70 | 99.19/102.70 |
| Mixed sizes | v0.2 candidate | 16.17/18.03 | 16.17/18.03 | 30.77/33.83 | 50.18/50.45 | 4.16/7.22 | 11.73/43.64 | 137.56/137.80 |
| Synthetic scan | v0.1.1 | 10.33/11.72 | 10.33/11.72 | 38.45/42.90 | 48.09/48.50 | 6.54/8.17 | 21.01/40.78 | 76.52/76.78 |
| Synthetic scan | 62025b3 | 10.34/12.99 | 10.34/12.99 | 38.25/45.82 | 48.15/48.48 | 6.92/11.51 | 22.94/44.66 | 74.58/76.97 |
| Synthetic scan | v0.2 candidate | 10.32/11.88 | 10.32/11.88 | 43.65/51.39 | 48.23/48.64 | 4.69/8.92 | 4.93/48.49 | 70.25/70.30 |
| 10,000 pages | v0.1.1 | 53.21/58.24 | 53.21/58.24 | 83.29/90.66 | 63.22/63.75 | 5.02/6.16 | 18.40/34.11 | 93.73/102.66 |
| 10,000 pages | 62025b3 | 53.36/58.53 | 53.36/58.54 | 83.38/92.59 | 63.02/63.45 | 5.55/8.96 | 19.58/38.53 | 102.05/102.27 |
| 10,000 pages | v0.2 candidate | 53.33/57.99 | 53.33/57.99 | 86.55/97.32 | 63.27/63.69 | 2.55/6.33 | 2.56/31.75 | 91.45/91.53 |

`BenchmarkZoom10000Pages` performs two zoom changes per operation. Both baselines allocated 327,680 bytes per operation, and the candidate allocated zero. Median operation times were 41,377 ns for v0.1.1, 42,485 ns for 62025b3, and 103 ns for the candidate.

The candidate meets the allocation target and both ordinary-document P95 targets. The ordinary first viewport was 100.4% of the release baseline and the gesture frame was 75.6%. The ordinary cold-open refinement reached P95 in 66.11 ms, up from 57.87 ms. Mixed-size gesture RSS rose to 137.80 MiB at P95 with nearby tile retention. These headless results measure CPU rendering. Native window timing and GPU memory were outside this measurement.

## v0.3.0 comparison

The same Apple M3, macOS 27.2.1, Go 1.27.4, MyGo v0.10.0, and PDFium chromium/8179 were used for the `v0.2.1` and `v0.3.0` builds. The opt-in harness interleaved builds, took 30 fresh opens and three gesture runs per fixture, and used nearest-rank percentiles. Reader runs use a measured `bytes.Reader` to isolate the callback path from filesystem cache behavior. The `long-mixed` fixture has 10,000 pages with late wide, tall, and cropped pages. Its byte-backed reference uses the candidate's eager loading path.

| Fixture | Source and build | First bitmap | Full viewport | Open sharp | Open RSS | Gesture frame | Gesture sharp | Gesture RSS |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Ordinary | v0.2.1 bytes | 18.21/20.72 | 18.21/20.72 | 50.98/60.81 | 49.67/49.91 | 1.94/4.66 | 1.94/30.84 | 75.58/75.88 |
| Ordinary | v0.3.0 bytes | 18.20/21.72 | 18.20/21.72 | 51.07/65.17 | 49.88/50.12 | 2.79/7.99 | 2.79/35.34 | 83.30/83.73 |
| Ordinary | v0.3.0 reader | 18.11/20.50 | 18.11/20.50 | 50.89/56.71 | 45.92/46.20 | 2.04/5.06 | 2.04/32.92 | 67.56/67.72 |
| Mixed sizes | v0.2.1 bytes | 15.69/17.37 | 15.69/17.37 | 30.00/33.43 | 50.20/50.41 | 3.67/4.23 | 11.25/38.03 | 137.55/137.70 |
| Mixed sizes | v0.3.0 bytes | 15.66/16.99 | 15.66/16.99 | 29.95/33.68 | 50.22/50.48 | 3.65/4.29 | 11.16/37.96 | 137.78/142.22 |
| Mixed sizes | v0.3.0 reader | 15.58/17.41 | 15.58/17.41 | 29.93/33.60 | 48.09/48.33 | 3.63/4.14 | 11.22/38.12 | 137.50/137.70 |
| Synthetic scan | v0.2.1 bytes | 10.08/11.86 | 10.08/11.86 | 42.80/49.68 | 48.09/48.50 | 4.06/7.81 | 4.07/43.82 | 69.77/70.41 |
| Synthetic scan | v0.3.0 bytes | 10.08/13.40 | 10.08/13.40 | 42.81/52.33 | 48.38/48.75 | 4.04/7.79 | 4.05/43.70 | 70.45/70.47 |
| Synthetic scan | v0.3.0 reader | 10.09/12.27 | 10.09/12.27 | 42.76/51.88 | 48.44/48.77 | 4.03/7.81 | 4.05/43.85 | 70.12/70.59 |
| 10,000 pages | v0.2.1 bytes | 52.44/57.15 | 52.44/57.15 | 85.30/95.46 | 63.30/64.08 | 1.70/4.46 | 1.70/30.67 | 91.81/92.06 |
| 10,000 pages | v0.3.0 bytes | 52.84/57.35 | 52.84/57.35 | 85.43/92.65 | 63.17/63.98 | 1.82/4.45 | 1.82/30.35 | 98.89/99.20 |
| 10,000 pages | v0.3.0 reader | 20.24/22.87 | 20.24/22.87 | 52.82/58.46 | 50.70/51.12 | 1.70/4.44 | 1.71/30.73 | 72.11/72.31 |
| 10,000 mixed pages | v0.3.0 bytes | 26.59/27.98 | 26.59/27.98 | 29.09/30.70 | 46.08/46.69 | 1.90/4.28 | 2.09/6.57 | 67.23/67.77 |
| 10,000 mixed pages | v0.3.0 reader | 6.83/10.20 | 6.83/10.20 | 38.38/44.93 | 47.30/47.81 | 1.57/4.34 | 1.57/29.57 | 69.08/69.17 |

The uniform long document reached a complete viewport at 40.0% of the paired v0.2.1 P95, with one explicit page-size probe and 198 reads totaling 294,047 bytes. The mixed long document used one probe and 192 reads totaling 291,330 bytes. Separate 30-process runs timed PDFium's custom open and page count at 1.90/2.71 ms for the uniform fixture and 1.90/1.97 ms for the mixed fixture. The mixed long fixture reached the first viewport earlier with the reader and took longer to reach sharp output as its dimensions settled.

Ordinary gesture runs were sensitive to a few slow frames. The first three-run set measured 7.60 ms P95 on the reader. The comparison above measured 7.99 ms on bytes. A larger isolated set of 12 runs per source measured 1.89/4.81 ms for v0.2.1, 1.85/4.66 ms for candidate bytes, and 1.71/4.46 ms for the reader. Both candidate paths met the 6.53 ms target in that set. Ordinary refinement on bytes still exceeds the original v0.1.1 result. Mixed-size gesture RSS rose by 3.3% on bytes compared with paired v0.2.1 and was equal on the reader. Whole-document zoom still allocates zero bytes per operation.

For reader runs, set `MYGO_PDF_MEASURE_SOURCE=reader`. To isolate the native open, set `MYGO_PDF_MEASURE=native`. Use `TestViewerPerformance/long-mixed` to select the new mixed-size case. The headless harness measures CPU rendering and process RSS.
