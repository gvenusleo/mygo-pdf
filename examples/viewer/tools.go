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

func (d *demo) readingTools(c *ui.Context) {
	s := d.status
	ui.Row(c.Key("reading tools")).FillWidth().AlignItems(ui.Center).Gap(c.Theme().Space(2)).Padding(c.Theme().Space(2)).Children(func() {
		disabled := s.PageCount == 0 || s.Loading
		ui.Button(c, "Outline").Disabled(disabled && !d.showOutline).OnClick(func() {
			d.showOutline = !d.showOutline
			if d.showOutline {
				d.requestOutline()
			}
		})
		find := func(direction pdf.FindDirection) { d.viewer.Find(d.ctx, d.findQuery, direction) }
		ui.TextInput(c.Key("find"), &d.findQuery).Label("Find text").Placeholder("Find text").Width(210).Disabled(disabled).
			OnSubmit(func() { find(pdf.FindForward) }).OnChange(func() { d.viewer.ClearFind() })
		ui.Button(c, "Previous match").Disabled(disabled || d.findQuery == "").OnClick(func() { find(pdf.FindBackward) })
		ui.Button(c, "Next match").Disabled(disabled || d.findQuery == "").OnClick(func() { find(pdf.FindForward) })
		if message := findMessage(s.Find); message != "" {
			ui.Text(c, message).TextColor(c.Theme().TextMuted).Wrap()
		}
	})
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
	ui.Column(c).Width(250).MinHeight(0).Padding(c.Theme().Space(2)).Label("Document outline").Children(func() {
		switch {
		case d.outlineLoading:
			ui.Text(c, "Reading outline…")
		case d.outline.Err != nil:
			ui.Text(c, d.outline.Err.Error()).Wrap()
		case len(d.outline.Entries) == 0:
			ui.Text(c, "No bookmarks")
		default:
			if d.outline.Truncated {
				ui.Text(c, "The outline is incomplete.").Wrap()
			}
			rows := d.outlineRowsCache
			ui.List(c, &d.outlineList, len(rows)).Grow(1).ItemKey(func(i int) any { return rows[i].key }).Rows(func(row ui.ListRow) {
				d.outlineRow(c, rows[row.Index])
			})
		}
	})
}

func (d *demo) outlineRow(c *ui.Context, row outlineRow) {
	entry := row.entry
	name := cmp.Or(entry.Title, "Untitled bookmark")
	ui.Row(c).FillWidth().PaddingStart(float32(min(row.depth, 12)) * 10).Children(func() {
		if len(entry.Children) > 0 {
			label, marker := "Expand "+name, "▸"
			if d.expanded[row.key] {
				label, marker = "Collapse "+name, "▾"
			}
			ui.Button(c.Key("toggle"), marker).Label(label).OnClick(func() {
				if d.expanded == nil {
					d.expanded = make(map[string]bool)
				}
				d.expanded[row.key] = !d.expanded[row.key]
				d.refreshOutlineRows()
			})
		}
		if entry.Page > 0 {
			ui.Button(c.Key("entry"), name).Grow(1).OnClick(func() { d.viewer.GoToOutline(entry) })
		} else {
			ui.Text(c.Key("heading"), name).Wrap()
		}
	})
}
