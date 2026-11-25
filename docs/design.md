# MyGo PDF design

MyGo PDF is a read-only PDF viewer for desktop applications. It supports continuous vertical scrolling, zoom, fit-to-width and fit-to-page, text selection and copying, and links. The host can build its own controls using the public API. See the [development plan](plan.md) for milestones and release criteria.

## Packages

| Location | Responsibility |
| --- | --- |
| [Root `pdf` package](../api.go) | Public API, UI-thread state, MyGo view and input, viewport layout, bitmap caching, and worker scheduling |
| [`internal/pdfium`](../internal/pdfium) | PDFium loading, platform ABI, native handles, rasterization, and text and link extraction |
| [`internal/render`](../internal/render) | Raster request keys, size limits, and cache budgets shared by the viewer and PDFium layers |
| [`cmd/pdfium-resources`](../cmd/pdfium-resources) | Verify upstream archives and libraries, and prepare the library and licenses for the host's build |
| [`examples/viewer`](../examples/viewer) | Demonstrate host-side file opening, toolbar composition, and embedded preview |

The root package depends on `internal/pdfium` and `internal/render`. The PDFium package uses `internal/render` and has no MyGo dependency. The host handles file reads, path authorization, monitoring, document generation, persistence, and export.

## Public contracts

- `NewViewer` binds to one MyGo window and tracks its display scale and lifetime. Call the API on the window's UI thread and build the view with a stable `c.Key`. Change the comparable layout argument to `View` when the parent layout changes.
- `LoadBytes` takes ownership of immutable bytes and accepts a cancellation context. It returns the request generation. Opening a new document clears the current document and starts on the first page. Refreshing keeps the existing image, zoom, and reading position until the new visible pages can be installed together. A previously measured viewer applies a successful refresh once its view is built with current geometry. If the host temporarily hides that view, the refresh can be canceled or can finish when the view returns.
- `State` returns a snapshot. `OnChange` coalesces notifications and invokes callbacks on the UI thread after committing state. Callbacks may clear, close, or start another load. Public page numbers start at 1, with 0 for an empty document. Opening a document or explicitly navigating to a page selects that page even if multiple pages fit in the viewport. Once the vertical scroll offset changes, the current page follows the viewport center or the nearest page when the center falls in a gap.
- `Close` returns immediately and stops accepting results. `Done` closes when the worker releases the native document. Wait off the UI thread. Menu labels default to English and can be overridden or supplied by `ChineseLabels`. The viewer handles internal destinations and passes validated `http`, `https`, and `mailto` URLs to the host's `OnLink`. Hosts that supply `OpenExternal` can use `CanOpenExternal` to decide whether to show that menu item for the current document.

## Rendering and resources

PDFium calls are serialized within the process, and document bytes stay pinned until the native document closes. The worker coalesces viewport requests and uses `Window.Update` to apply results that still match the current document generation and raster demand. Cancellation prevents stale results from reaching the viewer. A synchronous native call must return before its resources can be released.

The viewer builds only visible pages. During zoom, it displays existing bitmaps and renders high-resolution viewport tiles when a full-page raster would exceed the budget. It queues missing visible-page bitmaps before requesting detail tiles. The full-page and tile caches have budgets of 24 MiB and 40 MiB, respectively. Document bytes, in-flight bitmaps, and native memory are accounted for separately. One document can contain up to 256 MiB of input and 100,000 pages.

Build targets are macOS 13 or later, Windows, and glibc Linux on amd64 and arm64. `mygo-plugin.json` declares the matching PDFium library and a shared file with all license notices. MyGo fetches and verifies both assets during development and packaging, and places them at the root of the app's resource directory. At runtime the viewer loads the bundled library without network access. An absolute `LibraryPath` override supports custom packaging. Track build coverage, PDFium integration tests, native input, and installer verification separately for each platform.
