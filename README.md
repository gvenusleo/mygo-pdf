# MyGo PDF

[简体中文](README.zh-CN.md)

A native PDF viewer for [MyGo](https://github.com/egoist/mygo) desktop apps. It displays continuous pages, zooms around the pointer, renders visible detail at high DPI and supports text selection, copy and links. Refresh a document in place to preserve the reading position while the new visible pages render.

The viewer uses PDFium via purego. Supported build targets are macOS 13 or later, Windows and glibc Linux on amd64 and arm64. This is a third-party MyGo component.

## Run the example

```sh
mise trust
mise run dev
```

The sample PDF is bundled. Use the toolbar to open a local PDF, change page or fit, and use `Cmd`/`Ctrl`+`P` to toggle the toolbar. The viewer itself works without a toolbar. The example's sample PDF can be regenerated with `typst compile testdata/sample.typ examples/viewer/sample.pdf`.

## Embed in a MyGo app

Create `pdf.NewViewer(window, pdf.Options{})` on the UI thread. Call `viewer.View(c.Key("pdf"), layoutKey)` from the window's `ui.View`. Use `nil` for the layout key when the parent layout is fixed. Supply a comparable value that changes when the parent rearranges the viewer.

Call `LoadBytes(ctx, data, pdf.OpenDocument)` with an immutable byte slice to display a new document. Use `pdf.RefreshDocument` when updating the same document and retaining the reading position. If the host temporarily hides a previously displayed viewer, the refresh waits for the view to return or for cancellation. If you read a file, do that in the background and call `LoadBytes` on the UI thread after checking that the result still belongs to the current request.

`State` and `OnChange` report loading, visible rendering, errors, page number and zoom. `GoToPage`, `SetZoom` and `SetFit` let the host supply its own controls. Use `Options.OnLink` to handle validated external URLs and `Options.OpenExternal` to offer a fallback action. `Options.CanOpenExternal` lets the host show that action for selected documents. `ChineseLabels()` supplies Simplified Chinese menu labels. Window closure stops the viewer. When removing it earlier, call `Close` on the UI thread and use `Done` to wait for disposal from outside that thread.

### PDFium resources

With MyGo `v0.10.0` or later, run `mygo dev` or `mygo build` from the host app. MyGo reads the plugin manifest, downloads the native library for the build target and the license notices, verifies their SHA-256 checksums, and packages them with the app. The viewer loads the bundled library at runtime. For direct `go run` or custom packaging, set `Options.LibraryPath` to an absolute library path.

The plugin maintainer prepares release assets from the pinned upstream archives. Run `go run ./cmd/pdfium-resources -release=<tag>` to generate the assets and manifest in `build/pdfium-release`. Copy the generated manifest into the module root and publish its referenced assets with that tag.

## Verification

`mise run ci` checks Go formatting, Go and MyGo vet, race tests and builds/tests with cgo disabled. The task prepares a verified native PDFium library before running the integration tests. Use `mise run build` for the desktop example package.

See the [design](docs/design.md) for component contracts and the [development plan](docs/plan.md) for milestones and release criteria.

## License

[MIT](LICENSE). Portions based on MyGo are covered by the [MyGo contributor license](resources/pdfium-mygo-LICENSE.txt). PDFium and third-party notices are installed with the native library.
