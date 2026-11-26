# MyGo PDF

[简体中文](README.zh-CN.md)

A native PDF viewer for [MyGo](https://github.com/egoist/mygo) desktop apps. It displays continuous pages, zooms around the pointer, renders visible detail at high DPI and supports text selection, copy and links. Refresh a document in place to preserve the reading position while the new visible pages render.

The viewer uses PDFium via purego. Supported build targets are macOS 13 or later, Windows and glibc Linux on amd64 and arm64. This is a third-party MyGo component.

## Run the example

```sh
mise trust
mise run dev
```

The sample PDF is bundled. The toolbar opens local files, navigates by page or outline, finds text in either direction, changes fit and zoom, and rotates the view. Use `Cmd`/`Ctrl`+`P` to toggle the toolbar. The viewer itself works without a toolbar. The example's sample PDF can be regenerated with `typst compile examples/viewer/sample.typ examples/viewer/sample.pdf`.

## Embed in a MyGo app

Import `github.com/gvenusleo/mygo-pdf/pdf`. Create `pdf.NewViewer(window, pdf.Options{})` on the UI thread. Call `viewer.View(c.Key("pdf"), layoutKey)` from the window's `ui.View`. Use `nil` for the layout key when the parent layout is fixed. Supply a comparable value that changes when the parent rearranges the viewer.

Call `LoadBytes(ctx, data, pdf.OpenDocument)` with an immutable byte slice to display a new document. Use `pdf.RefreshDocument` when updating the same document and retaining the reading position. If the host temporarily hides a previously displayed viewer, the refresh waits for the view to return or for cancellation. If you read a file, do that in the background and call `LoadBytes` on the UI thread after checking that the result still belongs to the current request.

For a stable local file, open it and obtain its size in the background. On the UI thread, call `LoadReaderAt(ctx, file, size, pdf.LoadOptions{})`. Wait for the returned release channel on a background goroutine before closing the file. The input must stay stable and readable even after the load finishes. Use immutable bytes or a file snapshot for files rewritten in place. Both APIs limit one document to 256 MiB and 100,000 pages.

If a load returns `ErrPassword` through `State.Err`, the host can prompt and retry with `LoadOptions.Password`. Use `LoadBytesWithOptions` for bytes and a fresh reader for a file retry. Each retry is a new request. Password handling belongs to the host.

`State` and `OnChange` report loading, visible rendering, errors, page number, zoom, rotation, and find status. `GoToPage`, `SetZoom`, `SetFit`, and `SetRotation` let the host compose its own controls. Use `ReadOutline` and `GoToOutline` for local bookmarks, and `Find` and `ClearFind` for cancelable navigation to matching text. Visible pages expose page numbers, permitted text, and keyboard links through MyGo accessibility. Use `Options.OnLink` to handle validated external URLs and `Options.OpenExternal` to offer a fallback action. `Options.CanOpenExternal` lets the host show that action for selected documents. `ChineseLabels()` supplies Simplified Chinese menu labels. Window closure stops the viewer. When removing it earlier, call `Close` on the UI thread and use `Done` to wait for disposal from outside that thread.

### PDFium resources

With MyGo `v0.10.0` or later, run `mygo dev` or `mygo build` from the host app. MyGo reads the plugin manifest, downloads the native library for the build target and the license notices, verifies their SHA-256 checksums, and packages them with the app. The viewer loads the bundled library at runtime. For direct `go run` or custom packaging, set `Options.LibraryPath` to an absolute library path.

The plugin manifest pins a published PDFium asset release. MyGo PDF versions can share the same six libraries and license notices. See the [release procedure](docs/plan.md#release-procedure) when updating those assets.

## Verification

`mise run ci` checks Go formatting, Go and MyGo vet, race tests and builds/tests with cgo disabled. The task prepares a verified native PDFium library before running the integration tests. Before tagging a release, start the [Release workflow](.github/workflows/release.yml) manually to run native PDFium tests on all six targets and verify the referenced assets. Pushing a matching `v*` tag publishes the GitHub Release after the workflow confirms that the tagged commit passed verification. Use `mise run build` for the desktop example package.

See the [design](docs/design.md) for component contracts and the [development plan](docs/plan.md) for milestones and release criteria.

## License

[MIT](LICENSE). Portions based on MyGo are covered by the [MyGo contributor license](resources/pdfium-mygo-LICENSE.txt). PDFium and third-party notices are installed with the native library.
