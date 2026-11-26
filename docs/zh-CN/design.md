# MyGo PDF 设计

[English](../en/design.md)

查看器负责文档显示和导航。宿主提供 PDF 字节或内容稳定的随机访问源，并负责文件监控、路径授权、文档生成、持久化、导出和外部打开。宿主 API 见[接入指南](usage.md)，后续工作见[路线图](plan.md)。

## 包结构

| 位置 | 职责 |
| --- | --- |
| [`pdf`](../../pdf) | 公开 API、窗口线程状态、MyGo 视图与输入、位图缓存和 worker 调度 |
| [`internal/layout`](../../internal/layout) | 页面尺寸索引、视口几何和阅读位置 |
| [`internal/pdfium`](../../internal/pdfium) | 加载 PDFium、平台 ABI、原生句柄、光栅化和元数据提取 |
| [`internal/render`](../../internal/render) | 共享页面尺寸、光栅键、整页分辨率上限和缓存预算 |
| [`cmd/pdfium-resources`](../../cmd/pdfium-resources) | 校验并准备原生动态库和许可声明 |
| [`examples/viewer`](../../examples/viewer) | 演示宿主如何打开文件、组装工具栏和嵌入预览 |

公开包使用上述三个内部包。`internal/layout` 和 `internal/pdfium` 共享 `internal/render` 中的类型，均独立于 MyGo。

## 窗口和 worker

每个查看器属于一个窗口。UI 线程持有视口、当前代次、缓存和回调。每次构建视图时会提交当前视口的需求。worker 在 UI 线程之外读取文档和渲染像素，再通过 `Window.Update` 发布结果。应用结果前，UI 会核对请求代次或显示代次。渲染结果还须匹配当前光栅需求。`OnChange` 在状态提交后运行，回调可以清除、关闭或重新加载查看器。

刷新会先准备候选文档的可见页面，再一起替换布局和位图。视图此前已完成测量时，会在提交刷新前检查当前布局。刷新失败时会保留正在显示的文档。显式选中的页码会保持到纵向滚动位置发生变化。随后，当前页由视口中心决定。中心落在页间空隙时，取离中心最近的页。

## PDFium 生命周期和调度

进程内的 PDFium 调用串行执行。在原生文档还能读取 PDF 字节期间，字节保持固定地址。读取器支持的文档共用一个 purego 回调，通过注册的标记找到读取源。回调将数据读入临时原生缓冲区，读取错误归到触发它的操作。关闭文档句柄前，先释放页面和文本句柄。原生文档关闭后，才解除字节固定或发出读取源可释放的信号。`Close` 立即停止接收结果并返回。`Done` 表示 worker 已释放原生文档。

渐进式渲染在 PDFium 的暂停点让出执行权，恢复前检查取消状态。跨窗口调度时，可见任务优先于预取。等待恢复的预取任务若被取消，会按到达顺序进入前台队列，以释放原生句柄。进程最多允许四个进行中的原生光栅任务，其中一个名额留给可见任务。原生调用须先返回，其资源才能释放。显式指定的动态库缺少渐进式符号时，改用同步渲染。

## 页面几何和渲染

从字节加载时，打开前会测量所有页面尺寸。从读取器加载时，先测量首屏的页面，其余页面在读到实际尺寸前使用估算值。线段树维护页面累计尺寸和适配计算所需的最大宽高。优先测量显式跳转目标和可见页，随后测量并预取相邻页，再扫描其余页面。尺寸更新会保留阅读锚点。每次页面光栅化都使用准确尺寸。

查看器先渲染缺失的可见页位图，再处理可见文本和链接几何、可见图像的高清细节，以及滚动方向上的一页预览。缩放时会继续显示已有位图。视口外一个瓦片距离内的瓦片可以留在缓存中，光栅键匹配当前需求时才会绘制。整页位图、瓦片、文本与链接几何的缓存预算分别是 24 MiB、40 MiB 和 8 MiB。文档字节、生成中的位图和原生内存分别计量。测试负载和实测数据见[性能实测](performance.md)。

布局、选区、链接、跳转目标和搜索高亮共用页面坐标。查看器以页面的固有方向为基准，每次顺时针旋转 90 度。打开新文档会重置旋转角度，刷新则保留角度。等待新位图时，已有页面预览会随视图一起旋转。

## 导航和无障碍

内部跳转支持 `XYZ`、`Fit`、`FitH`、`FitV`、`FitR`、`FitB`、`FitBH` 和 `FitBV`。跳转缩放受查看器的缩放范围限制。边界框模式会估算裁剪框内的内容范围，边界为空或无法取得时使用裁剪后的整页。由于 PDFium 会为显式空值返回数值，`FitH` 或 `FitV` 中的空坐标按零处理。`XYZ` 中省略的轴或缩放会保留对应的相对位置或比例。

目录条目只用于生成它的查看器和显示代次。搜索一次跳到一处匹配，到达文档末端后从另一端继续。新加载会取消目录和搜索任务。跳转到尚未测量的页面时会等待尺寸。尺寸读取失败时，页码和书签跳转通过 `State.Err` 报错。搜索跳转通过 `State.Find.Err` 报错，同时清除当前匹配。

可见页面会提供页码、权限允许时每页最多 8 KiB 的无障碍文本，以及可聚焦的链接。键盘和辅助技术触发链接时，使用与鼠标相同的处理流程。文本提取遵循 PDFium 的顺序和权限标记。搜索高亮遵守文档的普通复制权限。

## 原生资源

[插件清单](../../internal/pdfium/mygo-plugin.json)与 PDFium 绑定放在一起。MyGo 通过宿主的导入关系找到清单，在打包前校验动态库和许可声明。查看器在运行时加载包内的动态库。参见[运行宿主应用](usage.md#运行宿主应用)和 [PDFium 资产更新流程](development.md#更新-pdfium-资产)。

## 设计参考

- MyGo 的上游 PDF 提案为查看器的生命周期和预览行为提供了参考。[MyGo PDF 提案](https://github.com/egoist/mygo/pull/124)
- PDFium 的渐进式接口为原生渲染提供暂停点。pdfrx 用控制器管理页码跳转、缩放和视口状态。[渐进式渲染 API](https://pdfium.googlesource.com/pdfium/+/refs/heads/chromium/8179/public/fpdf_progressive.h) · [pdfrx 控制器](https://github.com/espresso3389/pdfrx/blob/5476a917729fade064e0d8af279554db72644c11/packages/pdfrx/lib/src/widgets/pdf_viewer.dart#L6360)
- PDF.js 在缩放时复用当前图像。React-PDF 由宿主组装界面和控件。[PDF.js 缩放](https://github.com/mozilla/pdf.js/blob/3471ae1691d08cbe7ae2a751259bbfdda3acf1/web/pdf_viewer.js#L1605) · [PDF.js 渲染队列](https://github.com/mozilla/pdf.js/blob/3471ae1691d08cbe7ae2a751259bbfdda3acf1/web/pdf_rendering_queue.js#L104) · [React-PDF Page API](https://github.com/wojtekmaj/react-pdf/blob/bb19ba0dd2e5cfe3d3a813f104811d0ff50c9203/packages/react-pdf/README.md#page)
