# Embed MyGo PDF

[简体中文](../zh-CN/usage.md)

## Add the package

From your MyGo application's module directory, add MyGo PDF as a Go dependency. Keep your existing `mygo.json` and build commands.

```sh
go get github.com/gvenusleo/mygo-pdf/pdf@latest
```

## Mount the viewer

Create the viewer once on its window's UI thread and keep it with the host's window state. The example below takes PDF bytes the host already has. Call `newPreview` from the application's window setup, such as its `App.WhenReady` callback.

```go
import (
    "context"

    "github.com/egoist/mygo"
    "github.com/egoist/mygo/ui"
    "github.com/gvenusleo/mygo-pdf/pdf"
)

func newPreview(ctx context.Context, data []byte) *mygo.Window {
    var viewer *pdf.Viewer
    window := mygo.NewWindow(mygo.WindowOptions{
        Title: "PDF Preview",
        Content: ui.View(func(c *ui.Context) {
            if viewer != nil {
                viewer.View(c.Key("pdf"), nil)
            }
        }),
    })
    viewer = pdf.NewViewer(window, pdf.Options{})
    viewer.LoadBytes(ctx, data, pdf.OpenDocument)
    return window
}
```

To embed in an existing window, keep the `Viewer` in that window's state and call `View` from the desired pane. `c.Key("pdf")` gives the view a stable identity. Pass `nil` for a fixed parent layout or a comparable value that changes when the host rearranges the pane.

## Run the host

Run `mygo dev` or `mygo build` as usual. The Go import activates discovery of the [plugin manifest](../../internal/pdfium/mygo-plugin.json). MyGo selects and verifies PDFium for the target, then bundles the library and its license notices with the host. Importing the package is the only registration step.

For direct `go run` or custom packaging, set `Options.LibraryPath` to the native library's absolute path. Include its notices when distributing the library yourself.

## Load and refresh a PDF

| Input | API and lifetime |
| --- | --- |
| Bytes already in memory | Pass an immutable slice to `LoadBytes`. Allocate new bytes for subsequent edits. |
| A stable local file or snapshot | Open it and get its size off the UI thread. Call `LoadReaderAt` on the UI thread and keep the source readable and stable until the returned release channel closes. Wait and close the file in a background goroutine. |

Use `OpenDocument` to switch documents and `RefreshDocument` for a new revision of the same document. Refresh preserves the old viewport until the replacement is ready. A failed or canceled refresh retains the displayed document. Use a snapshot or immutable bytes if another process rewrites the source in place. The limits are 256 MiB and 100,000 pages per document.

Watch `State.Err` for `errors.Is(err, pdf.ErrPassword)`. After the host prompts for a password, retry with `LoadOptions.Password` through `LoadBytesWithOptions` or with a freshly opened reader through `LoadReaderAt`. [File opening and retry example](../../examples/viewer/open.go)

## Build host controls

The viewer draws the scrollable PDF and handles selection, links, the context menu and accessibility. The host supplies its own toolbar, search field and outline panel. Subscribe with `OnChange` to keep controls in sync with `State`. Both the callback and unsubscribe run on the window's UI thread. Page numbers are one-based, with zero for an empty viewer.

| Control | Viewer API |
| --- | --- |
| Page, zoom, fit and rotation | `GoToPage`, `SetZoom`, `SetFit`, `SetRotation` |
| Bookmarks | `ReadOutline` returns an `OutlineResult` on the UI thread. Render its `Entries` in the host and pass the selected entry back to `GoToOutline`. |
| Search | `Find` navigates to the next or previous match. Read `State.Find` for progress and results, and call `ClearFind` to clear the highlight. |
| External actions | Use `Options.OnLink` for validated external URLs. Use `Options.OpenExternal` and `Options.CanOpenExternal` to offer opening the original file. |

Set `Options.Labels` or use `ChineseLabels()` to customize the viewer's built-in labels. The viewer closes with its window. To remove it earlier, call `Close` on the UI thread and wait on `Done` off that thread if you need to wait for native resources to be released.

See the [standalone example](../../examples/viewer) for a complete host with file selection, a toolbar, bookmarks, search and a password prompt.
