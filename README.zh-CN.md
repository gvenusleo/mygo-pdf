# MyGo PDF

[English](README.md)

MyGo PDF 是面向 [MyGo](https://github.com/egoist/mygo) 桌面应用的第三方 PDF 预览组件。支持连续分页、围绕指针缩放、可见区高清渲染、选字复制和链接跳转。同一份文档刷新时，保留阅读位置和旧画面，直到新文档的可见页准备好。

组件通过 purego 调用 PDFium。构建支持 macOS 13 或更新版本、Windows 和 glibc Linux，均支持 amd64 与 arm64。

## 运行示例

```sh
mise trust
mise run dev
```

示例自带一份 PDF。可以用工具栏打开本地文件、切换页码或适配模式，按 `Cmd` 或 `Ctrl` 加 `P` 切换工具栏。查看器可独立嵌入，也可搭配宿主自己的控件。需要重新生成样例 PDF 时，运行 `typst compile testdata/sample.typ examples/viewer/sample.pdf`。

## 接入 MyGo 应用

在窗口主线程调用 `pdf.NewViewer(window, pdf.Options{})`。在窗口的 `ui.View` 中调用 `viewer.View(c.Key("pdf"), layoutKey)`。如果父布局固定，`layoutKey` 可传 `nil`。如果父布局会变化，传入一个随布局更新的可比较值。

用 `LoadBytes(ctx, data, pdf.OpenDocument)` 打开新文档。更新同一份文档且需要保留阅读位置时，使用 `pdf.RefreshDocument`。如果宿主暂时隐藏已经显示过的查看器，刷新会等到视图重新出现或请求被取消。传入的字节由查看器持有，调用方随后不得修改。读取文件应在后台进行，回到主线程确认结果仍对应当前请求后再调用 `LoadBytes`。

通过 `State` 和 `OnChange` 获取加载状态、可见区渲染状态、错误、页码和缩放倍率。宿主可用 `GoToPage`、`SetZoom` 和 `SetFit` 组装自己的工具栏。`Options.OnLink` 处理经校验的外部链接。`Options.OpenExternal` 添加系统打开入口，宿主可用 `Options.CanOpenExternal` 控制哪些文档显示此入口。`ChineseLabels()` 提供简体中文菜单。关闭窗口时查看器会自动停止。需要提前移除时，在主线程调用 `Close`，在 UI 线程之外等待 `Done`。

### 准备 PDFium 资源

使用 MyGo `v0.10.0` 或更高版本时，宿主直接运行 `mygo dev` 或 `mygo build`。MyGo 读取插件清单，按目标下载动态库和许可证，校验 SHA-256 后随应用打包。查看器运行时加载包内的库。直接使用 `go run` 或自定义打包时，可通过 `Options.LibraryPath` 指定动态库的绝对路径。

插件维护者从固定的上游归档准备发布资源。运行 `go run ./cmd/pdfium-resources -release=<tag>`，会在 `build/pdfium-release` 生成资源和清单。将生成的清单复制到模块根目录，并用对应标签发布其中引用的资源。

## 验证

`mise run ci` 执行格式和静态检查、真实 PDFium 竞态测试及无 cgo 测试。用 `mise run build` 构建桌面示例安装包。

组件契约见 [设计文档](docs/design.md)，版本安排和验收标准见 [开发计划](docs/plan.md)。

## 许可

项目采用 [MIT 许可证](LICENSE)。源自 MyGo 的部分保留 [MyGo 贡献者的许可声明](resources/pdfium-mygo-LICENSE.txt)。PDFium 和第三方许可随动态库分发。
