# MyGo PDF 性能实测

[English](../en/performance.md)

## 当前实测结果

测试使用当前查看器源码（`99231ad`）。环境为 Apple M3、macOS 27.2.1、Go 1.27.4、MyGo v0.10.0 和 PDFium chromium/8179。无窗口的 MyGo Tester 视口为 900 × 700 DIP，光栅缩放为 2 倍。每行报告 30 个独立进程的 P50/P95。时间单位为毫秒，进程峰值 RSS 单位为 MiB。

| 文档 | 输入 | 完整首屏 | 渲染就绪 | 峰值 RSS |
| --- | --- | --- | --- | --- |
| 15 页 | 字节 | 18.41/23.40 | 51.54/66.32 | 49.97/50.12 |
| 15 页 | `io.ReaderAt` | 18.23/18.34 | 50.90/51.41 | 46.00/49.73 |
| 10,000 页 | 字节 | 52.58/56.50 | 85.14/92.23 | 63.59/64.09 |
| 10,000 页 | `io.ReaderAt` | 20.26/24.38 | 52.83/59.93 | 50.86/51.41 |

完整首屏时间截至第一组位图覆盖所有可见页，渲染就绪时间截至 `RenderReady`。万页文档使用读取器时，完整首屏之前只测量了 1 页尺寸，共读取 198 次、294,047 字节。读取器测试用内存中的 `bytes.Reader` 测量回调路径。

15 页文档的同步手势帧耗时，字节输入为 1.82/4.45 ms，`io.ReaderAt` 为 1.70/4.45 ms。每种输入测量 12 轮，每轮 12 次手势。`BenchmarkZoom10000Pages` 五轮中位数为 304.7 ns/op，每次操作包含两次缩放，分配零字节。这些结果覆盖无窗口渲染和进程内存。原生窗口时序和 GPU 内存需要单独测量。

## 运行基准

在 macOS 上从仓库根目录运行以下命令。做对比时，先等其他构建和测试任务结束。

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

测试文档可选 `ordinary`、`mixed`、`scan`、`long` 或 `long-mixed`。设置 `MYGO_PDF_MEASURE_SOURCE=reader` 可通过 `io.ReaderAt` 打开。设置 `MYGO_PDF_MEASURE=gesture` 可运行 12 步捏合和滚动测试，设置 `MYGO_PDF_MEASURE=native` 可单独测量 PDFium 的自定义打开和页数查询。测试日志中的时间单位为纳秒。每次冷打开使用新的进程，对比时使用相同的输入、硬件和工具链。[`TestViewerPerformance`](../../pdf/performance_test.go)
