# MyGo PDF design

[简体中文](../zh-CN/design.md)

The viewer owns document display and navigation. The host supplies PDF bytes or a stable random-access source and owns file monitoring, path authorization, document generation, persistence, export and external opening. See the [integration guide](usage.md) for the host API and the [roadmap](plan.md) for upcoming work.

## Packages

| Location | Responsibility |
| --- | --- |
| [`pdf`](../../pdf) | Public API, window-thread state, MyGo view and input, bitmap caches and worker scheduling |
| [`internal/layout`](../../internal/layout) | Page size indexing, viewport geometry and reading positions |
| [`internal/pdfium`](../../internal/pdfium) | PDFium loading, platform ABI, native handles, rasterization and metadata extraction |
| [`internal/render`](../../internal/render) | Shared page dimensions, raster keys, whole-page resolution limits and cache budgets |
| [`cmd/pdfium-resources`](../../cmd/pdfium-resources) | Verify and prepare the native libraries and notices |
| [`examples/viewer`](../../examples/viewer) | Host-side file opening, toolbar composition and embedded preview |

The public package uses the three internal packages. `internal/layout` and `internal/pdfium` share types from `internal/render` and have no MyGo dependency.

## Window and worker

Each viewer belongs to one window. The UI thread owns the viewport, current generation, caches and callbacks. View builds submit demand for the current viewport. The worker reads the document and renders pixels off the UI thread, then publishes results through `Window.Update`. The UI checks the request or displayed generation and, for rendering, the current raster demand before applying the result. `OnChange` runs after state is committed, and a callback can clear, close or reload the viewer.

A refresh prepares the visible pages of a candidate document before replacing the displayed layout and bitmaps together. A previously measured view checks its current geometry before committing a refresh. An unsuccessful refresh retains the displayed document. For an explicit page selection, the reported page remains selected until the vertical scroll offset changes. The viewport center then determines the page, with the nearest page selected when the center falls in a gap.

## PDFium lifetime and scheduling

PDFium calls are serialized within the process. Bytes stay pinned while the native document can read them. Reader-backed documents share a purego callback and use a registered source token. The callback reads into a temporary native buffer, and read errors belong to the operation that triggered them. Page and text handles are released before the document handle. Closing the document precedes unpinning the bytes or signaling that a reader can be released. `Close` stops accepting results and returns immediately. `Done` signals that the worker has released the native document.

Progressive rendering yields at PDFium pause points and checks cancellation before resuming. Visible work has priority over prefetch across windows. A canceled prefetch waiting to resume joins the foreground queue in arrival order to release its native handles. The process allows four active native rasters, with one slot reserved for visible work. An in-flight native call must return before its resources can be released. A library override without progressive symbols uses synchronous rendering.

## Page geometry and rendering

Byte-backed documents measure all page dimensions before opening. Reader-backed documents measure the opening viewport and estimate the other dimensions until they are read. A segment tree tracks page extents and fit maxima. Explicit navigation and visible pages get priority, then the neighboring page is measured and prefetched before the remaining scan. Updates retain the reading anchor. Each page raster uses exact dimensions.

The viewer renders missing visible-page bitmaps first, then services visible text and link geometry, visible image refinements and a preview in the scroll direction. Existing bitmaps remain visible during zoom. Nearby tiles are retained within one tile of the viewport and drawn when their keys match the current demand. The whole-page bitmap, tile and text and link geometry caches have separate budgets of 24 MiB, 40 MiB and 8 MiB. Document bytes, in-flight bitmaps and native memory are accounted for separately. The [performance report](performance.md) documents the workloads and measurements.

The layout, selection, links, destinations and search highlights share page coordinates. Viewer rotation is relative to the intrinsic orientation and uses clockwise quarter turns. A new document resets that rotation, while refresh preserves it. Existing page previews rotate while matching rasters are prepared.

## Navigation and accessibility

Internal destinations support `XYZ`, `Fit`, `FitH`, `FitV`, `FitR`, `FitB`, `FitBH` and `FitBV`. Destination zoom follows the viewer's zoom limits. Bounding-box modes estimate the content extent within the crop box, using the cropped page when bounds are unavailable or empty. An explicit null `FitH` or `FitV` coordinate is treated as zero because PDFium exposes a numeric value. In `XYZ`, omitted axes or zoom retain the corresponding relative position or scale.

Outline entries are scoped to the viewer and displayed generation. Search navigates to one match at a time and wraps around the document. New loads cancel outline and search work. Navigation to an unmeasured page waits for its size. A failed size read is reported through `State.Err` for a page or outline jump, or through `State.Find.Err` with the active match cleared for a search.

Visible pages expose their page number, up to 8 KiB of accessible text per page where permitted, and focusable links. Keyboard and assistive activation share the pointer link routing. Text extraction follows PDFium's ordering and permission flags, and search highlights keep ordinary copying subject to those permissions.

## Native resources

The [plugin manifest](../../internal/pdfium/mygo-plugin.json) lives next to its PDFium bindings. MyGo discovers it through the host's imports and verifies the library and notices before bundling them. The viewer loads the bundled library at runtime. See [running the host](usage.md#run-the-host) and the [PDFium asset update procedure](development.md#update-pdfium-assets).

## Design references

- MyGo's upstream PDF proposal informed the viewer lifecycle and preview behavior. [MyGo PDF proposal](https://github.com/egoist/mygo/pull/124)
- PDFium's progressive interface provides pause points for native rendering. pdfrx shows the use of a controller for page navigation, zoom and viewport state. [Progressive rendering API](https://pdfium.googlesource.com/pdfium/+/refs/heads/chromium/8179/public/fpdf_progressive.h) · [pdfrx controller](https://github.com/espresso3389/pdfrx/blob/5476a917729fade064e0d8af279554db72644c11/packages/pdfrx/lib/src/widgets/pdf_viewer.dart#L6360)
- PDF.js reuses the current image during zoom. React-PDF leaves composition and controls to the host. [PDF.js zoom](https://github.com/mozilla/pdf.js/blob/3471ae1691e1d08cbe7ae2a751259bbfdda3acf1/web/pdf_viewer.js#L1605) · [PDF.js rendering queue](https://github.com/mozilla/pdf.js/blob/3471ae1691e1d08cbe7ae2a751259bbfdda3acf1/web/pdf_rendering_queue.js#L104) · [React-PDF Page API](https://github.com/wojtekmaj/react-pdf/blob/bb19ba0dd2e5cfe3d3a813f104811d0ff50c9203/packages/react-pdf/README.md#page)
