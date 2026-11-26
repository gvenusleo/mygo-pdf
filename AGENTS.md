# MyGo PDF development guidelines

MyGo PDF is a read-only desktop viewer built with Go, MyGo Native UI and PDFium. See the [integration guide](docs/en/usage.md) for host APIs, [design](docs/en/design.md) for component contracts and the [roadmap](docs/en/plan.md) for upcoming work.

## Project language

Write all project-authored material in English. This includes source comments, Go documentation, default UI text, command help, errors, logs, test messages, configuration comments, commit messages, pull requests, and release titles and notes. Keep English as the source for documentation. Maintain `README.zh-CN.md` alongside `README.md`, and mirror the files in `docs/en/` under `docs/zh-CN/` with the same filenames. Link both language versions near the top of each page and update the translation in the same change as its English source. Explicitly selected UI translations and test fixtures may contain text in the language they exercise.

## Principles

- Prefer the standard library and existing MyGo components. Add abstractions, options, and dependencies when a caller needs them.
- Update the design and agree on the direction before changing the public API, input ownership, shutdown or refresh behavior, supported platforms, or packaging. Fix deviations from an existing contract against the documented behavior.
- The component loads PDF bytes and local random-access sources, displays pages, and handles navigation. The host owns file monitoring, path authorization, compilation, export, and external opening policy. The library is read-only.
- Keep API behavior and Go documentation in sync. Verify changes through the standalone example and keep host-specific types outside the component.

## Packages and code

- The public `pdf` package in `pdf/` owns the viewer API, MyGo view, input, UI-thread state, cache, and worker scheduling. `internal/layout` computes page geometry and viewport offsets without UI or FFI dependencies. `internal/pdfium` owns library loading, ABI bindings, native handles, rasterization, and text extraction. `internal/render` defines shared page dimensions, raster requests, and budgets.
- `cmd/pdfium-resources` prepares test libraries and plugin release assets. `internal/pdfium/mygo-plugin.json` declares the released libraries and license notices alongside the package that loads them. `examples/viewer` demonstrates the public API. Core packages do not depend on the commands or example.
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

The [development guide](docs/en/development.md) lists the local commands. Run `mise run ci` for formatting, vet and native integration checks. Use `mise run dev` to exercise the example and `mise exec -- gofmt -w <file>` to format edited Go files. Follow the [release procedure](docs/en/development.md#release-procedure) when preparing a tag.

For FFI or packaging changes, verify the affected ABI, library checksums, license notices, and installed resource paths. Check document links and punctuation. Use English Conventional Commits. Commit, push, and publish only with explicit authorization.
