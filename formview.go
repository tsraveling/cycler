package main

import (
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// @region form:view -- FIELD LAYOUT

func (f *form) labelStyle(fl field) lipgloss.Style {
	for _, l := range f.lines() {
		if l.f == fl && l.disabled {
			return grayStyle
		}
	}
	if f.focus == fl {
		return orangeStyle
	}
	return grayStyle
}

func (f *form) label(fl field, s string) string { return f.labelStyle(fl).Render(s) }

// is the cursor sitting in this cell right now
func (f *form) inCell(fl field, cell int) bool {
	return f.editing && f.focus == fl && f.cell == cell
}

// a blurred input keeps no cursor cell, so a selected line reads as plain text
func (f *form) cellView(fl field, cell int) string {
	in := f.ins[fl][cell]
	if f.inCell(fl, cell) {
		return in.View()
	}
	if in.Value() == "" {
		return grayStyle.Render(in.Placeholder)
	}
	// a blurred input no longer scrolls itself, so it is cut to its own width
	v := in.Value()
	if in.Width > 0 {
		v = truncWidth(v, in.Width)
	}
	return textStyle.Render(v)
}

func (f *form) textView(fl field) string { return f.cellView(fl, 0) }

// hh:mm, with a red marker when the pair cannot be read as a time
func (f *form) timeView(fl field, fw int) string {
	hh, mm := f.ins[fl][0], f.ins[fl][1]
	var body string
	switch {
	case f.inCell(fl, 0) || f.inCell(fl, 1):
		body = f.cellPad(fl, 0) + grayStyle.Render(":") + f.cellPad(fl, 1)
	case hh.Value() == "" && mm.Value() == "":
		body = grayStyle.Render("--:--")
	default:
		body = textStyle.Render(pad2(hh.Value())) + grayStyle.Render(":") + textStyle.Render(pad2(mm.Value()))
	}
	if f.clockValid(fl) {
		return body
	}
	warn := redStyle.Render("Invalid!")
	gap := max(1, fw-lipgloss.Width(body)-lipgloss.Width(warn))
	return body + spaces(gap) + warn
}

// while editing, the cell being typed into shows its cursor and the other keeps
// its width so the colon does not jump
func (f *form) cellPad(fl field, cell int) string {
	if f.inCell(fl, cell) {
		return f.ins[fl][cell].View()
	}
	v := f.ins[fl][cell].Value()
	if v == "" {
		return grayStyle.Render("--")
	}
	return textStyle.Render(pad2(v))
}

func pad2(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

// the item timer, which keeps its own minutes:seconds shape
func (f *form) timerView() string {
	return f.numView(0) + grayStyle.Render(":") + f.numView(1)
}

func (f *form) numView(cell int) string {
	in := f.ins[fTimer][cell]
	if f.inCell(fTimer, cell) {
		return in.View()
	}
	if in.Value() == "" {
		return grayStyle.Render(in.Placeholder)
	}
	return textStyle.Render(in.Value())
}

// an empty limit reads as "no limit" once the field is left
func (f *form) timesView() string {
	in := f.ins[fTimes][0]
	if f.inCell(fTimes, 0) {
		return in.View()
	}
	if in.Value() == "" {
		return grayStyle.Render("no limit")
	}
	return textStyle.Render(in.Value())
}

// ◀ label ▶, in the given style, dimmed when the line is disabled
func (f *form) picker(fl field, label string, body lipgloss.Style) string {
	for _, l := range f.lines() {
		if l.f == fl && l.disabled {
			return grayStyle.Render("◀ " + label + " ▶")
		}
	}
	arrow := textStyle
	if f.focus == fl {
		arrow = orangeStyle
		body = body.Bold(true)
	}
	return arrow.Render("◀ ") + body.Render(label) + arrow.Render(" ▶")
}

// the preset cells and the seven day cells, on one row when it fits
func (f *form) dayRows(fw int) []string {
	cell := func(i int, text string, on bool) string {
		switch {
		case f.focus == fDays && f.dayCur == i:
			return orangeStyle.Bold(true).Render(text)
		case on:
			return textStyle.Render(text)
		}
		return grayStyle.Render(text)
	}
	presets := cell(cellEveryDay, "Every day", slices.Equal(f.days, allDays())) +
		"  " + cell(cellWeekdays, "Weekdays", slices.Equal(f.days, weekdayDays()))
	cells := make([]string, len(weekOrder))
	for i, d := range weekOrder {
		cells[i] = cell(cellFirstDay+i, dayAbbrev[d], slices.Contains(f.days, int(d)))
	}
	days := strings.Join(cells, " ")
	if one := presets + "  " + days; lipgloss.Width(one) <= fw {
		return []string{one}
	}
	return []string{presets, days}
}

// the day set as it will read back on the config screen
func (f *form) daysValue(fw int) string {
	style := textStyle
	if len(f.days) == 0 {
		style = grayStyle
	}
	return style.Render(truncWidth(daysLabel(f.days), fw))
}

func (f *form) buttons() string {
	saveStyle, cancelStyle := greenStyle, grayStyle
	if !f.saveReady() {
		saveStyle = grayStyle
	}
	if f.cancelSel {
		saveStyle, cancelStyle = grayStyle, redStyle
	}
	if f.focus == fButtons {
		if f.cancelSel {
			cancelStyle = cancelStyle.Bold(true)
		} else {
			saveStyle = saveStyle.Bold(true)
		}
	}
	return saveStyle.Render("[ save ]") + "   " + cancelStyle.Render("[ cancel ]")
}

// the source type picker, its input and its preview; one gray dash when the
// block type makes them meaningless
func (f *form) sourceRows(fw int, compact bool, field func(field, string, string) []string) []string {
	if f.isCycler() {
		return []string{grayStyle.Render("Source"), grayStyle.Render("-")}
	}
	lines := []string{"", f.label(fSrcType, "Source type")}
	if compact {
		lines = []string{""}
		lines = append(lines, field(fSrcType, "Source type", f.picker(fSrcType, sourceTypes[f.typ].label(), textStyle))...)
	} else {
		lines = append(lines, f.picker(fSrcType, sourceTypes[f.typ].label(), textStyle))
	}
	lines = append(lines, "", f.label(fSrc, sourceTypes[f.typ].hint()), f.textView(fSrc))
	for _, l := range wrapCapped(f.preview.text, fw, 2) {
		lines = append(lines, f.preview.style().Render(l))
	}
	return lines
}

func (m model) formView() string {
	w, h := m.w, m.h
	cw, ch := contentWidth(w), contentHeight(h)
	f := m.f
	fw := f.width(w)

	title := "New item"
	switch {
	case f.kind == formBlock && f.editID != "":
		title = "Edit block"
	case f.kind == formBlock:
		title = "New block"
	case f.editID != "":
		title = "Edit item"
	}
	head := []string{orangeStyle.Bold(true).Render(title), ""}

	help := toggledHelp(formHelp, m.c.HelpFull, cw)
	if m.prompt != nil {
		help = []string{promptStyle.Render(m.prompt.prompt + " y/n")}
	}

	build := func(withDaysValue, compact bool) []string {
		// a short panel puts a label and its value on one row, where they fit
		field := func(fl field, name, value string) []string {
			if one := f.label(fl, name) + "  " + value; compact && lipgloss.Width(one) <= fw {
				return []string{one}
			}
			return []string{f.label(fl, name), value}
		}

		var lines []string
		if f.kind == formBlock {
			lines = []string{f.label(fTitle, "Title"), f.textView(fTitle), ""}
			lines = append(lines, field(fType, "Type", f.picker(fType, blockTypes[f.btyp].label(), textStyle))...)
			lines = append(lines, "")
			sw := blockColors[f.color]
			lines = append(lines, field(fColor, "Color", f.picker(fColor, sw.name, lipgloss.NewStyle().Foreground(sw.color)))...)
			lines = append(lines, "")
			lines = append(lines, field(fStart, "Start", f.timeView(fStart, fw))...)
			lines = append(lines, "")
			lines = append(lines, field(fEnd, "End", f.timeView(fEnd, fw))...)
			lines = append(lines, "")
		} else {
			lines = []string{f.label(fName, "Name"), f.textView(fName), ""}
			lines = append(lines, field(fTimer, "Timer", f.timerView())...)
			lines = append(lines, "")
			lines = append(lines, field(fTimes, "Times per day", f.timesView())...)
			lines = append(lines, "")
		}

		rows := f.dayRows(fw)
		lines = append(lines, field(fDays, "Active on", rows[0])...)
		lines = append(lines, rows[1:]...)
		if withDaysValue {
			lines = append(lines, f.daysValue(fw))
		}
		lines = append(lines, f.sourceRows(fw, compact, field)...)
		lines = append(lines, "", f.buttons())
		if f.err != "" {
			lines = append(lines, redStyle.Render(truncWidth(f.err, fw)))
		}
		return lines
	}

	// a short panel sheds the spacer rows first, then the day summary, which
	// only restates the row of toggles above it, then the row per label
	room := blockRows(len(head), len(help), ch)
	lines := build(true, false)
	if len(lines) > room {
		lines = dropBlanks(lines)
	}
	if len(lines) > room {
		lines = dropBlanks(build(false, false))
	}
	if len(lines) > room {
		lines = dropBlanks(build(false, true))
	}

	block := padBlock(lines, fw, cw)
	return renderPanel(stackContent(head, block, help, ch), w, h, orange)
}

func dropBlanks(lines []string) []string {
	packed := lines[:0:0]
	for _, l := range lines {
		if l != "" {
			packed = append(packed, l)
		}
	}
	return packed
}

// left-aligns rows in a block of width bw, centered in cw
func padBlock(rows []string, bw, cw int) []string {
	left := spaces(max(0, (cw-bw)/2))
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = left + r
	}
	return out
}
