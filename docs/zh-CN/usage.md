# 在应用中嵌入 MyGo PDF

[English](../en/usage.md)

## 添加依赖

在 MyGo 应用的模块目录中添加 MyGo PDF 依赖。继续沿用应用的 `mygo.json` 和构建命令。

```sh
go get github.com/gvenusleo/mygo-pdf/pdf@latest
```

## 嵌入查看器

在所属窗口的 UI 线程创建一次查看器，并将其保存在宿主的窗口状态中。下方示例接收宿主已取得的 PDF 字节。在应用初始化窗口时调用 `newPreview`，例如在 `App.WhenReady` 回调中调用。

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

嵌入已有窗口时，将 `Viewer` 保存在该窗口的状态中，在对应的窗格内调用 `View`。`c.Key("pdf")` 为视图提供稳定标识。父布局固定时传 `nil`，宿主会调整窗格布局时传一个随布局变化的可比较值。

## 运行宿主应用

照常运行 `mygo dev` 或 `mygo build`。MyGo 沿 Go 的导入关系找到[插件清单](../../internal/pdfium/mygo-plugin.json)，选择并校验目标平台的 PDFium 动态库，再随宿主打包动态库及许可声明。导入包即可完成接入。

直接使用 `go run` 或自行打包时，通过 `Options.LibraryPath` 指定原生动态库的绝对路径。自行分发动态库时，请附带许可声明。

## 加载和刷新 PDF

| 输入 | API 与生命周期 |
| --- | --- |
| 已在内存中的字节 | 向 `LoadBytes` 传入不可变切片。之后更新文档时使用新的切片。 |
| 内容稳定的本地文件或快照 | 在 UI 线程之外打开并获取文件大小，在 UI 线程调用 `LoadReaderAt`。保持文件内容稳定且可读，直到返回的释放通道关闭。请在后台 goroutine 等待通道并关闭文件。 |

切换文档时使用 `OpenDocument`，更新同一份文档时使用 `RefreshDocument`。刷新会保留旧视口，直到新画面准备好。刷新失败或取消时，当前文档会保留。如果其他进程会原地重写文件，请使用快照或不可变字节。每份文档最多 256 MiB、100,000 页。

用 `errors.Is(err, pdf.ErrPassword)` 检查 `State.Err` 是否为密码错误。由宿主提示输入密码，再通过 `LoadOptions.Password` 重试。字节输入用 `LoadBytesWithOptions`，文件输入重新打开读取源后调用 `LoadReaderAt`。[打开文件和重试示例](../../examples/viewer/open.go)

## 添加宿主控件

查看器绘制可滚动的 PDF，并处理选字、链接、右键菜单和无障碍。工具栏、搜索框和目录栏由宿主提供。通过 `OnChange` 订阅 `State` 更新，让控件与查看器保持同步。回调和取消订阅函数都在所属窗口的 UI 线程执行。页码从 1 开始，空查看器返回 0。

| 控件 | 查看器 API |
| --- | --- |
| 页码、缩放、适配和旋转 | `GoToPage`、`SetZoom`、`SetFit`、`SetRotation` |
| 书签 | `ReadOutline` 在 UI 线程回调 `OutlineResult`。宿主渲染 `Entries`，再把选中的条目传回 `GoToOutline`。 |
| 搜索 | `Find` 跳到下一处或上一处匹配。通过 `State.Find` 读取进度和结果，通过 `ClearFind` 清除高亮。 |
| 外部操作 | `Options.OnLink` 处理经过校验的外部链接。用 `Options.OpenExternal` 和 `Options.CanOpenExternal` 提供打开原文件的操作。 |

可以设置 `Options.Labels`，或用 `ChineseLabels()` 定制查看器自带的标签。窗口关闭时，查看器也会关闭。要提前移除查看器，请在 UI 线程调用 `Close`。如需确认原生资源已经释放，请在 UI 线程之外等待 `Done`。

[独立示例](../../examples/viewer)展示了带文件选择、工具栏、书签、搜索和密码提示的完整宿主。
