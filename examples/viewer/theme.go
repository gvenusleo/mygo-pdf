package main

import "github.com/egoist/mygo/ui"

func readerChrome(system *ui.Theme) ui.Theme {
	theme := *system
	if theme.Dark {
		theme.Background = ui.Hex("#0d0d0d")
		theme.Surface = ui.Hex("#191919")
		theme.SurfaceHover = ui.Hex("#222222")
		theme.SurfacePressed = ui.Hex("#2c2c2c")
		theme.Border = ui.Hex("#303030")
		theme.Text = ui.Hex("#ededed")
		theme.TextMuted = ui.Hex("#a1a1a1")
	} else {
		theme.Background = ui.Hex("#ffffff")
		theme.Surface = ui.Hex("#fafafa")
		theme.SurfaceHover = ui.Hex("#f1f1f1")
		theme.SurfacePressed = ui.Hex("#e8e8e8")
		theme.Border = ui.Hex("#e0e0e0")
		theme.Text = ui.Hex("#171717")
		theme.TextMuted = ui.Hex("#626262")
	}
	theme.Accent, theme.AccentText = theme.Text, theme.Background
	theme.AccentHover = theme.Accent.Mix(theme.AccentText, 0.12)
	theme.AccentPressed = theme.Accent.Mix(theme.AccentText, 0.2)
	theme.InactiveAccent = theme.Accent
	theme.Radius, theme.FontSize = 6, 13
	return theme
}
