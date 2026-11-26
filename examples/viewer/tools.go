package main

import (
	"cmp"
	"fmt"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/gvenusleo/mygo-pdf/pdf"
)

func pageLabel(page int) string {
	if page == 0 {
		return ""
	}
	return strconv.Itoa(page)
}

func (d *demo) changed(state pdf.State) {
	if state.DisplayedGeneration != d.status.DisplayedGeneration {
		if state.DisplayedGeneration == d.openGeneration && state.DisplayedGeneration != 0 {
			d.displayedName = d.openName
		}
		d.outline = pdf.OutlineResult{}
		d.outlineRowsCache = nil
		d.outlineID, d.outlineLoading = 0, false
		clear(d.expanded)
	}
	if state.Loading && !d.status.Loading {
		d.outlineLoading = false
	}
	d.status = state
	if d.showOutline {
		d.requestOutline()
	}
}

func (d *demo) requestOutlineIfVisible() {
	if d.showOutline {
		d.requestOutline()
	}
}

func (d *demo) focusFind() {
	if d.status.PageCount > 0 {
		d.preview, d.showFind = false, true
		d.findField.SelectAll()
	}
}

func (d *demo) findVisibilityChanged() {
	if d.showFind {
		d.focusFind()
	} else {
		d.closeFind()
	}
}

func (d *demo) closeFind() {
	d.showFind = false
	d.findField.CancelFocus()
	d.viewer.ClearFind()
}

func (d *demo) findBar(c *ui.Context) {
	s, t := d.status, c.Theme()
	ui.Toolbar(c.Key("find bar"), func() {
		ui.Spacer(c)
		disabled := s.PageCount == 0 || s.Loading
		if message := findMessage(s.Find); message != "" {
			ui.Text(c.Key("find result"), message).MaxWidth(160).Wrap().TextColor(t.TextMuted).FontSize(12)
		}
		find := func(direction pdf.FindDirection) { d.viewer.Find(d.ctx, d.findQuery, direction) }
		ui.TextInput(c.Key("find field"), &d.findQuery).Label("Find text").Placeholder("Find in document").
			Size(192, controlSize).PaddingX(8).Bind(&d.findField).Disabled(disabled).
			OnSubmit(func() { find(pdf.FindForward) }).OnChange(d.viewer.ClearFind)
		d.findField.OnShortcut(c, 0, ui.KeyEscape, d.closeFind)
		toolbarButton(c.Key("previous match"), "Previous match", leftIcon).
			Disabled(disabled || d.findQuery == "").OnClick(func() { find(pdf.FindBackward) })
		toolbarButton(c.Key("next match"), "Next match", rightIcon).
			Disabled(disabled || d.findQuery == "").OnClick(func() { find(pdf.FindForward) })
		toolbarButton(c.Key("close find"), "Close find", closeIcon).OnClick(d.closeFind)
	}).Label("Search controls").FillWidth().Gap(6).Padding(0, chromeInset, 6)
}

func findMessage(state pdf.FindState) string {
	switch {
	case state.Err != nil:
		return state.Err.Error()
	case state.Searching:
		return "Searching…"
	case state.Page > 0:
		return fmt.Sprintf("Match on page %d", state.Page)
	case state.RequestGeneration > 0:
		return "No matches"
	default:
		return ""
	}
}

func (d *demo) requestOutline() {
	if d.status.Loading || d.outlineLoading || d.outline.DisplayedGeneration == d.status.DisplayedGeneration {
		return
	}
	d.outlineID = d.viewer.ReadOutline(d.ctx, func(result pdf.OutlineResult) {
		if result.RequestGeneration == d.outlineID {
			d.outline, d.outlineLoading = result, false
			d.refreshOutlineRows()
		}
	})
	d.outlineLoading = d.outlineID != 0
}

type outlineRow struct {
	entry pdf.OutlineEntry
	key   string
	depth int
}

func (d *demo) refreshOutlineRows() {
	rows := d.outlineRowsCache
	d.outlineRowsCache = d.outlineRows(rows[:0], d.outline.Entries, "", 0)
	if len(d.outlineRowsCache) < len(rows) {
		clear(rows[len(d.outlineRowsCache):])
	}
}

func (d *demo) outlineRows(rows []outlineRow, entries []pdf.OutlineEntry, parent string, depth int) []outlineRow {
	for i, entry := range entries {
		key := parent + "/" + strconv.Itoa(i)
		rows = append(rows, outlineRow{entry: entry, key: key, depth: depth})
		if d.expanded[key] {
			rows = d.outlineRows(rows, entry.Children, key, depth+1)
		}
	}
	return rows
}

func (d *demo) outlineView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Width(180).Shrink(0).FillHeight().MinHeight(0).Background(t.Background).Label("Document outline").Children(func() {
		ui.Row(c.Key("outline heading")).FillWidth().Height(32).PaddingX(chromeInset).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Outline").FontWeight(600)
		})
		ui.Column(c.Key("outline contents")).Grow(1).FillWidth().MinHeight(0).Padding(6).Gap(6).Children(func() {
			switch {
			case d.outlineLoading:
				ui.Text(c, "Reading outline…").Padding(8).TextColor(t.TextMuted)
			case d.outline.Err != nil:
				ui.Text(c, d.outline.Err.Error()).Padding(8).TextColor(t.Danger).Wrap()
			case len(d.outline.Entries) == 0:
				ui.Text(c, "No bookmarks").Padding(8).TextColor(t.TextMuted)
			default:
				if d.outline.Truncated {
					ui.Text(c, "The outline is incomplete.").PaddingX(8).TextColor(t.TextMuted).Wrap()
				}
				rows := d.outlineRowsCache
				ui.List(c.Key("bookmarks"), &d.outlineList, len(rows)).Grow(1).ItemKey(func(i int) any { return rows[i].key }).Rows(func(row ui.ListRow) {
					d.outlineRow(c, rows[row.Index])
				})
			}
		})
	})
}

func (d *demo) outlineRow(c *ui.Context, row outlineRow) {
	entry := row.entry
	name := cmp.Or(entry.Title, "Untitled bookmark")
	ui.Row(c).FillWidth().AlignItems(ui.Center).PaddingStart(float32(min(row.depth, 5)) * 10).Children(func() {
		if len(entry.Children) > 0 {
			label, icon := "Expand "+name, rightIcon
			if d.expanded[row.key] {
				label, icon = "Collapse "+name, downIcon
			}
			ui.ButtonBase(c.Key("toggle")).Size(24, controlSize).Label(label).Tooltip(label).Expanded(d.expanded[row.key]).
				Children(func() { toolbarIcon(c, icon) }).OnClick(func() {
				if d.expanded == nil {
					d.expanded = make(map[string]bool)
				}
				d.expanded[row.key] = !d.expanded[row.key]
				d.refreshOutlineRows()
			})
		}
		if entry.Page > 0 {
			t := c.Theme()
			current := d.status.Page == entry.Page
			button := ui.ButtonBase(c.Key("entry")).Grow(1).MinWidth(0).Height(controlSize).PaddingX(6).Radius(t.Radius).
				Justify(ui.Start).Label(name).Tooltip(name).OnClick(func() { d.viewer.GoToOutline(entry) })
			if current {
				button.Current(ui.CurrentPage)
			}
			if button.Pressed() || current {
				button.Background(t.SurfacePressed)
			} else if button.Hovered() {
				button.Background(t.SurfaceHover)
			}
			button.Children(func() {
				title := ui.Text(c, name).Grow(1).MinWidth(0).SingleLine().FontSize(13)
				if current {
					title.FontWeight(600)
				}
				ui.Textf(c, "%d", entry.Page).PaddingStart(6).TextColor(t.TextMuted).FontSize(12)
			})
		} else {
			ui.Text(c.Key("heading"), name).Padding(8).Wrap()
		}
	})
}
