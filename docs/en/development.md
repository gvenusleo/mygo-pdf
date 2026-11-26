# Develop and release MyGo PDF

[简体中文](../zh-CN/development.md)

## Run the example and tests

Install [mise](https://mise.jdx.dev/), then run `mise trust` from the repository root. The repository pins Go in `mise.toml`, MyGo in `go.mod` and the PDFium archives in [`cmd/pdfium-resources`](../../cmd/pdfium-resources).

| Command | What it does |
| --- | --- |
| `mise run dev` | Runs the [standalone viewer](../../examples/viewer) with live reload. |
| `mise run build` | Packages the example and its native resources under `build/` for the host target. |
| `mise run resources` | Downloads and verifies the host's PDFium library for integration tests. |
| `mise run ci` | Checks formatting, runs Go and MyGo vet, then runs the PDFium integration suite with the race detector and again with cgo disabled. |

`mise run ci` requires a Go race-detector toolchain on the host. GitHub Actions runs the cgo-disabled tests on all six desktop targets. The race suite covers both macOS targets, both Linux targets and Windows amd64. A cross-build checks compilation and packaging for its target. Run the integration suite on that target to exercise its native PDFium calls.

For a focused integration run after preparing the library:

```sh
MYGO_PDF_REQUIRE_INTEGRATION=1 go test -count=1 ./pdf -run '^TestPasswordRetryAndExtractionPermissions$'
```

`MYGO_PDF_REQUIRE_INTEGRATION=1` makes missing PDFium resources a failure. `MYGO_PDF_TEST_LIBRARY` can point tests at an absolute path to a compatible library. See [performance measurements](performance.md) for the opt-in benchmarks.

To regenerate the bundled PDF with [Typst](https://typst.app/), run `typst compile --ignore-system-fonts examples/viewer/sample.typ examples/viewer/sample.pdf`. Encrypted fixtures have a [separate regeneration procedure](../../pdf/testdata/README.md).

## Contribute

Use [GitHub Issues](https://github.com/gvenusleo/mygo-pdf/issues) to report a bug or propose a change. Include the MyGo PDF and MyGo versions, the OS and architecture, and steps to reproduce the issue. If the result depends on the document, use a redistributable sample.

Run `mise run ci` before submitting changes. For changes in navigation, input or rendering, exercise the standalone example and note the platforms tested. See [design](design.md) for component boundaries and [AGENTS.md](../../AGENTS.md) for repository conventions.

## Release procedure

1. Set `mygo.json` to the next release version. Existing PDFium assets can be reused when the binaries and notices match the pinned manifest.
2. Run `mise run ci`, commit the candidate and push it to `main`. From **Actions**, run **Release** for `main`, or use `gh workflow run release.yml --ref main`. The [workflow](../../.github/workflows/release.yml) runs the six native test jobs and verifies the published PDFium assets.
3. Wait for the run to succeed. Confirm that its head SHA matches the exact commit to be released. Create and push a `v*` tag for that commit with a version matching `mygo.json`. Go users can fetch a tag as soon as it is pushed.
4. Check the tag run and the published GitHub Release. The tag run checks the version and the successful manual verification for that SHA before publishing.

Rerun a failed verification for a transient runner or download failure. If the candidate needs a fix, verify the updated commit before tagging it. Publish a new patch release for a fix to already tagged source. Official macOS installers require signing credentials.

## Update PDFium assets

Update the upstream version and pinned checksums in [`cmd/pdfium-resources`](../../cmd/pdfium-resources). If the binaries or notices change, choose a new asset tag. For a revision of the PDFium 8179 assets, use a tag such as `pdfium-8179-r2`.

```sh
go run ./cmd/pdfium-resources -release=pdfium-8179-r2
gh release create pdfium-8179-r2 --latest=false \
  --title "PDFium 8179 assets (revision 2)" --notes "PDFium binaries and license notices for MyGo PDF." \
  build/pdfium-release/darwin-*-libpdfium.dylib \
  build/pdfium-release/linux-*-libpdfium.so \
  build/pdfium-release/windows-*-pdfium.dll \
  build/pdfium-release/pdfium-notices.txt
```

Verify that all seven public downloads match the generated files. Copy `build/pdfium-release/mygo-plugin.json` to `internal/pdfium/mygo-plugin.json` and include it in the next source release. Keep older assets available for hosts using earlier module versions. The generator verifies the pinned upstream archives and libraries before writing the manifest. A cache with matching SHA-256 digests can serve the assets during an upstream outage.
