# MyGo PDF roadmap

[简体中文](../zh-CN/plan.md)

## Before v1.0

- **Stabilize the host API.** Validate the import path, input ownership, navigation and callback contracts in an independent host before promising compatibility.
- **Finish native verification.** Exercise keyboard, mouse, clipboard, DPI changes and shutdown on Windows and Linux. Cover Wayland and X11 on Linux. Verify pinch on macOS, Windows and Wayland where supported, and modifier-wheel and keyboard zoom on X11. Record screen-reader results for page labels, permitted text and link activation on the supported desktops.
- **Verify installers.** Build from an empty cache and check signing, unpacking, bundled PDFium resources and startup after installation on the supported desktops. Record native results separately from cross-builds.
- **Complete regression coverage.** Check Chinese and right-to-left text, mixed page sizes, cropped pages and complex scans alongside the existing refresh, cancellation and password tests. Check that a newly displayed document covers the actual viewport.
- **Verify performance.** Use the [current measurements](performance.md) to repeat the headless workloads under the same conditions. For ordinary documents, keep first-viewport and gesture-frame P95 within 110% of the baseline. Keep allocations per operation in the 10,000-page zoom benchmark within 10% of its baseline. Check bitmap and tile budgets. Measure native window timing and GPU memory separately from process RSS and in-flight bitmaps.
