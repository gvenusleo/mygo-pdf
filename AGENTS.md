# MyGo PDF development guidelines

MyGo PDF is a read-only desktop viewer built with Go, MyGo Native UI, and PDFium. See the [design](docs/design.md) for the current contracts and the [development plan](docs/plan.md) for upcoming work.

## Project language

Write all project-authored material in English. This includes source comments, Go documentation, default UI text, command help, errors, logs, test messages, configuration comments, commit messages, pull requests, and release titles and notes. Keep the canonical documentation in English. Selected documents may have separate, locale-suffixed Chinese translations, such as `README.zh-CN.md`. Explicitly selected UI translations and test fixtures may contain text in the language they exercise.

## Principles

- Prefer the standard library and existing MyGo components. Add abstractions, options, and dependencies when a caller needs them.
- Update the design and agree on the direction before changing the public API, input ownership, shutdown or refresh behavior, supported platforms, or packaging. Fix deviations from an existing contract against the documented behavior.
- The component loads PDF bytes and local random-access sources, displays pages, and handles navigation. The host owns file monitoring, path authorization, compilation, export, and external opening policy. The library is read-only.
- Keep API behavior and Go documentation in sync. Verify changes through the standalone example and keep host-specific types outside the component.

## Packages and code

- The root `pdf` package owns the viewer API, MyGo view, input, layout, cache, and worker scheduling. `internal/pdfium` owns library loading, ABI bindings, native handles, rasterization, and text extraction. `internal/render` defines raster requests and budgets and has no UI or FFI dependencies.
- `cmd/pdfium-resources` prepares test libraries and plugin release assets. `mygo-plugin.json` declares the released libraries and license notices for host builds. `examples/viewer` demonstrates the public API. Core packages do not depend on the commands or example.
- Explain constraints and reasons in comments. Organize files by responsibility and aim for functions of 60 lines or fewer.
- Consult `use-modern-go` before editing Go and `mygo-maintenance` for MyGo UI or lifecycle changes when those skills are available. Read files before editing and keep changes scoped to the task.

## Lifecycle and rendering

- Read and write UI state on the window thread. Pass snapshots to workers and publish results through `Window.Update`. Use stable `c.Key` values. Keep state, handles, or services across builds. `ui.Element`, builder parts, and `*ui.Context` are scoped to view construction.
- `LoadBytes` takes ownership of immutable input. Keep its address pinned while PDFium can read it. `LoadReaderAt` borrows a stable source. The host may close it after the release channel closes. Release page and text handles before the document handle, then unpin bytes or release the reader. Serialize all PDFium calls within the process.
- Check cancellation before starting a load. Before applying a result, check the request generation, document generation, and current viewport demand. Prepare the visible pages for a refresh, then replace the layout and images together.
- Deliver `OnChange` after committing viewer state. Callback code may clear, close, or reload the viewer. `Close` is idempotent and returns immediately. `Done` signals that the worker has released the document. Wait for it off the UI thread.
- Use MyGo native components and theme values. Run slow I/O and native rendering in the background. When changing scheduling or caching, verify visible-page priority, cancellation, and memory budgets.

## Verification and delivery

Keep tests that prevent specific regressions in generation handling, cancellation, reentrancy, resource disposal, or multi-page refresh. Drive asynchronous tests with controlled results or state signals. For reproducible regressions, verify that the test fails against the old behavior. Check visual changes and input with `ui.NewTester` or real windows, and report the platforms exercised.

| Command | Purpose |
| --- | --- |
| `mise run dev` | Start the example with live reload using the plugin manifest |
| `mise run resources` | Prepare and verify PDFium for local integration tests |
| `mise run ci` | Check formatting, run Go and MyGo vet, and test with PDFium, the race detector, and cgo disabled |
| `mise run build` | Package the example and its declared PDFium assets |
| `mise exec -- gofmt -w <file>` | Format the edited Go file |

For FFI or packaging changes, verify the affected ABI, library checksums, license notices, and installed resource paths. Keep README translations in sync with the English instructions and check document links and punctuation. Use English Conventional Commits. Commit, push, and publish only with explicit authorization.
