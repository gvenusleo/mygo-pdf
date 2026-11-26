# 开发和发布 MyGo PDF

[English](../en/development.md)

## 运行示例和测试

安装 [mise](https://mise.jdx.dev/)，然后在仓库根目录运行 `mise trust`。Go 版本由 `mise.toml` 指定，MyGo 版本见 `go.mod`，PDFium 上游归档由 [`cmd/pdfium-resources`](../../cmd/pdfium-resources) 指定。

| 命令 | 用途 |
| --- | --- |
| `mise run dev` | 启动[独立查看器](../../examples/viewer)，支持热重载。 |
| `mise run build` | 为本机平台打包示例和原生资源，产物写入 `build/`。 |
| `mise run resources` | 下载并校验本机平台的 PDFium 动态库，供集成测试使用。 |
| `mise run ci` | 检查格式，运行 Go 和 MyGo vet，再分别运行带竞态检测和禁用 cgo 的 PDFium 集成测试。 |

`mise run ci` 需要本机工具链支持 Go 竞态检测。GitHub Actions 在六个桌面目标上运行禁用 cgo 的测试，并在 macOS 和 Linux 的两个架构以及 Windows amd64 上运行竞态检测。交叉构建可以检查目标平台的编译和打包。要验证原生 PDFium 调用，请在目标平台运行集成测试。

准备好动态库后，可以单独运行密码与权限测试。

```sh
MYGO_PDF_REQUIRE_INTEGRATION=1 go test -count=1 ./pdf -run '^TestPasswordRetryAndExtractionPermissions$'
```

设置 `MYGO_PDF_REQUIRE_INTEGRATION=1` 后，缺少 PDFium 资源会导致测试失败。可以用 `MYGO_PDF_TEST_LIBRARY` 指定兼容动态库的绝对路径。按需运行的基准见[性能实测](performance.md)。

要用 [Typst](https://typst.app/) 重新生成内置 PDF，请运行 `typst compile --ignore-system-fonts examples/viewer/sample.typ examples/viewer/sample.pdf`。加密夹具另有[生成步骤](../../pdf/testdata/README.md)。

## 参与贡献

通过 [GitHub Issues](https://github.com/gvenusleo/mygo-pdf/issues) 报告问题或提出建议。请附上 MyGo PDF 和 MyGo 版本、操作系统和架构，以及复现步骤。如果问题取决于特定文档，请使用可再分发的样例。

提交变更前运行 `mise run ci`。涉及导航、输入或渲染时，请运行独立示例并注明实际测试的平台。组件边界见[设计文档](design.md)，仓库约定见 [AGENTS.md](../../AGENTS.md)。

## 发布流程

1. 在 `mygo.json` 中设置下一版的版本号。如果动态库和许可声明仍与清单中的记录一致，可以复用现有 PDFium 资产。
2. 运行 `mise run ci`，提交候选版本并推送到 `main`。在 **Actions** 为 `main` 手动运行 **Release**，或执行 `gh workflow run release.yml --ref main`。[工作流](../../.github/workflows/release.yml)会运行六个目标的原生测试并校验已发布的 PDFium 资产。
3. 等验证通过后，确认运行记录的提交 SHA 与待发布提交完全一致。为该提交创建并推送与 `mygo.json` 版本匹配的 `v*` 标签。标签一经推送，Go 用户就可以获取该版本。
4. 查看标签触发的工作流和最终发布的 GitHub Release。工作流会核对标签版本和同一 SHA 的手动验证记录，再创建发布。

如果运行器或下载暂时失败，可以重跑验证。候选提交需要修复时，先验证修正后的提交，再打标签。已打标签的源码需要修复时，发布新的补丁版本。正式分发的 macOS 安装包需要签名凭据。

## 更新 PDFium 资产

在 [`cmd/pdfium-resources`](../../cmd/pdfium-resources) 中更新上游版本和校验值。动态库或许可声明发生变化时，使用新的资产标签。例如，PDFium 8179 资产的一次修订可以使用 `pdfium-8179-r2`。

```sh
go run ./cmd/pdfium-resources -release=pdfium-8179-r2
gh release create pdfium-8179-r2 --latest=false \
  --title "PDFium 8179 assets (revision 2)" --notes "PDFium binaries and license notices for MyGo PDF." \
  build/pdfium-release/darwin-*-libpdfium.dylib \
  build/pdfium-release/linux-*-libpdfium.so \
  build/pdfium-release/windows-*-pdfium.dll \
  build/pdfium-release/pdfium-notices.txt
```

确认七个公开下载文件都与生成产物一致。将 `build/pdfium-release/mygo-plugin.json` 复制到 `internal/pdfium/mygo-plugin.json`，随下一次源码发布提交。保留旧资产，供使用早期模块版本的宿主下载。生成器会在写清单前校验指定的上游归档和动态库。上游暂时不可用时，可以使用通过 SHA-256 校验的缓存。
