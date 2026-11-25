# MyGo PDF 开发计划

MyGo PDF 是面向 MyGo 桌面应用的第三方 PDF 组件。首版使用 Go `1.27.4`、MyGo `v0.10.0` 和 PDFium `chromium/8179`。当前契约和包职责见 [设计文档](design.md)。

## 产品目标

同一个组件支持实时预览和独立阅读。编辑器可连续更新 PDF 并保留阅读位置和画面。桌面应用可在查看器外组装阅读控件。示例应用展示带工具栏与无工具栏的用法。

首版覆盖本地 PDF 和宿主生成的内存 PDF，目标为 macOS、Windows、glibc Linux 的 amd64 与 arm64。后续加入搜索、目录、密码和按需读取。移动端、Web、远程加载、PDF 编辑、交互表单、打印和 OCR 暂不纳入 `v1.0`。

## 技术路线

查看器采用连续分页、可见区分块和原子换版。生命周期设计参考 MyGo 上游 PDF 提案。首版只公开 `pdf` 包，原生句柄和 PDFium 类型留在内部。宿主负责文件读取、路径校验、文档生成和系统打开原文件。 [MyGo 上游 PDF 提案](https://github.com/egoist/mygo/pull/124)

- **pdfrx** 的控制器提供跳页、缩放、适配和可见区状态。渲染调度可借鉴其 PDFium 渐进接口，在原生可暂停点交还执行权。进程内保持 PDFium 调用串行，复杂页仍可能超过一次时间片。 [pdfrx 控制器](https://github.com/espresso3389/pdfrx/blob/5476a917729fade064e0d8af279554db72644c11/packages/pdfrx/lib/src/widgets/pdf_viewer.dart#L6360) · [PDFium 渐进渲染接口](https://pdfium.googlesource.com/pdfium/+/refs/heads/chromium/8179/public/fpdf_progressive.h)
- **PDF.js** 的缩放先变换已有画面。可见页缺图时先补整页回退图，再补高清区，最后按滚动方向预取一页。高清块可保留视口四周最多一块的余量，优先淘汰远离视口的块。页间缩放保留锚点到页边的距离。 [PDF.js 缩放](https://github.com/mozilla/pdf.js/blob/3471ae1691e1d08cbe7ae2a751259bbfdda3acf1/web/pdf_viewer.js#L1605) · [PDF.js 渲染队列](https://github.com/mozilla/pdf.js/blob/3471ae1691e1d08cbe7ae2a751259bbfdda3acf1/web/pdf_rendering_queue.js#L104)
- **React-PDF** 将页面显示和宿主控件分开。文字与链接几何按文档代次和页面缓存，在缩放或更换位图时复用。工具栏、进度和目录由宿主按需组合。 [React-PDF Page API](https://github.com/wojtekmaj/react-pdf/blob/bb19ba0dd2e5cfe3d3a813f104811d0ff50c9203/packages/react-pdf/README.md#page)

## 开发里程碑

| 里程碑 | 交付 | 验收 |
| --- | --- | --- |
| **`v0.1.0` 首版** | 连续分页、高清分块、整屏换版、公开 API、独立窗口示例和资源准备命令。 | 示例用 `mygo dev` 启动，六个目标均能构建并装入正确的动态库和许可证。覆盖保色渲染、复制、链接、同屏换版、页数减少、过期结果和回调重入。 |
| **`v0.2` 渲染与交互** | 按滚动方向预取，复用高清块，支持取消渐进渲染。将文字和链接几何从位图缓存中拆出。缓存静态页面几何，减少缩放时的全篇布局分配。补齐 `XYZ`、`Fit`、`FitH/V`、`FitR` 及其边界盒变体。 | 用固定视口录像核对闪白、锚点和返回已浏览区域的表现。测试跨窗口优先级、过期任务、退出后的资源回收，并测量高倍细节、横向平移和高 DPI。 |
| **`v0.3` 长文阅读** | 加入基于 `io.ReaderAt` 和文件大小的本地随机读。保留 256 MiB 上限，文件内容在引擎释放前须稳定。先测量首屏所需页面，再按视口优先级补齐其余尺寸。用 PDFium 接口实现密码重试、目录和可取消搜索。示例增加页码输入、目录、搜索和 90° 旋转。通过 MyGo 的语义节点提供可访问的文字、页码和键盘链接。 | 覆盖未加密文档、密码错误、限制复制、混排、旋转与裁切、目录与搜索跳转。实际运行屏幕阅读器，记录当前 MyGo 能提供的语义范围。 [PDFium 公开 API](https://pdfium.googlesource.com/pdfium/+/refs/heads/chromium/8179/public/) |
| **`v1.0` 稳定交付** | 冻结经过独立示例与宿主接入验证的 API，记录兼容范围、PDFium 更新步骤和桌面支持表。验证自动打包及许可分发。 | 从空缓存构建，验证签名、解包和安装后的启动。三种桌面系统按同一清单验证输入、导航、清晰度和错误恢复。 |

## 验证与发行

- **自动测试。** 每次发布运行 `mise run ci`，缺少 PDFium 时集成测试必须失败。使用自己生成或可再分发的文档，覆盖混合页面尺寸与裁切、中文及 RTL、复制限制、密码、损坏文件、长文档和复杂扫描页。六个构建目标使用各自的动态库。
- **性能验收。** 首次发布前记录本项目的首版基线。后续使用相同硬件、样本和脚本，记录 MyGo 与 PDFium 版本。至少执行 30 次冷打开和 3 次固定手势脚本，报告首个可见位图、首屏齐备、手势帧时、高清完成时间及峰值 RSS 的 P50/P95。普通文档的首屏耗时和手势帧时 P95 不超过首版的 `110%`。`v0.2` 的 1 万页缩放基准每次分配量最多为首版的 `10%`。整页和分块缓存预算分别为 24 MiB 与 40 MiB，在途位图、原生堆和 GPU 另计。新代次首次显示应覆盖实际可见范围。
- **原生验收。** 在 macOS、Windows、Linux Wayland 和 X11 上验证键盘、鼠标、剪贴板、DPI 切换及退出。macOS、Windows 和 Wayland 还需用相应设备验证捏合。X11 用修饰键滚轮和键盘缩放。记录每个平台的实际窗口和安装器结果。 [MyGo 平台输入源码](https://github.com/egoist/mygo/tree/v0.10.0/internal)
- **资源打包。** 构建前运行 `cmd/pdfium-resources`，校验上游归档和动态库，随包分发许可证。MyGo v0.10 的 `mygo-plugin.json` 下载单个带校验和的文件，PDFium 上游提供含动态库和许可的归档。自动打包使用项目发布的单文件资产，并验证许可证随宿主应用入包。若清单无法分发完整许可，以命令行准备资源作为正式接入方式。运行时从应用包或显式路径加载。 [MyGo 清单加载](https://github.com/egoist/mygo/blob/v0.10.0/cmd/mygo/natives.go#L45) · [PDFium 归档](https://github.com/bblanchon/pdfium-binaries/releases/tag/chromium/8179)
- **版本与回退。** 宿主固定依赖版本，遇到回归可回退到上一个标签。上游下载中断时可使用经 SHA-256 校验的缓存。发布资产需要仓库的 GitHub 发布权限，正式 macOS 安装包需要相应签名凭据。

MyGo 上游若加入连续视口和原子换版，重新核对渲染、公开 API 和打包兼容性。可共享的底层改进优先回馈上游。
