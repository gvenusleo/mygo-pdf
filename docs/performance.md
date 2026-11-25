# MyGo PDF performance baseline

Measured on an Apple M3 running macOS 27.2.1 with Go 1.27.4, MyGo v0.10.0, and PDFium chromium/8179. The reference build is `v0.1.1` (`7239d35`). The pre-change branch is `62025b3`.

The opt-in `TestViewerPerformance` in `performance_test.go` uses the same generated documents and 900 × 700 DIP headless viewport at 2× raster scale on all three builds. A cold-open sample starts a fresh process. Each gesture run performs 12 pinch and scroll steps, waiting for visible rendering to settle after each step. Open timings start immediately before `LoadBytes`. Gesture frame timing covers delivery of the input and its synchronous MyGo Tester frame. Peak RSS comes from `/usr/bin/time -l` on macOS and includes the test process. Run the timings in isolation from build and test jobs.

```sh
mise run resources
go test -c -o /tmp/mygo-pdf-performance.test .
MYGO_PDF_REQUIRE_INTEGRATION=1 MYGO_PDF_MEASURE=open \
  /usr/bin/time -l /tmp/mygo-pdf-performance.test -test.v \
  -test.run '^TestViewerPerformance$/^ordinary$'
go test -run '^$' -bench '^BenchmarkZoom10000Pages$' -benchmem -count=10 .
```

Replace `open` with `gesture` for gesture runs. The fixture names are `ordinary`, `mixed`, `scan`, and `long`. To run the same test against the older revisions, use a temporary checkout, copy `performance_test.go`, and copy the `encodePDF` helper from `viewer_test.go` into a test file in that checkout. For the older zoom benchmark, copy only `BenchmarkZoom10000Pages` into a test file because the anchor tests use the new layout geometry.

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
